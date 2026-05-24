// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package raster

import (
	"image"
	"image/color"
	"image/draw"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	icol "github.com/lrstanley/x/charm/still/internal/color"
	idraw "github.com/lrstanley/x/charm/still/internal/draw"
	"golang.org/x/image/font"
)

// DrawGlyphLayout rasterizes layout onto img.
func DrawGlyphLayout(ctx GlyphContext, img draw.Image, layout Layout) {
	if mask, ok := layoutBoxGlyphMask(ctx, layout); ok {
		drawBoxGlyphMask(img, layout.Fg, mask)
		return
	}
	switch layout.Mode {
	case rasterScaled:
		idraw.FallbackGlyphScaled(img, layout.Target, layout.Face, layout.Fg, layout.Glyph, layout.Dot, layout.ScaleMode, layout.AlignEnd)
	case rasterDirect:
		drawer := font.Drawer{
			Dst:  img,
			Src:  image.NewUniform(layout.Fg),
			Face: layout.Face,
			Dot:  layout.Dot,
		}
		drawer.DrawString(layout.Glyph)
	default:
		panic("invalid glyph layout mode")
	}
}

// DrawGlyph rasterizes the cell's grapheme into area.
func DrawGlyph(ctx GlyphContext, img draw.Image, area image.Rectangle, cell *uv.Cell, fg color.Color) {
	layout, ok := LayoutGlyph(ctx, area, cell, fg)
	if !ok {
		return
	}
	DrawGlyphLayout(ctx, img, layout)
}

// AccumulateGlyphLayout adds layout ink to coverage accumulation.
func AccumulateGlyphLayout(ctx GlyphContext, cov *Coverage, layout Layout) {
	if mask, ok := layoutBoxGlyphMask(ctx, layout); ok {
		fg := icol.NRGBA(layout.Fg)
		dr := mask.dr
		for y := dr.Min.Y; y < dr.Max.Y; y++ {
			for x := dr.Min.X; x < dr.Max.X; x++ {
				ax := x - dr.Min.X
				ay := y - dr.Min.Y
				if ga := mask.alpha.AlphaAt(ax, ay).A; ga != 0 {
					cov.Accumulate(x, y, ga, fg)
				}
			}
		}
		return
	}
	r, n := utf8.DecodeRuneInString(layout.Glyph)
	if n == 0 || r == utf8.RuneError {
		return
	}
	dr, mask, maskp, _, ok := layout.Face.Glyph(layout.Dot, r)
	if !ok || dr.Empty() || mask == nil {
		return
	}

	fg := icol.NRGBA(layout.Fg)
	for y := dr.Min.Y; y < dr.Max.Y; y++ {
		for x := dr.Min.X; x < dr.Max.X; x++ {
			if ga := glyphMaskAlpha(mask, maskp, dr, x, y); ga != 0 {
				cov.Accumulate(x, y, ga, fg)
			}
		}
	}
}

func glyphMaskAlpha(mask image.Image, maskp image.Point, dr image.Rectangle, x, y int) uint8 {
	mx := maskp.X + (x - dr.Min.X)
	my := maskp.Y + (y - dr.Min.Y)
	if !image.Pt(mx, my).In(mask.Bounds()) {
		return 0
	}
	_, _, _, a := mask.At(mx, my).RGBA()
	return uint8(a >> 8) //nolint:gosec // RGBA alpha is 16-bit; high byte is 0-255
}
