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
	"github.com/lrstanley/x/charm/still/internal/alpha"
	idraw "github.com/lrstanley/x/charm/still/internal/draw"
	"golang.org/x/image/font"
)

const glyphMaskCacheLimit = 128

type glyphMaskKey struct {
	face  font.Face
	glyph string
}

type cachedGlyphMask struct {
	offset image.Point
	alpha  *image.Alpha
}

type glyphMaskCache map[glyphMaskKey]cachedGlyphMask

// DrawGlyphLayout rasterizes layout onto img.
func DrawGlyphLayout(ctx GlyphContext, img draw.Image, layout *Layout) {
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
func DrawGlyph(ctx GlyphContext, img draw.Image, area image.Rectangle, cell *uv.Cell, fg color.NRGBA) {
	var layout Layout
	if !LayoutGlyph(ctx, area, cell, fg, &layout) {
		return
	}
	DrawGlyphLayout(ctx, img, &layout)
}

// AccumulateGlyphLayout adds layout ink to coverage accumulation.
func AccumulateGlyphLayout(ctx GlyphContext, cov *Coverage, layout *Layout) {
	accumulateGlyphLayout(ctx, cov, layout, nil)
}

func accumulateGlyphLayout(ctx GlyphContext, cov *Coverage, layout *Layout, cache *glyphMaskCache) {
	if mask, ok := layoutBoxGlyphMask(ctx, layout); ok {
		accumulateGlyphMask(cov, mask.dr, mask.alpha.Pix, mask.alpha.Stride, layout.Fg)
		return
	}
	mask, ok := glyphLayoutMask(layout, cache)
	if !ok {
		return
	}
	dr := mask.alpha.Bounds().Add(layout.Area.Min.Add(mask.offset))
	accumulateGlyphMask(cov, dr, mask.alpha.Pix, mask.alpha.Stride, layout.Fg)
}

func accumulateGlyphMask(cov *Coverage, dr image.Rectangle, pix []uint8, stride int, fg color.NRGBA) {
	dr = dr.Intersect(cov.bounds)
	if dr.Empty() {
		return
	}
	b := cov.bounds
	dx := dr.Dx()
	covDx := b.Dx()
	minX, minY := b.Min.X, b.Min.Y
	pixels := cov.pixels
	for y := dr.Min.Y; y < dr.Max.Y; y++ {
		ay := y - dr.Min.Y
		row := pix[ay*stride : ay*stride+dx]
		rowBase := (y-minY)*covDx + (dr.Min.X - minX)
		for x := dr.Min.X; x < dr.Max.X; x++ {
			ga := row[x-dr.Min.X]
			if ga == 0 {
				continue
			}
			i := rowBase + (x - dr.Min.X)
			if ga > uint8(pixels[i]) {
				pixels[i] = packCoveragePixel(ga, fg)
				cov.markDirty(x, y)
			}
		}
	}
}

func glyphLayoutMask(layout *Layout, cache *glyphMaskCache) (cachedGlyphMask, bool) {
	if cache == nil {
		return buildGlyphLayoutMask(layout)
	}
	key := glyphMaskKey{
		face:  layout.Face,
		glyph: layout.Glyph,
	}
	if *cache != nil {
		if mask, ok := (*cache)[key]; ok {
			return mask, true
		}
	} else {
		*cache = make(glyphMaskCache)
	}
	mask, ok := buildGlyphLayoutMask(layout)
	if !ok {
		return cachedGlyphMask{}, false
	}
	if len(*cache) >= glyphMaskCacheLimit {
		for _, m := range *cache {
			alpha.ReleaseGlyphAlpha(m.alpha)
		}
		clear(*cache)
	}
	(*cache)[key] = mask
	return mask, true
}

func buildGlyphLayoutMask(layout *Layout) (cachedGlyphMask, bool) {
	r, n := utf8.DecodeRuneInString(layout.Glyph)
	if n == 0 || r == utf8.RuneError {
		return cachedGlyphMask{}, false
	}
	dr, mask, maskp, _, ok := layout.Face.Glyph(layout.Dot, r)
	if !ok || dr.Empty() || mask == nil {
		return cachedGlyphMask{}, false
	}
	return cachedGlyphMask{
		offset: dr.Min.Sub(layout.Area.Min),
		alpha:  alpha.RasterGlyph(dr, mask, maskp),
	}, true
}
