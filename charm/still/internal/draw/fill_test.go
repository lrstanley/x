// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package draw_test

import (
	"image"
	"image/color"
	"testing"

	idraw "github.com/lrstanley/x/charm/still/internal/draw"
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
