// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package sink

import (
	"io"
	"testing"
)

func TestGIFRejectsLoopCount(t *testing.T) {
	t.Parallel()

	for _, n := range []int{LoopOnce - 1, maxGIFLoopCount + 1} {
		if _, err := NewGIF(io.Discard, WithLoopCount(n)); err == nil {
			t.Fatalf("loop count %d: expected error", n)
		}
	}
}

func TestDefaultOptimizeAll(t *testing.T) {
	t.Parallel()

	if got := collectOptions(nil).optimize; got != OptimizeAll {
		t.Fatalf("default optimize = %#x, want OptimizeAll (%#x)", got, OptimizeAll)
	}
}
