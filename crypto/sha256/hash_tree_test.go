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

package sha256_test

import (
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/itsdevbear/bolaris/crypto/sha256"
	"github.com/protolambda/ztyp/tree"
	"github.com/stretchr/testify/require"
)

func Test_HashTreeRootEqualInputs(t *testing.T) {
	// Test with slices of varying sizes to ensure robustness across different conditions
	sliceSizes := []int{16, 32, 64}
	for _, size := range sliceSizes {
		t.Run(fmt.Sprintf("Size%d", size*sha256.MinParallelizationSize), func(t *testing.T) {
			largeSlice := make([]tree.Root, size*sha256.MinParallelizationSize)
			secondLargeSlice := make([]tree.Root, size*sha256.MinParallelizationSize)
			// Assuming hash reduces size by half
			hash1 := make([]tree.Root, size*sha256.MinParallelizationSize/2)
			var hash2 []tree.Root
			var err error

			wg := sync.WaitGroup{}
			wg.Add(1)
			go func() {
				defer wg.Done()
				var tempHash []tree.Root
				tempHash, err = sha256.HashTreeRoot(largeSlice)
				copy(hash1, tempHash)
			}()
			wg.Wait()
			require.NoError(t, err)

			hash2, err = sha256.HashTreeRoot(secondLargeSlice)
			require.NoError(t, err)

			require.Equal(t, len(hash1), len(hash2), "Hash lengths should be equal")
			for i, r := range hash1 {
				require.Equal(t, r, hash2[i], fmt.Sprintf("Hash mismatch at index %d", i))
			}
		})
	}
}

func Test_GoHashTreeHashConformance(t *testing.T) {
	// Define a test table with various input sizes,
	// including ones above and below MinParallelizationSize
	testCases := []struct {
		name    string
		size    int
		wantErr bool
	}{
		{"BelowMinParallelizationSize", sha256.MinParallelizationSize / 2, false},
		{"AtMinParallelizationSize", sha256.MinParallelizationSize, false},
		{"AboveMinParallelizationSize", sha256.MinParallelizationSize * 2, false},
		{"SmallSize", 16, false},
		{"MediumSize", 64, false},
		{"LargeSize", 128, false},
		{"TestRemainderStartIndexSmall", sha256.MinParallelizationSize + 6, false},
		{"TestRemainderStartIndexBig", sha256.MinParallelizationSize - 2, false},
		{"TestOddLength", sha256.MinParallelizationSize + 1, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			inputList := make([]tree.Root, tc.size)
			// Fill inputList with pseudo-random data
			randSource := rand.NewSource(time.Now().UnixNano())
			randGen := rand.New(randSource)
			for i := range inputList {
				for j := range inputList[i] {
					inputList[i][j] = byte(randGen.Intn(256))
				}
			}
			requireGoHashTreeEquivalence(t, inputList, runtime.GOMAXPROCS(0)-1, tc.wantErr)
		})
	}
}