// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package draw_test

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"math/rand/v2"
	"slices"
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

// TestOverAt checks that OverAt blends one pixel the way OverCoverage does,
// into NRGBA and other destinations, and ignores pixels outside the image.
func TestOverAt(t *testing.T) {
	t.Parallel()

	src := color.NRGBA{R: 255, G: 69, B: 58, A: 200}
	for _, dst := range []color.NRGBA{{R: 20, G: 30, B: 40, A: 255}, {R: 200, G: 10, B: 90, A: 100}, {}} {
		for _, cov := range []uint8{0, 1, 128, 255} {
			want := image.NewNRGBA(image.Rect(0, 0, 3, 1))
			want.SetNRGBA(1, 0, dst)
			idraw.OverCoverage(want, image.Rect(1, 0, 2, 1), src, []uint8{cov})

			got := image.NewNRGBA(image.Rect(0, 0, 3, 1))
			got.SetNRGBA(1, 0, dst)
			idraw.OverAt(got, 1, 0, src, cov)
			idraw.OverAt(got, 5, 0, src, 255)
			idraw.OverAt(got, -1, 0, src, 255)
			if got.NRGBAAt(1, 0) != want.NRGBAAt(1, 0) || got.NRGBAAt(0, 0) != (color.NRGBA{}) || got.NRGBAAt(2, 0) != (color.NRGBA{}) {
				t.Errorf("dst %v cov %d: NRGBA got %v, want %v", dst, cov, got.Pix, want.Pix)
			}

			// A destination that is not *image.NRGBA takes the generic path.
			other := image.NewNRGBA(image.Rect(0, 0, 3, 1))
			other.SetNRGBA(1, 0, dst)
			idraw.OverAt(wrappedImage{other}, 1, 0, src, cov)
			if other.NRGBAAt(1, 0) != want.NRGBAAt(1, 0) {
				t.Errorf("dst %v cov %d: generic got %v, want %v", dst, cov, other.NRGBAAt(1, 0), want.NRGBAAt(1, 0))
			}
		}
	}
}

// wrappedImage hides the concrete image type from type switches.
type wrappedImage struct{ draw.Image }

// TestOverAtRGBA checks that OverAt on an RGBA destination gives the same
// pixels as converting through color.NRGBAModel and Set, and that OverRow
// matches OverAt on each pixel.
func TestOverAtRGBA(t *testing.T) {
	t.Parallel()

	src := color.NRGBA{R: 255, G: 69, B: 58, A: 200}
	for _, dst := range []color.RGBA{{R: 20, G: 30, B: 40, A: 255}, {R: 80, G: 10, B: 60, A: 100}, {}} {
		for _, cov := range []uint8{1, 128, 255} {
			got := image.NewRGBA(image.Rect(0, 0, 3, 1))
			want := image.NewRGBA(got.Rect)
			for x := range 3 {
				got.SetRGBA(x, 0, dst)
				want.SetRGBA(x, 0, dst)
			}
			idraw.OverRow(got, -2, 5, 0, src, cov)
			for x := range 3 {
				px, _ := color.NRGBAModel.Convert(want.At(x, 0)).(color.NRGBA)
				ref := image.NewNRGBA(image.Rect(0, 0, 1, 1))
				ref.SetNRGBA(0, 0, px)
				idraw.OverCoverage(ref, ref.Rect, src, []uint8{cov})
				want.Set(x, 0, ref.NRGBAAt(0, 0))
			}
			if !slices.Equal(got.Pix, want.Pix) {
				t.Errorf("dst %v cov %d: got %v, want %v", dst, cov, got.Pix, want.Pix)
			}
		}
	}
}

// TestOverAtRGBANoAllocs checks that blending into an RGBA destination does
// not box colors.
func TestOverAtRGBANoAllocs(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	col := color.NRGBA{R: 10, G: 200, B: 30, A: 255}
	if n := testing.AllocsPerRun(100, func() {
		idraw.OverAt(img, 1, 1, col, 128)
		idraw.RecolorAt(img, 2, 2, col, 0.5)
	}); n != 0 {
		t.Fatalf("OverAt and RecolorAt allocate %v times per call on an RGBA image", n)
	}
}

// TestRecolorAt checks that a translucent pixel is recolored, keeping its
// alpha, the same on NRGBA and other destinations, and that opaque and
// transparent pixels are not.
func TestRecolorAt(t *testing.T) {
	t.Parallel()

	col := color.NRGBA{R: 200, G: 100, B: 0, A: 255}
	for _, tc := range []struct {
		px       color.NRGBA
		k        float64
		want     color.NRGBA
		recolors bool
	}{
		{color.NRGBA{R: 0, G: 0, B: 200, A: 255}, 0.5, color.NRGBA{R: 0, G: 0, B: 200, A: 255}, false},
		{color.NRGBA{R: 0, G: 0, B: 200, A: 0}, 0.5, color.NRGBA{R: 0, G: 0, B: 200, A: 0}, true},
		{color.NRGBA{R: 0, G: 0, B: 200, A: 128}, 0.5, color.NRGBA{R: 100, G: 50, B: 100, A: 128}, true},
		{color.NRGBA{R: 0, G: 0, B: 200, A: 128}, 0, color.NRGBA{R: 0, G: 0, B: 200, A: 128}, true},
	} {
		for _, img := range []draw.Image{image.NewNRGBA(image.Rect(0, 0, 1, 1)), wrappedImage{image.NewNRGBA(image.Rect(0, 0, 1, 1))}} {
			img.Set(0, 0, tc.px)
			if got := idraw.RecolorAt(img, 0, 0, col, tc.k); got != tc.recolors {
				t.Errorf("RecolorAt(%v, %v) = %v, want %v", tc.px, tc.k, got, tc.recolors)
			}
			if got := color.NRGBAModel.Convert(img.At(0, 0)); got != tc.want {
				t.Errorf("RecolorAt(%v, %v) on %T left %v, want %v", tc.px, tc.k, img, got, tc.want)
			}
		}
	}
}
