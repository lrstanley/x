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

	// target tells apart a glyph that may grow into the next cell.
	target image.Point
}

type cachedGlyphMask struct {
	offset image.Point
	alpha  *image.Alpha
}

type glyphMaskCache struct {
	items   map[glyphMaskKey]cachedGlyphMask
	bytes   int
	evicted []*image.Alpha // released by releaseEvicted: queued glyphs may still read them.
}

func (c *glyphMaskCache) releaseEvicted() {
	for _, a := range c.evicted {
		alpha.ReleaseGlyphAlpha(a)
	}
	c.evicted = c.evicted[:0]
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
	if !LayoutGlyph(ctx, area, cell, false, fg, &layout) {
		return
	}
	DrawGlyphLayout(ctx, img, &layout)
}

// layoutMaskPixels resolves layout to its destination rectangle and alpha mask.
func layoutMaskPixels(ctx GlyphContext, layout *Layout, cache *glyphMaskCache) (dr image.Rectangle, pix []uint8, stride int, ok bool) {
	if mask, ok := layoutSpriteMask(ctx, layout); ok {
		return mask.dr, mask.alpha.Pix, mask.alpha.Stride, true
	}
	mask, ok := glyphLayoutMask(layout, cache)
	if !ok {
		return dr, nil, 0, false
	}
	dr = mask.alpha.Bounds().Add(layout.Area.Min.Add(mask.offset))
	return dr, mask.alpha.Pix, mask.alpha.Stride, true
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
		face:   layout.Face,
		glyph:  layout.Glyph,
		size:   layout.Area.Size(),
		target: layout.Target.Size(),
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
			cache.evicted = append(cache.evicted, m.alpha)
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
		if dr, ok := idraw.GlyphRasterBounds(layout.Face, dot, layout.Glyph); ok {
			if idraw.FallbackGlyphNeedsScale(dr, layout.Target) {
				return scaledGlyphLayoutMask(layout, dot, idraw.ScaleDefault)
			}
			shift := layout.spanShift(dr)
			dot = dot.Add(fixed.P(shift.X, shift.Y))
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
	dr = dr.Add(layout.spanShift(dr))
	return cachedGlyphMask{offset: dr.Min.Sub(layout.Area.Min), alpha: mask}, true
}

// spanShift returns the move that puts ink dr where Ghostty aligns an icon
// that may grow past its cell: ink that already lies within the first cell
// stays put. Anything else is centered vertically in the target, and
// horizontally in the first cell, or starts at its left edge when wider than
// it, leaving the room after it empty (Ghostty's center1 alignment).
func (l *Layout) spanShift(dr image.Rectangle) image.Point {
	if !l.SpansNext || dr.In(image.Rect(l.Area.Min.X, dr.Min.Y, l.Area.Max.X, dr.Max.Y)) {
		return image.Point{}
	}
	return image.Pt(
		l.Area.Min.X+max(0, (l.Area.Dx()-dr.Dx())/2)-dr.Min.X,
		l.Target.Min.Y+(l.Target.Dy()-dr.Dy())/2-dr.Min.Y,
	)
}
