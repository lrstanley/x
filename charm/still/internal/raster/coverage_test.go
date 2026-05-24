// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package raster_test

import (
	"image"
	"image/color"
	"testing"

	"github.com/lrstanley/x/charm/still/internal/raster"
)

func TestCoverageAccumulateAndComposite(t *testing.T) {
	t.Parallel()

	var pass raster.Pass
	bounds := image.Rect(0, 0, 4, 4)
	pass.Reset(bounds)

	pass.Coverage().Accumulate(1, 1, 128, color.NRGBA{R: 255, A: 255})
	pass.Coverage().Accumulate(1, 1, 64, color.NRGBA{G: 255, A: 255})
	pass.Coverage().Accumulate(-1, 1, 255, color.NRGBA{B: 255, A: 255})

	dst := image.NewNRGBA(bounds)
	dst.SetNRGBA(1, 1, color.NRGBA{G: 255, A: 255})

	pass.Composite(dst)
	got := dst.NRGBAAt(1, 1)
	if got.R == 0 || got.G == 0 {
		t.Fatalf("composite did not blend fg over bg: %+v", got)
	}
	if dst.NRGBAAt(0, 0).A != 0 {
		t.Fatal("untouched pixel should remain transparent")
	}
}

func TestPassAppendAndReset(t *testing.T) {
	t.Parallel()

	var pass raster.Pass
	pass.Reset(image.Rect(0, 0, 2, 2))
	pass.AppendDecoration(image.Rect(0, 0, 1, 1), nil, color.White)

	pass.Coverage().Accumulate(0, 0, 255, color.NRGBA{R: 1, A: 255})
	dst := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	pass.Composite(dst)
	if got := dst.NRGBAAt(0, 0).R; got != 1 {
		t.Fatalf("first reset composite R = %d, want 1", got)
	}

	pass.Reset(image.Rect(0, 0, 3, 3))
	pass.Coverage().Accumulate(2, 2, 255, color.NRGBA{G: 2, A: 255})
	dst = image.NewNRGBA(image.Rect(0, 0, 3, 3))
	pass.Composite(dst)
	if got := dst.NRGBAAt(2, 2).G; got != 2 {
		t.Fatalf("second reset composite G = %d, want 2", got)
	}
}

func TestCoverageMaxAlpha(t *testing.T) {
	t.Parallel()

	var pass raster.Pass
	pass.Reset(image.Rect(0, 0, 2, 2))

	pass.Coverage().Accumulate(0, 0, 100, color.NRGBA{R: 1, A: 255})
	pass.Coverage().Accumulate(0, 0, 255, color.NRGBA{R: 255, A: 255})

	dst := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	dst.SetNRGBA(0, 0, color.NRGBA{A: 255})
	pass.Composite(dst)
	if got := dst.NRGBAAt(0, 0).R; got != 255 {
		t.Fatalf("max alpha fg R = %d, want 255", got)
	}
}
