// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package raster

import (
	"image"
	"image/color"
	"image/draw"
	"testing"

	"github.com/lrstanley/x/term/reel/render/internal/testutil"
)

func colorLayout(area image.Rectangle, opacity uint8) *Layout {
	return &Layout{Kind: KindColor, Area: area, Color: testutil.SolidColorGlyph{R: 255, A: 255}, Opacity: opacity}
}

func newColorDst(bg color.NRGBA) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, 40, 20))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)
	return dst
}

func TestDrawColorLayout(t *testing.T) {
	t.Parallel()

	bg := color.NRGBA{R: 10, G: 20, B: 30, A: 255}
	area := image.Rect(10, 4, 30, 16)
	red := color.NRGBA{R: 255, A: 255}

	dst := newColorDst(bg)
	DrawGlyphLayout(nil, dst, colorLayout(area, 255))
	for y := range 20 {
		for x := range 40 {
			want := bg
			if image.Pt(x, y).In(area) {
				want = red
			}
			if got := dst.NRGBAAt(x, y); got != want {
				t.Fatalf("pixel (%d,%d) = %v, want %v", x, y, got, want)
			}
		}
	}

	// Faint glyphs fade into what is behind them.
	dst = newColorDst(bg)
	DrawGlyphLayout(nil, dst, colorLayout(area, 128))
	got := dst.NRGBAAt(20, 10)
	if got.R < 125 || got.R > 150 || got.G < 8 || got.G > 14 || got.A != 255 {
		t.Fatalf("faint pixel = %v, want about half way between %v and %v", got, bg, red)
	}

	// A glyph with no image draws nothing.
	dst = newColorDst(bg)
	DrawGlyphLayout(nil, dst, &Layout{Kind: KindColor, Area: area, Color: noGlyph{}, Opacity: 255})
	if px := dst.NRGBAAt(20, 10); px != bg {
		t.Fatalf("pixel = %v, want the background %v", px, bg)
	}
}

type noGlyph struct{}

func (noGlyph) Image(int, int) *image.RGBA { return nil }

func TestPassDefersColorGlyphs(t *testing.T) {
	t.Parallel()

	bg := color.NRGBA{R: 10, G: 20, B: 30, A: 255}
	dst := newColorDst(bg)
	var p Pass
	p.Reset(dst.Bounds())
	p.AddGlyph(nil, colorLayout(image.Rect(10, 4, 30, 16), 255))

	// Queued glyphs are composited first; a color glyph waits for DrawPending.
	p.Composite(dst)
	if got := dst.NRGBAAt(20, 10); got != bg {
		t.Fatalf("pixel after Composite = %v, want the background %v", got, bg)
	}
	p.DrawPending(nil, dst)
	if got, want := dst.NRGBAAt(20, 10), (color.NRGBA{R: 255, A: 255}); got != want {
		t.Fatalf("pixel after DrawPending = %v, want %v", got, want)
	}

	// Reset drops it.
	p.Reset(dst.Bounds())
	dst = newColorDst(bg)
	p.DrawPending(nil, dst)
	if got := dst.NRGBAAt(20, 10); got != bg {
		t.Fatalf("pixel after Reset = %v, want the background %v", got, bg)
	}
}

// TestBlendPremultipliedMatchesDraw checks the direct blit against image/draw
// for translucent and transparent pixels over backgrounds of every alpha. The
// opacity is applied to the 8 bit source, so a faint glyph over a mostly
// transparent background may differ by a few levels.
func TestBlendPremultipliedMatchesDraw(t *testing.T) {
	t.Parallel()

	area := image.Rect(2, 1, 18, 9)
	src := image.NewRGBA(image.Rect(0, 0, area.Dx(), area.Dy()))
	for y := range area.Dy() {
		for x := range area.Dx() {
			a := uint8((x*17 + y*5) % 256)                                                             //nolint:gosec // Bounded.
			src.Set(x, y, color.NRGBA{R: uint8(x * 15), G: uint8(y * 30), B: uint8(255 - x*10), A: a}) //nolint:gosec // Bounded.
		}
	}
	for _, bgAlpha := range []uint8{255, 200, 90, 0} {
		for _, opacity := range []uint8{255, 128, 30, 0} {
			bg := color.NRGBA{R: 40, G: 90, B: 160, A: bgAlpha}
			got, want := newColorDst(bg), newColorDst(bg)
			blendPremultiplied(got, area, src, opacity)
			draw.DrawMask(want, area, src, image.Point{}, image.NewUniform(color.Alpha{A: opacity}), image.Point{}, draw.Over)
			for y := range got.Bounds().Dy() {
				for x := range got.Bounds().Dx() {
					g, w := got.NRGBAAt(x, y), want.NRGBAAt(x, y)
					if w.A == 0 {
						// The color of a transparent pixel means nothing.
						if g.A > 1 {
							t.Fatalf("bg alpha %d opacity %d: pixel (%d,%d) alpha = %d, want about 0", bgAlpha, opacity, x, y, g.A)
						}
						continue
					}
					if absDiff(g.R, w.R) > 2 || absDiff(g.G, w.G) > 2 || absDiff(g.B, w.B) > 2 || absDiff(g.A, w.A) > 2 {
						t.Fatalf("bg alpha %d opacity %d: pixel (%d,%d) = %v, image/draw gives %v", bgAlpha, opacity, x, y, g, w)
					}
				}
			}
		}
	}

	// A destination smaller than the area is clipped, not overrun.
	blendPremultiplied(image.NewNRGBA(image.Rect(0, 0, 6, 4)), area, src, 255)
}

func absDiff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}
