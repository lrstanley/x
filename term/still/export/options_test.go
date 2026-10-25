// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package export

import (
	"image"
	"io"
	"testing"
)

func TestRecordRequiresCaptureSource(t *testing.T) {
	t.Parallel()

	err := recordErr()
	if err == nil {
		t.Fatal("expected error when neither WithFrameRate nor WithChannel is set")
	}
}

func TestRecordRejectsMutuallyExclusiveSources(t *testing.T) {
	t.Parallel()

	src := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	ch := make(<-chan Frame)
	err := recordErr(
		WithFrameRate(30, false, func() image.Image { return src }),
		WithChannel(ch),
	)
	if err == nil {
		t.Fatal("expected error for WithFrameRate and WithChannel together")
	}
}

func TestDelayGridValidation(t *testing.T) {
	t.Parallel()

	src := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	fn := func() image.Image { return src }

	for _, fps := range []int{0, -1, MaxFrameRate + 1} {
		if err := recordErr(WithChannel(make(<-chan Frame)), WithDelayGrid(fps)); err == nil {
			t.Fatalf("expected error for WithDelayGrid(%d)", fps)
		}
	}
	if err := recordErr(WithFrameRate(10, false, fn), WithDelayGrid(20)); err == nil {
		t.Fatal("expected error for WithFrameRate and WithDelayGrid together")
	}
}

func TestRecordRejectsInvalidFrameRate(t *testing.T) {
	t.Parallel()

	src := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	fn := func() image.Image { return src }

	for _, fps := range []int{0, -1, MaxFrameRate + 1} {
		err := recordErr(WithFrameRate(fps, false, fn))
		if err == nil {
			t.Fatalf("fps %d: expected error", fps)
		}
	}
}

func TestGIFRejectsLoopCount(t *testing.T) {
	t.Parallel()

	ch := make(<-chan Frame)
	for _, n := range []int{LoopOnce - 1, maxGIFLoopCount + 1} {
		err := recordErr(WithChannel(ch), WithLoopCount(n))
		if err == nil {
			t.Fatalf("loop count %d: expected error", n)
		}
	}
}

func TestRecordRejectsNilFrameFunction(t *testing.T) {
	t.Parallel()

	err := recordErr(WithFrameRate(30, false, nil))
	if err == nil {
		t.Fatal("expected error for nil WithFrameRate callback")
	}
}

// recordErr returns the error of starting a [Recorder] into a GIF that is
// thrown away, closing the recorder if it starts.
func recordErr(opts ...Option) error {
	enc, err := NewGIF(io.Discard, opts...)
	if err != nil {
		return err
	}
	rec, err := Record(enc, opts...)
	if err != nil {
		return err
	}
	return rec.Close()
}

func TestDefaultGIFOptimizeAll(t *testing.T) {
	t.Parallel()

	ch := make(<-chan Frame)
	cfg, err := collectOptions(validateGIF, WithChannel(ch))
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
