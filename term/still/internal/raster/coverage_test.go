// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package raster

import (
	"image"
	"image/color"
	"testing"
)

func TestCoverageAccumulateAndComposite(t *testing.T) {
	t.Parallel()

	var pass Pass
	bounds := image.Rect(0, 0, 4, 4)
	pass.Reset(bounds)

	pass.cov.Accumulate(1, 1, 128, color.NRGBA{R: 255, A: 255})
	pass.cov.Accumulate(1, 1, 64, color.NRGBA{G: 255, A: 255})
	pass.cov.Accumulate(-1, 1, 255, color.NRGBA{B: 255, A: 255})

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

	var pass Pass
	pass.Reset(image.Rect(0, 0, 2, 2))
	pass.AppendDecoration(image.Rect(0, 0, 1, 1), nil, color.NRGBA{A: 0xff})

	pass.cov.Accumulate(0, 0, 255, color.NRGBA{R: 1, A: 255})
	dst := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	pass.Composite(dst)
	if got := dst.NRGBAAt(0, 0).R; got != 1 {
		t.Fatalf("first reset composite R = %d, want 1", got)
	}

	pass.Reset(image.Rect(0, 0, 3, 3))
	pass.cov.Accumulate(2, 2, 255, color.NRGBA{G: 2, A: 255})
	dst = image.NewNRGBA(image.Rect(0, 0, 3, 3))
	pass.Composite(dst)
	if got := dst.NRGBAAt(2, 2).G; got != 2 {
		t.Fatalf("second reset composite G = %d, want 2", got)
	}
}

func TestPassResetReusesCoverageBuffer(t *testing.T) {
	var pass Pass
	bounds := image.Rect(0, 0, 500, 300)
	pass.Reset(bounds)
	if n := testing.AllocsPerRun(50, func() { pass.Reset(bounds) }); n != 0 {
		t.Fatalf("allocs per reset = %v, want 0", n)
	}
}

func TestCoverageMaxAlpha(t *testing.T) {
	t.Parallel()

	var pass Pass
	pass.Reset(image.Rect(0, 0, 2, 2))

	pass.cov.Accumulate(0, 0, 100, color.NRGBA{R: 1, A: 255})
	pass.cov.Accumulate(0, 0, 255, color.NRGBA{R: 255, A: 255})

	dst := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	dst.SetNRGBA(0, 0, color.NRGBA{A: 255})
	pass.Composite(dst)
	if got := dst.NRGBAAt(0, 0).R; got != 255 {
		t.Fatalf("max alpha fg R = %d, want 255", got)
	}
}

func TestCoverageResetClearsPreviousInk(t *testing.T) {
	t.Parallel()

	var pass Pass
	bounds := image.Rect(1, 2, 6, 5)
	pass.Reset(bounds)
	pass.cov.Accumulate(2, 3, 255, color.NRGBA{R: 255, A: 255})
	pass.cov.Accumulate(4, 3, 128, color.NRGBA{G: 255, A: 255})

	pass.Reset(bounds)
	dst := image.NewNRGBA(bounds)
	bg := color.NRGBA{R: 1, G: 2, B: 3, A: 255}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			dst.SetNRGBA(x, y, bg)
		}
	}
	pass.Composite(dst)
	if got := dst.NRGBAAt(2, 3); got != bg {
		t.Fatalf("stale pixel after same-bounds reset: %+v", got)
	}

	pass.cov.Accumulate(3, 4, 255, color.NRGBA{B: 9, A: 255})
	pass.Composite(dst)
	if got := dst.NRGBAAt(2, 3); got != bg {
		t.Fatalf("old ink returned: %+v", got)
	}
	if got := dst.NRGBAAt(3, 4).B; got != 9 {
		t.Fatalf("new ink B = %d, want 9", got)
	}
}

func TestCompositePartialBlendExact(t *testing.T) {
	t.Parallel()

	alphas := []uint8{1, 2, 127, 128, 254}
	channels := []uint8{0, 1, 127, 128, 254, 255}
	for _, a := range alphas {
		for _, fg := range channels {
			for _, bg := range channels {
				var cov Coverage
				cov.reset(image.Rect(0, 0, 1, 1))
				cov.Accumulate(0, 0, a, color.NRGBA{R: fg, G: fg, B: fg, A: 255})
				dst := image.NewNRGBA(image.Rect(0, 0, 1, 1))
				dst.SetNRGBA(0, 0, color.NRGBA{R: bg, G: bg, B: bg, A: 255})
				cov.Composite(dst)
				inv := 255 - int(a)
				want := uint8((int(fg)*int(a) + int(bg)*inv) / 255)
				got := dst.NRGBAAt(0, 0)
				if got.R != want || got.G != want || got.B != want || got.A != 255 {
					t.Fatalf("a=%d fg=%d bg=%d got=%+v want channel %d", a, fg, bg, got, want)
				}
			}
		}
	}
}

func TestCompositeSkipsZeroGaps(t *testing.T) {
	t.Parallel()

	var cov Coverage
	cov.reset(image.Rect(0, 0, 16, 1))
	cov.Accumulate(0, 0, 255, color.NRGBA{R: 255, A: 255})
	cov.Accumulate(8, 0, 128, color.NRGBA{G: 255, A: 255})
	dst := image.NewNRGBA(image.Rect(0, 0, 16, 1))
	bg := color.NRGBA{B: 10, A: 255}
	for x := range 16 {
		dst.SetNRGBA(x, 0, bg)
	}
	cov.Composite(dst)
	if got := dst.NRGBAAt(0, 0); got != (color.NRGBA{R: 255, A: 255}) {
		t.Fatalf("opaque pixel = %+v", got)
	}
	if got := dst.NRGBAAt(4, 0); got != bg {
		t.Fatalf("gap pixel = %+v, want %+v", got, bg)
	}
	got := dst.NRGBAAt(8, 0)
	if got.G != 128 || got.B != 4 || got.R != 0 || got.A != 255 {
		t.Fatalf("partial pixel = %+v, want G=128 B=4", got)
	}
}

func TestAccumulateGlyphMaskIndexWalk(t *testing.T) {
	t.Parallel()

	var cov Coverage
	bounds := image.Rect(2, 4, 20, 7)
	cov.reset(bounds)
	// Nonzero at columns 0, 7, and 8 of a 10px row, plus a short second row.
	pix := []uint8{
		9, 0, 0, 0, 0, 0, 0, 40, 255, 3, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0, 7, 0, 0, 0,
	}
	fg := color.NRGBA{R: 10, G: 20, B: 30, A: 255}
	accumulateGlyphMask(&cov, image.Rect(2, 4, 12, 6), pix, 12, fg)
	if cov.rowLo[0] != 2 || cov.rowHi[0] != 11 {
		t.Fatalf("row 0 span = %d..%d, want 2..11", cov.rowLo[0], cov.rowHi[0])
	}
	if cov.rowLo[1] != 10 || cov.rowHi[1] != 10 {
		t.Fatalf("row 1 span = %d..%d, want 10..10", cov.rowLo[1], cov.rowHi[1])
	}

	// Lower alpha must not replace a higher sample. A new sample on an empty
	// pixel still writes, and a zero mask byte stays empty. x=4 is inside
	// the existing inclusive span, so the span does not move.
	again := []uint8{0, 0, 1, 0, 0, 0, 0, 0, 10, 0}
	accumulateGlyphMask(&cov, image.Rect(2, 4, 12, 5), again, 10, color.NRGBA{R: 255, A: 255})
	if cov.rowLo[0] != 2 || cov.rowHi[0] != 11 {
		t.Fatalf("span after second mask = %d..%d, want 2..11", cov.rowLo[0], cov.rowHi[0])
	}
	if got := uint8(cov.pixels[cov.index(10, 4)]); got != 255 {
		t.Fatalf("max alpha = %d, want 255", got)
	}
	if got := uint8(cov.pixels[cov.index(3, 4)]); got != 0 {
		t.Fatalf("zero mask byte wrote coverage: %d", got)
	}
	if got := uint8(cov.pixels[cov.index(4, 4)]); got != 1 {
		t.Fatalf("empty pixel alpha = %d, want 1", got)
	}

	dst := image.NewNRGBA(bounds)
	bg := color.NRGBA{R: 1, G: 2, B: 3, A: 255}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			dst.SetNRGBA(x, y, bg)
		}
	}
	cov.Composite(dst)
	if got := dst.NRGBAAt(3, 4); got != bg {
		t.Fatalf("zero mask pixel changed dst: %+v", got)
	}
	if got := dst.NRGBAAt(10, 4).R; got != 10 {
		t.Fatalf("opaque mask R = %d, want 10", got)
	}

	cov.reset(bounds)
	dst.SetNRGBA(10, 4, bg)
	cov.Composite(dst)
	if got := dst.NRGBAAt(10, 4); got != bg {
		t.Fatalf("ink survived reset: %+v", got)
	}
}
