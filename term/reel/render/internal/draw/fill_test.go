// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package draw_test

import (
	"image"
	"image/color"
	"image/draw"
	"testing"

	idraw "github.com/lrstanley/x/term/reel/render/internal/draw"
)

func TestFillAndOverlay(t *testing.T) {
	t.Parallel()

	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	idraw.Fill(img, image.Rect(1, 1, 3, 3), color.NRGBA{R: 255, A: 255})
	if got := img.NRGBAAt(2, 2); got.R != 255 {
		t.Fatalf("Fill center = %+v, want red", got)
	}
	if got := img.NRGBAAt(0, 0); got.R != 0 {
		t.Fatalf("Fill outside = %+v, want untouched", got)
	}

	idraw.Overlay(img, image.Rect(0, 0, 2, 2), color.NRGBA{A: 128})
	if got := img.NRGBAAt(0, 0); got.A == 0 {
		t.Fatal("Overlay should change alpha at (0,0)")
	}
}

// TestOverlayMatchesDraw checks that the NRGBA path of Overlay blends as
// [draw.Draw] with [draw.Over] does, to within rounding, and clips to the
// image.
func TestOverlayMatchesDraw(t *testing.T) {
	t.Parallel()

	base := image.NewNRGBA(image.Rect(0, 0, 4, 1))
	for x, c := range []color.NRGBA{{R: 200, G: 10, B: 90, A: 255}, {R: 30, G: 220, B: 60, A: 128}, {}, {R: 255, G: 255, B: 255, A: 255}} {
		base.SetNRGBA(x, 0, c)
	}
	for _, col := range []color.NRGBA{{R: 255, G: 255, B: 255, A: 77}, {A: 41}, {R: 10, G: 200, B: 30, A: 255}} {
		got := image.NewNRGBA(base.Rect)
		copy(got.Pix, base.Pix)
		idraw.Overlay(got, image.Rect(-2, -2, 9, 9), col)
		want := image.NewNRGBA(base.Rect)
		copy(want.Pix, base.Pix)
		draw.Draw(want, want.Rect, image.NewUniform(col), image.Point{}, draw.Over)
		for i := range got.Pix {
			if d := int(got.Pix[i]) - int(want.Pix[i]); d < -1 || d > 1 {
				t.Fatalf("Overlay(%v) pixel %d channel %d = %d, want %d", col, i/4, i%4, got.Pix[i], want.Pix[i])
			}
		}
	}
}
