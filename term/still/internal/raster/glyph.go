// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package raster

import (
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"unicode/utf8"

	"github.com/lrstanley/x/term/still/internal/alpha"
	idraw "github.com/lrstanley/x/term/still/internal/draw"
	"github.com/lrstanley/x/term/vt/screen"
	"golang.org/x/image/font"
)

const (
	// glyphMaskCacheEntries and glyphMaskCacheBytes bound the alpha-mask atlas.
	// Masks are color-independent, so one entry covers every foreground that
	// uses that face and grapheme. The caps follow a session-sized atlas
	// (thousands of glyphs, tens of megabytes) rather than a single screen.
	glyphMaskCacheEntries = 4096
	glyphMaskCacheBytes   = 32 << 20
)

type glyphMaskKey struct {
	face  font.Face
	glyph string
}

type cachedGlyphMask struct {
	offset image.Point
	alpha  *image.Alpha
}

type glyphMaskCache struct {
	items map[glyphMaskKey]cachedGlyphMask
	bytes int
}

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
func DrawGlyph(ctx GlyphContext, img draw.Image, area image.Rectangle, cell *screen.Cell, fg color.NRGBA) {
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
	xOrigin := dr.Min.X
	for y := dr.Min.Y; y < dr.Max.Y; y++ {
		off := (y - dr.Min.Y) * stride
		row := pix[off : off+dx : off+dx]
		rowBase := (y-minY)*covDx + (xOrigin - minX)
		rowDirty := false
		rowMin, rowMax := 0, 0
		for xi := 0; xi < dx; {
			// Most mask bytes are 0. A zero uint64 is eight zero alphas.
			if xi&7 == 0 && xi+8 <= dx && binary.LittleEndian.Uint64(row[xi:]) == 0 {
				xi += 8
				continue
			}
			ga := row[xi]
			if ga != 0 {
				i := rowBase + xi
				if ga > uint8(pixels[i]) {
					pixels[i] = packCoveragePixel(ga, fg)
					x := xOrigin + xi
					if !rowDirty {
						rowMin = x
						rowDirty = true
					}
					rowMax = x
				}
			}
			xi++
		}
		if rowDirty {
			cov.markRow(y, rowMin, rowMax)
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
	if cache.items != nil {
		if mask, ok := cache.items[key]; ok {
			return mask, true
		}
	} else {
		cache.items = make(map[glyphMaskKey]cachedGlyphMask)
	}
	mask, ok := buildGlyphLayoutMask(layout)
	if !ok {
		return cachedGlyphMask{}, false
	}
	n := 0
	if mask.alpha != nil {
		n = len(mask.alpha.Pix)
	}
	if n > glyphMaskCacheBytes {
		return mask, true
	}
	if len(cache.items) >= glyphMaskCacheEntries || cache.bytes+n > glyphMaskCacheBytes {
		for _, m := range cache.items {
			alpha.ReleaseGlyphAlpha(m.alpha)
		}
		clear(cache.items)
		cache.bytes = 0
	}
	cache.items[key] = mask
	cache.bytes += n
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
