// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package raster

import (
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"

	"github.com/lrstanley/x/term/still/internal/alpha"
	idraw "github.com/lrstanley/x/term/still/internal/draw"
	"github.com/lrstanley/x/term/vt/screen"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

const (
	// glyphMaskCacheEntries and glyphMaskCacheBytes bound the alpha-mask atlas.
	// Masks are color-independent, so one entry covers every foreground that
	// uses that face and grapheme. The caps follow a session-sized atlas
	// (thousands of glyphs, tens of megabytes) rather than a single screen.
	glyphMaskCacheEntries = 4096
	glyphMaskCacheBytes   = 32 << 20
)

// glyphMaskKey identifies a cached mask. Masks are positioned relative to the
// cell area, which is the same for every cell of a size.
type glyphMaskKey struct {
	face  font.Face
	glyph string
	size  image.Point
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
	if mask, ok := layoutSpriteMask(ctx, layout); ok {
		drawBoxGlyphMask(img, layout.Fg, mask)
		return
	}
	if layout.Kind == KindGrid {
		drawer := font.Drawer{
			Dst:  img,
			Src:  image.NewUniform(layout.Fg),
			Face: layout.Face,
			Dot:  layout.Dot,
		}
		drawer.DrawString(layout.Glyph)
		return
	}
	if mask, ok := buildGlyphLayoutMask(layout); ok {
		drawGlyphLayoutMask(img, layout, mask)
		alpha.ReleaseGlyphAlpha(mask.alpha)
	}
}

func drawGlyphLayoutMask(img draw.Image, layout *Layout, mask cachedGlyphMask) {
	dr := mask.alpha.Bounds().Add(layout.Area.Min.Add(mask.offset))
	draw.DrawMask(img, dr, image.NewUniform(layout.Fg), image.Point{}, mask.alpha, image.Point{}, draw.Over)
}

// DrawGlyph rasterizes the cell's grapheme into area.
func DrawGlyph(ctx GlyphContext, img draw.Image, area image.Rectangle, cell *screen.Cell, fg color.NRGBA) {
	var layout Layout
	if !LayoutGlyph(ctx, area, cell, fg, &layout) {
		return
	}
	DrawGlyphLayout(ctx, img, &layout)
}

func accumulateGlyphLayout(ctx GlyphContext, cov *Coverage, layout *Layout, cache *glyphMaskCache) {
	if mask, ok := layoutSpriteMask(ctx, layout); ok {
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
		size:  layout.Area.Size(),
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

// buildGlyphLayoutMask rasterizes a face glyph. Powerline glyphs are scaled
// into their target; Nerd Font and fallback glyphs are moved vertically to fit
// the cell, and scaled down into their target band only when their ink still
// overflows it.
func buildGlyphLayoutMask(layout *Layout) (cachedGlyphMask, bool) {
	dot := layout.Dot
	switch layout.Kind {
	case KindGrid, KindSprite:
	case KindPowerline:
		return scaledGlyphLayoutMask(layout, dot, layout.ScaleMode)
	case KindFallback:
		dot.Y += fixed.I(idraw.GlyphVerticalAdjust(layout.Face, dot, layout.Glyph, layout.Area))
		if dr, ok := idraw.GlyphRasterBounds(layout.Face, dot, layout.Glyph); ok && idraw.FallbackGlyphNeedsScale(dr, layout.Target) {
			return scaledGlyphLayoutMask(layout, dot, idraw.ScaleDefault)
		}
	}
	dr, mask, maskp, _, ok := layout.Face.Glyph(dot, layout.Rune)
	if !ok || dr.Empty() || mask == nil {
		return cachedGlyphMask{}, false
	}
	return cachedGlyphMask{
		offset: dr.Min.Sub(layout.Area.Min),
		alpha:  alpha.RasterGlyph(dr, mask, maskp),
	}, true
}

func scaledGlyphLayoutMask(layout *Layout, dot fixed.Point26_6, mode idraw.ScaleMode) (cachedGlyphMask, bool) {
	dr, mask, ok := idraw.ScaledGlyphMask(layout.Target, layout.Face, layout.Glyph, dot, mode, layout.AlignEnd)
	if !ok {
		return cachedGlyphMask{}, false
	}
	return cachedGlyphMask{offset: dr.Min.Sub(layout.Area.Min), alpha: mask}, true
}
