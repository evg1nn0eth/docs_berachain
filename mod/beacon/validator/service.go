// SPDX-License-Identifier: MIT
//
// Copyright (c) 2024 Berachain Foundation
//
// Permission is hereby granted, free of charge, to any person
// obtaining a copy of this software and associated documentation
// files (the "Software"), to deal in the Software without
// restriction, including without limitation the rights to use,
// copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the
// Software is furnished to do so, subject to the following
// conditions:
//
// The above copyright notice and this permission notice shall be
// included in all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
// EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES
// OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
// NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT
// HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
// WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
// FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR
// OTHER DEALINGS IN THE SOFTWARE.

package validator

import (
	"context"

	"github.com/berachain/beacon-kit/mod/core/state"
	"github.com/berachain/beacon-kit/mod/errors"
	"github.com/berachain/beacon-kit/mod/log"
	"github.com/berachain/beacon-kit/mod/primitives"
	"github.com/berachain/beacon-kit/mod/primitives/pkg/common"
	"github.com/berachain/beacon-kit/mod/primitives/pkg/consensus"
	"github.com/berachain/beacon-kit/mod/primitives/pkg/crypto"
	"github.com/berachain/beacon-kit/mod/primitives/pkg/math"
)

// Service is responsible for building beacon blocks.
type Service[
	BlobSidecarsT BlobSidecars,
] struct {
	// cfg is the validator config.
	cfg *Config
	// logger is a logger.
	logger log.Logger[any]

	// chainSpec is the chain spec.
	chainSpec primitives.ChainSpec

	// signer is used to retrieve the public key of this node.
	signer crypto.BLSSigner

	// blobFactory is used to create blob sidecars for blocks.
	blobFactory BlobFactory[BlobSidecarsT, consensus.BeaconBlockBody]

	// randaoProcessor is responsible for building the reveal for the
	// current slot.
	randaoProcessor RandaoProcessor[state.BeaconState]

	// ds is used to retrieve deposits that have been
	// queued up for inclusion in the next block.
	ds DepositStore

	// localBuilder represents the local block builder, this builder
	// is connected to this nodes execution client via the EngineAPI.
	// Building blocks is done by submitting forkchoice updates through.
	// The local Builder.
	localBuilder PayloadBuilder[state.BeaconState]

	// remoteBuilders represents a list of remote block builders, these
	// builders are connected to other execution clients via the EngineAPI.
	remoteBuilders []PayloadBuilder[state.BeaconState]
}

// NewService creates a new validator service.
func NewService[BlobSidecarsT BlobSidecars](
	cfg *Config,
	logger log.Logger[any],
	chainSpec primitives.ChainSpec,
	signer crypto.BLSSigner,
	blobFactory BlobFactory[BlobSidecarsT, consensus.BeaconBlockBody],
	randaoProcessor RandaoProcessor[state.BeaconState],
	ds DepositStore,
	localBuilder PayloadBuilder[state.BeaconState],
	remoteBuilders []PayloadBuilder[state.BeaconState],
) *Service[BlobSidecarsT] {
	return &Service[BlobSidecarsT]{
		cfg:             cfg,
		logger:          logger,
		chainSpec:       chainSpec,
		signer:          signer,
		blobFactory:     blobFactory,
		randaoProcessor: randaoProcessor,
		ds:              ds,
		localBuilder:    localBuilder,
		remoteBuilders:  remoteBuilders,
	}
}

// Name returns the name of the service.
func (s *Service[BlobSidecarsT]) Name() string {
	return "validator"
}

func (s *Service[BlobSidecarsT]) Start(context.Context) {}

func (s *Service[BlobSidecarsT]) Status() error { return nil }

func (s *Service[BlobSidecarsT]) WaitForHealthy(context.Context) {}

// LocalBuilder returns the local builder.
//
//nolint:lll // weird.
func (s *Service[BlobSidecarsT]) LocalBuilder() PayloadBuilder[state.BeaconState] {
	return s.localBuilder
}

// RequestBestBlock builds a new beacon block.
//
//nolint:funlen // todo:fix.
func (s *Service[BlobSidecarsT]) RequestBestBlock(
	ctx context.Context,
	st state.BeaconState,
	slot math.Slot,
) (consensus.BeaconBlock, BlobSidecarsT, error) {
	var sidecars BlobSidecarsT
	s.logger.Info("our turn to propose a block 🙈", "slot", slot)
	// The goal here is to acquire a payload whose parent is the previously
	// finalized block, such that, if this payload is accepted, it will be
	// the next finalized block in the chain. A byproduct of this design
	// is that we get the nice property of lazily propogating the finalized
	// and safe block hashes to the execution client.
	reveal, err := s.randaoProcessor.BuildReveal(st)
	if err != nil {
		return nil, sidecars, errors.Newf("failed to build reveal: %w", err)
	}

	parentBlockRoot, err := st.GetBlockRootAtIndex(
		uint64(slot) % s.chainSpec.SlotsPerHistoricalRoot(),
	)
	if err != nil {
		return nil, sidecars, errors.Newf(
			"failed to get block root at index: %w",
			err,
		)
	}
	// Get the proposer index for the slot.
	proposerIndex, err := st.ValidatorIndexByPubkey(
		s.signer.PublicKey(),
	)
	if err != nil {
		return nil, sidecars, errors.Newf(
			"failed to get validator by pubkey: %w",
			err,
		)
	}

	// Compute the state root for the block.
	// TODO: IMPLEMENT RN THIS DOES NOTHING.
	stateRoot, err := s.computeStateRoot(ctx)
	if err != nil {
		return nil, sidecars, errors.Newf(
			"failed to compute state root: %w",
			err,
		)
	}

	// Create a new empty block from the current state.
	blk, err := consensus.EmptyBeaconBlock(
		slot,
		proposerIndex,
		parentBlockRoot,
		stateRoot,
		s.chainSpec.ActiveForkVersionForSlot(slot),
	)
	if err != nil {
		return nil, sidecars, err
	}

	// The latest execution payload header, will be from the previous block
	// during the block building phase.
	parentExecutionPayload, err := st.GetLatestExecutionPayloadHeader()
	if err != nil {
		return nil, sidecars, err
	}

	// Get the payload for the block.
	envelope, err := s.localBuilder.RetrieveOrBuildPayload(
		ctx,
		st,
		slot,
		parentBlockRoot,
		parentExecutionPayload.GetBlockHash(),
	)
	if err != nil {
		return blk, sidecars, errors.Newf(
			"failed to get block root at index: %w",
			err,
		)
	} else if envelope == nil {
		return nil, sidecars, ErrNilPayload
	}

	// Assemble a new block with the payload.
	body := blk.GetBody()
	if body.IsNil() {
		return nil, sidecars, ErrNilBlkBody
	}

	// If we get returned a nil blobs bundle, we should return an error.
	// TODO: allow external block builders to override the payload.
	blobsBundle := envelope.GetBlobsBundle()
	if blobsBundle == nil {
		return nil, sidecars, ErrNilBlobsBundle
	}

	// Dequeue deposits from the state.
	deposits, err := s.ds.ExpectedDeposits(
		s.chainSpec.MaxDepositsPerBlock(),
	)
	if err != nil {
		return nil, sidecars, err
	}

	payload := envelope.GetExecutionPayload()
	if payload == nil || payload.IsNil() {
		return nil, sidecars, ErrNilPayload
	}

	// Set the KZG commitments on the block body.
	body.SetBlobKzgCommitments(blobsBundle.GetCommitments())

	// Set the deposits on the block body.
	body.SetDeposits(deposits)

	// TODO: assemble real eth1data.
	body.SetEth1Data(&consensus.Eth1Data{
		DepositRoot:  primitives.Bytes32{},
		DepositCount: 0,
		BlockHash:    common.ExecutionHash{},
	})

	// Set the reveal on the block body.
	body.SetRandaoReveal(reveal)

	// Set the execution data.
	if err = body.SetExecutionData(payload); err != nil {
		return nil, sidecars, err
	}

	// Build the sidecars for the block.
	blobSidecars, err := s.blobFactory.BuildSidecars(blk, blobsBundle)
	if err != nil {
		return nil, sidecars, err
	}

	s.logger.Info("finished assembling beacon block 🛟",
		"slot", slot, "deposits", len(deposits))

	// Set the execution payload on the block body.
	return blk, blobSidecars, nil
}