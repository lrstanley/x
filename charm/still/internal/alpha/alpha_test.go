// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package alpha_test

import (
	"image"
	"image/color"
	"testing"

	"github.com/lrstanley/x/charm/still/internal/alpha"
)

func TestInkBounds(t *testing.T) {
	t.Parallel()

	a := image.NewAlpha(image.Rect(0, 0, 4, 4))
	if _, ok := alpha.InkBounds(a); ok {
		t.Fatal("empty alpha should have no ink")
	}

	a.SetAlpha(1, 1, color.Alpha{A: 128})
	a.SetAlpha(2, 2, color.Alpha{A: 255})
	bounds, ok := alpha.InkBounds(a)
	if !ok {
		t.Fatal("expected ink bounds")
	}
	want := image.Rect(1, 1, 3, 3)
	if bounds != want {
		t.Fatalf("bounds = %v, want %v", bounds, want)
	}
}

func TestSetMax(t *testing.T) {
	t.Parallel()

	dst := image.NewAlpha(image.Rect(0, 0, 2, 2))
	dst.SetAlpha(0, 0, color.Alpha{A: 100})

	alpha.SetMax(dst, 0, 0, 50)
	if got := dst.AlphaAt(0, 0).A; got != 100 {
		t.Fatalf("lower alpha replaced value: got %d", got)
	}
	alpha.SetMax(dst, 0, 0, 200)
	if got := dst.AlphaAt(0, 0).A; got != 200 {
		t.Fatalf("higher alpha not stored: got %d", got)
	}
	alpha.SetMax(dst, -1, 0, 255)
	if got := dst.AlphaAt(0, 0).A; got != 200 {
		t.Fatalf("out of bounds write changed value: got %d", got)
	}
}

func TestDilate(t *testing.T) {
	t.Parallel()

	src := image.NewAlpha(image.Rect(0, 0, 3, 3))
	src.SetAlpha(1, 1, color.Alpha{A: 255})

	out := alpha.Dilate(src, 1, 0)
	if out == src {
		t.Fatal("Dilate returned same pointer")
	}
	if out.AlphaAt(2, 1).A != 255 {
		t.Fatalf("horizontal dilate missing at (2,1): got %d", out.AlphaAt(2, 1).A)
	}

	unchanged := alpha.Dilate(src, 0, 0)
	if unchanged != src {
		t.Fatal("zero radius should return original alpha")
	}
}

func TestDilateX(t *testing.T) {
	t.Parallel()

	src := image.NewAlpha(image.Rect(0, 0, 3, 2))
	src.SetAlpha(0, 0, color.Alpha{A: 200})

	out := alpha.DilateX(src, 2)
	if out.Bounds().Dx() != 5 {
		t.Fatalf("width = %d, want 5", out.Bounds().Dx())
	}
	if out.AlphaAt(2, 0).A != 200 {
		t.Fatalf("dilateX missing at (2,0): got %d", out.AlphaAt(2, 0).A)
	}
}

func TestRasterGlyph(t *testing.T) {
	t.Parallel()

	mask := image.NewAlpha(image.Rect(0, 0, 2, 2))
	mask.SetAlpha(0, 0, color.Alpha{A: 255})
	dr := image.Rect(5, 5, 7, 7)

	out := alpha.RasterGlyph(dr, mask, image.Point{})
	if out.Bounds().Dx() != 2 || out.Bounds().Dy() != 2 {
		t.Fatalf("bounds = %v, want 2x2", out.Bounds())
	}
	if out.AlphaAt(0, 0).A != 255 {
		t.Fatalf("alpha at origin = %d, want 255", out.AlphaAt(0, 0).A)
	}
}

func TestAt(t *testing.T) {
	t.Parallel()

	nrgba := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	nrgba.SetNRGBA(0, 0, color.NRGBA{R: 10, G: 20, B: 30, A: 128})
	if got := alpha.At(nrgba, image.Pt(0, 0)); got != 128 {
		t.Fatalf("At() = %d, want 128", got)
	}
}
