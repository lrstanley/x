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

func TestDefaultGIFOptimizeAll(t *testing.T) {
	t.Parallel()

	cfg, err := collectOptions(validateGIF)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.optimize != OptimizeAll {
		t.Fatalf("default optimize = %#x, want OptimizeAll (%#x)", cfg.optimize, OptimizeAll)
	}
	if cfg.optimizeSet {
		t.Fatal("optimizeSet should be false when WithOptimize is omitted")
	}
}

func TestDefaultPNGOptimizeAll(t *testing.T) {
	t.Parallel()

	cfg, err := collectOptions(validatePNG)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.optimize != OptimizeColorQuantization {
		t.Fatalf("default optimize = %#x, want OptimizeColorQuantization (%#x)", cfg.optimize, OptimizeColorQuantization)
	}
	if cfg.optimizeSet {
		t.Fatal("optimizeSet should be false when WithOptimize is omitted")
	}
}

func TestPNGStripsGIFOnlyOptimizeFlags(t *testing.T) {
	t.Parallel()

	cfg, err := collectOptions(validatePNG, WithOptimize(OptimizeAll))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.optimize != OptimizeColorQuantization {
		t.Fatalf("optimize = %#x, want OptimizeColorQuantization only (%#x)", cfg.optimize, OptimizeColorQuantization)
	}
}
