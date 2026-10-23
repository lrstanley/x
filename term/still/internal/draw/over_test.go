// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package draw_test

import (
	"image"
	"image/color"
	"math"
	"math/rand/v2"
	"testing"

	idraw "github.com/lrstanley/x/term/still/internal/draw"
)

// overReference is non-premultiplied source-over in floating point.
func overReference(dst, src color.NRGBA, cov uint8) color.NRGBA {
	sa := float64(cov) / 255 * float64(src.A) / 255
	da := float64(dst.A) / 255
	oa := sa + da*(1-sa)
	if oa == 0 {
		return color.NRGBA{}
	}
	ch := func(s, d uint8) uint8 {
		return uint8(math.Round((float64(s)*sa + float64(d)*da*(1-sa)) / oa))
	}
	return color.NRGBA{R: ch(src.R, dst.R), G: ch(src.G, dst.G), B: ch(src.B, dst.B), A: uint8(math.Round(oa * 255))}
}

// closeTo reports whether a and b are the same color within rounding. The
// color of a fully transparent pixel does not matter.
func closeTo(a, b color.NRGBA) bool {
	if a.A == 0 && b.A == 0 {
		return true
	}
	d := func(x, y uint8) bool { return x-y <= 1 || y-x <= 1 }
	return d(a.R, b.R) && d(a.G, b.G) && d(a.B, b.B) && d(a.A, b.A)
}

func TestOverCoverage(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(3, 4))
	for _, src := range []color.NRGBA{{R: 10, G: 132, B: 255, A: 255}, {R: 255, G: 69, B: 58, A: 140}} {
		area := image.Rect(2, 1, 40, 4)
		cov := make([]uint8, area.Dx())
		for i := range cov {
			cov[i] = []uint8{0, 255, uint8(rng.IntN(256)), uint8(rng.IntN(256))}[i%4]
		}
		// Destination alphas: opaque, empty, and translucent.
		img := image.NewNRGBA(image.Rect(0, 0, 40, 5))
		for i := 0; i < len(img.Pix); i += 4 {
			a := []uint8{255, 0, uint8(1 + rng.IntN(254))}[(i/4)%3]
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = uint8(rng.IntN(256)), uint8(rng.IntN(256)), uint8(rng.IntN(256)), a
		}
		before := image.NewNRGBA(img.Rect)
		copy(before.Pix, img.Pix)

		idraw.OverCoverage(img, area, src, cov)

		for y := range 5 {
			for x := range 40 {
				want := before.NRGBAAt(x, y)
				if image.Pt(x, y).In(area) {
					want = overReference(want, src, cov[x-area.Min.X])
				}
				if got := img.NRGBAAt(x, y); !closeTo(got, want) {
					t.Fatalf("src %v pixel (%d,%d) = %v, want %v (dst was %v)", src, x, y, got, want, before.NRGBAAt(x, y))
				}
			}
		}
	}
}

// TestOverCoverageClipped checks that an area partly outside the image uses
// the coverage of the columns that are inside.
func TestOverCoverageClipped(t *testing.T) {
	t.Parallel()

	src := color.NRGBA{R: 200, A: 255}
	cov := []uint8{255, 255, 0, 255, 255, 255}
	img := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	idraw.OverCoverage(img, image.Rect(-2, -1, 4, 5), src, cov)
	for y := range 2 {
		for x := range 4 {
			want := color.NRGBA{}
			if cov[x+2] == 255 {
				want = src
			}
			if got := img.NRGBAAt(x, y); got != want {
				t.Errorf("pixel (%d,%d) = %v, want %v", x, y, got, want)
			}
		}
	}
	idraw.OverCoverage(img, image.Rect(10, 10, 12, 12), src, []uint8{255, 255}) // outside: no-op
}

// TestOverCoverageGeneric checks that a destination that is not an NRGBA
// gets the same pixels, within rounding, as one that is.
func TestOverCoverageGeneric(t *testing.T) {
	t.Parallel()

	src := color.NRGBA{R: 10, G: 132, B: 255, A: 255}
	area := image.Rect(1, 1, 9, 3)
	cov := []uint8{0, 40, 90, 140, 190, 230, 255, 17}
	a, b := image.NewNRGBA(image.Rect(0, 0, 10, 4)), image.NewRGBA(image.Rect(0, 0, 10, 4))
	for y := range 4 {
		for x := range 10 {
			c := color.NRGBA{R: uint8(x * 20), G: uint8(y * 50), B: 99, A: 255}
			a.SetNRGBA(x, y, c)
			b.SetRGBA(x, y, color.RGBAModel.Convert(c).(color.RGBA)) //nolint:errcheck // RGBAModel returns color.RGBA
		}
	}
	idraw.OverCoverage(a, area, src, cov)
	idraw.OverCoverage(b, area, src, cov)
	for y := range 4 {
		for x := range 10 {
			want, got := a.NRGBAAt(x, y), color.NRGBAModel.Convert(b.RGBAAt(x, y)).(color.NRGBA) //nolint:errcheck // NRGBAModel returns color.NRGBA
			if !closeTo(got, want) {
				t.Fatalf("pixel (%d,%d): generic %v, NRGBA %v", x, y, got, want)
			}
		}
	}
}
