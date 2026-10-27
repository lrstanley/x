// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package palette

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/lrstanley/x/term/reel/sink/internal/testutil"
)

type blendPair struct{ fg, bg color.NRGBA }

// rampFrame draws one band per pair: a solid background, a solid foreground
// block, and a one-pixel-tall ramp of every fg-over-bg blend, like the
// antialiased edges of glyphs.
func rampFrame(pairs []blendPair) *image.NRGBA {
	const bandH = 24
	img := image.NewNRGBA(image.Rect(0, 0, 256, bandH*len(pairs)))
	for i, p := range pairs {
		y0 := i * bandH
		testutil.PaintRect(img, image.Rect(0, y0, 256, y0+bandH), p.bg)
		testutil.PaintRect(img, image.Rect(0, y0, 128, y0+8), p.fg)
		for a := range 256 {
			img.SetNRGBA(a, y0+12, blendNRGBA(p.fg, p.bg, uint8(a))) //nolint:gosec // a is bounded.
		}
	}
	return img
}

func blendNRGBA(fg, bg color.NRGBA, a uint8) color.NRGBA {
	mix := func(f, b uint8) uint8 {
		return uint8((int(f)*int(a) + int(b)*(255-int(a)) + 127) / 255) //nolint:gosec // bounded.
	}
	return color.NRGBA{R: mix(fg.R, bg.R), G: mix(fg.G, bg.G), B: mix(fg.B, bg.B), A: 0xff}
}

// offBlendLine is the RGB distance from c to the segment between fg and bg,
// i.e. how far a blend has drifted toward another hue.
func offBlendLine(c, fg, bg color.NRGBA) float64 {
	d := [3]float64{float64(fg.R) - float64(bg.R), float64(fg.G) - float64(bg.G), float64(fg.B) - float64(bg.B)}
	p := [3]float64{float64(c.R) - float64(bg.R), float64(c.G) - float64(bg.G), float64(c.B) - float64(bg.B)}
	l2 := d[0]*d[0] + d[1]*d[1] + d[2]*d[2]
	t := 0.0
	if l2 > 0 {
		t = math.Max(0, math.Min(1, (p[0]*d[0]+p[1]*d[1]+p[2]*d[2])/l2))
	}
	sum := 0.0
	for i := range 3 {
		e := p[i] - t*d[i]
		sum += e * e
	}
	return math.Sqrt(sum)
}

func palettizeAdaptive(ap *Adaptive, src image.Image) *image.Paletted {
	pm := image.NewPaletted(src.Bounds(), ap.pal)
	ap.Palettize(pm, src)
	return pm
}

func nrgbaAt(pm *image.Paletted, x, y int) color.NRGBA {
	r, g, b, a := pm.At(x, y).RGBA()
	return color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)} //nolint:gosec // 16-bit channels.
}

var testBlendPairs = []blendPair{
	{color.NRGBA{0xcd, 0xd6, 0xf4, 0xff}, color.NRGBA{0x1e, 0x1e, 0x2e, 0xff}},
	{color.NRGBA{0xfa, 0xb3, 0x87, 0xff}, color.NRGBA{0x1e, 0x1e, 0x2e, 0xff}},
	{color.NRGBA{0xa6, 0xe3, 0xa1, 0xff}, color.NRGBA{0x31, 0x32, 0x44, 0xff}},
	{color.NRGBA{0xd2, 0x0f, 0x39, 0xff}, color.NRGBA{0xef, 0xf1, 0xf5, 0xff}},
	{color.NRGBA{0x89, 0xb4, 0xfa, 0xff}, color.NRGBA{0x18, 0x18, 0x25, 0xff}},
	{color.NRGBA{0xf5, 0xc2, 0xe7, 0xff}, color.NRGBA{0x11, 0x11, 0x1b, 0xff}},
}

func TestAdaptivePaletteMatchesWhenColorsFit(t *testing.T) {
	t.Parallel()

	src := rampFrame(testBlendPairs[:1])
	pm := palettizeAdaptive(NewAdaptive(true, false), src)
	for y := src.Rect.Min.Y; y < src.Rect.Max.Y; y++ {
		for x := src.Rect.Min.X; x < src.Rect.Max.X; x++ {
			got, want := nrgbaAt(pm, x, y), src.NRGBAAt(x, y)
			dr, dg, db := int(got.R)-int(want.R), int(got.G)-int(want.G), int(got.B)-int(want.B)
			if d := dr*dr + dg*dg + db*db; d > adaptiveMatchDist {
				t.Fatalf("pixel (%d,%d) = %v, want within match distance of %v", x, y, got, want)
			}
		}
	}
}

func TestAdaptivePaletteKeepsBlendsOnHue(t *testing.T) {
	t.Parallel()

	src := rampFrame(testBlendPairs)
	ap := NewAdaptive(true, false)
	pm := palettizeAdaptive(ap, src)
	if ap.n > adaptiveSlots {
		t.Fatalf("used %d entries, want at most %d", ap.n, adaptiveSlots)
	}

	const bandH = 24
	for i, p := range testBlendPairs {
		y0 := i * bandH
		if got := nrgbaAt(pm, 200, y0); got != p.bg {
			t.Errorf("pair %d background = %v, want exact %v", i, got, p.bg)
		}
		if got := nrgbaAt(pm, 0, y0); got != p.fg {
			t.Errorf("pair %d foreground = %v, want exact %v", i, got, p.fg)
		}
		var sum, worst float64
		for x := range 256 {
			off := offBlendLine(nrgbaAt(pm, x, y0+12), p.fg, p.bg)
			sum += off
			worst = max(worst, off)
		}
		if mean := sum / 256; mean > 2 || worst > 8 {
			t.Errorf("pair %d blend drift mean %.1f worst %.1f, want mean <= 2 and worst <= 8", i, mean, worst)
		}
	}
}

func TestAdaptivePaletteStableIndices(t *testing.T) {
	t.Parallel()

	ap := NewAdaptive(true, false)
	first := palettizeAdaptive(ap, rampFrame(testBlendPairs[:2]))
	palettizeAdaptive(ap, rampFrame(testBlendPairs[2:]))
	again := palettizeAdaptive(ap, rampFrame(testBlendPairs[:2]))
	if !bytes.Equal(first.Pix, again.Pix) {
		t.Fatal("repainting a frame after the palette grew changed its indices")
	}
}

func TestAdaptivePaletteLateSolidColorExact(t *testing.T) {
	t.Parallel()

	ap := NewAdaptive(true, false)
	palettizeAdaptive(ap, rampFrame(testBlendPairs))
	late := color.NRGBA{R: 0x40, G: 0xa0, B: 0x20, A: 0xff}
	src := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	testutil.PaintRect(src, src.Bounds(), late)
	pm := palettizeAdaptive(ap, src)
	if got := nrgbaAt(pm, 8, 8); got != late {
		t.Fatalf("late solid color = %v, want exact %v", got, late)
	}
}

func TestAdaptivePaletteTransparency(t *testing.T) {
	t.Parallel()

	src := image.NewNRGBA(image.Rect(0, 0, 4, 1))
	src.SetNRGBA(1, 0, color.NRGBA{R: 0xff, A: 0xff})
	src.SetNRGBA(2, 0, color.NRGBA{G: 0xff, A: 0x80})
	ap := NewAdaptive(false, false)
	pm := palettizeAdaptive(ap, src)
	for _, x := range []int{0, 3} {
		if idx := pm.ColorIndexAt(x, 0); idx != TransparentIndex {
			t.Errorf("transparent pixel %d index = %d, want %d", x, idx, TransparentIndex)
		}
	}
	for _, x := range []int{1, 2} {
		if idx := pm.ColorIndexAt(x, 0); idx == TransparentIndex {
			t.Errorf("opaque pixel %d mapped to the transparent index", x)
		}
	}
	if got, want := nrgbaAt(pm, 2, 0), (color.NRGBA{G: 0xff, A: 0xff}); got != want {
		t.Errorf("translucent pixel = %v, want its opaque color %v", got, want)
	}
}

func TestAdaptivePaletteRGBAMatchesNRGBA(t *testing.T) {
	t.Parallel()

	src := rampFrame(testBlendPairs)
	a := palettizeAdaptive(NewAdaptive(true, false), src)
	b := palettizeAdaptive(NewAdaptive(true, false), testutil.ToRGBA(src))
	if !bytes.Equal(a.Pix, b.Pix) {
		t.Fatal("RGBA and NRGBA sources palettized differently")
	}
}

// TestAdaptivePaletteSmallFlatShapeExact draws a 5x5 square, too few pixels
// to count as solid by area, in a frame whose large fills leave only a few
// slots for everything else, which blends near the square's hue compete
// for. The square is flat, so it still gets an exact entry, like the window
// buttons of a busy frame.
func TestAdaptivePaletteSmallFlatShapeExact(t *testing.T) {
	t.Parallel()

	fills := testutil.DistinctColors(232)
	src := image.NewNRGBA(image.Rect(0, 0, 8*len(fills), 32))
	for i, c := range fills {
		testutil.PaintRect(src, image.Rect(i*8, 0, (i+1)*8, 8), c)
	}
	// Blends on a checkerboard, so none forms a flat block. Each covers more
	// pixels than the square, but fewer than a fill.
	for i := range 64 * 32 {
		x, y := i%512*2+i/512%2, 12+i/512
		n := i % 64
		src.SetNRGBA(x, y, color.NRGBA{R: uint8(0xc0 + n), G: uint8(0x50 + n%4*8), B: 0x60, A: 0xff}) //nolint:gosec // bounded.
	}
	square := color.NRGBA{R: 0xff, G: 0x5f, B: 0x57, A: 0xff}
	testutil.PaintRect(src, image.Rect(1300, 20, 1305, 25), square)

	pm := palettizeAdaptive(NewAdaptive(true, false), src)
	if got := nrgbaAt(pm, 1302, 22); got != square {
		t.Fatalf("small flat square = %v, want exact %v", got, square)
	}
}
