// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package export

import (
	"image"
	"testing"
)

func TestGIFRequiresCaptureSource(t *testing.T) {
	t.Parallel()

	_, err := GIF(t.TempDir() + "/empty.gif")
	if err == nil {
		t.Fatal("expected error when neither WithFrameRate nor WithChannel is set")
	}
}

func TestGIFRejectsMutuallyExclusiveSources(t *testing.T) {
	t.Parallel()

	src := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	ch := make(<-chan Frame)
	_, err := GIF(t.TempDir()+"/bad.gif",
		WithFrameRate(30, false, func() image.Image { return src }),
		WithChannel(ch),
	)
	if err == nil {
		t.Fatal("expected error for WithFrameRate and WithChannel together")
	}
}

func TestGIFRejectsInvalidFrameRate(t *testing.T) {
	t.Parallel()

	src := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	fn := func() image.Image { return src }

	for _, fps := range []int{0, -1, MaxFrameRate + 1} {
		_, err := GIF(t.TempDir()+"/bad.gif", WithFrameRate(fps, false, fn))
		if err == nil {
			t.Fatalf("fps %d: expected error", fps)
		}
	}
}

func TestGIFRejectsNilFrameFunction(t *testing.T) {
	t.Parallel()

	_, err := GIF(t.TempDir()+"/bad.gif", WithFrameRate(30, false, nil))
	if err == nil {
		t.Fatal("expected error for nil WithFrameRate callback")
	}
}

func TestDefaultGIFOptimizeAll(t *testing.T) {
	t.Parallel()

	ch := make(<-chan Frame)
	cfg, err := collectOptions(formatGIF, WithChannel(ch))
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

	cfg, err := collectOptions(formatPNG)
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

	cfg, err := collectOptions(formatPNG, WithOptimize(OptimizeAll))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.optimize != OptimizeColorQuantization {
		t.Fatalf("optimize = %#x, want OptimizeColorQuantization only (%#x)", cfg.optimize, OptimizeColorQuantization)
	}
}
