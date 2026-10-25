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
	base  rune
	marks string
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
	if layout.Kind == KindColor {
		drawColorLayout(img, layout)
		return
	}
	if mask, ok := layoutSpriteMask(ctx, layout); ok {
		drawBoxGlyphMask(img, layout.Fg, mask)
		return
	}
	if layout.Kind == KindGrid && layout.Cluster.Marks == "" {
		drawer := font.Drawer{
			Dst:  img,
			Src:  image.NewUniform(layout.Fg),
			Face: layout.Face,
			Dot:  layout.Dot,
		}
		// Only the base rune: selectors and joiners have no glyph to draw.
		drawer.DrawString(string(layout.Cluster.Base))
		return
	}
	if mask, ok := buildGlyphLayoutMask(layout); ok {
		drawGlyphLayoutMask(img, layout, mask)
		alpha.ReleaseGlyphAlpha(mask.alpha)
	}
}

// drawColorLayout draws the glyph of a [KindColor] layout over its area. The
// glyph keeps its own colors; the foreground does not tint it.
func drawColorLayout(img draw.Image, layout *Layout) {
	src := layout.Color.Image(layout.Area.Dx(), layout.Area.Dy())
	if src == nil {
		return
	}
	if dst, ok := img.(*image.NRGBA); ok {
		blendPremultiplied(dst, layout.Area, src, layout.Opacity)
		return
	}
	if layout.Opacity == 255 {
		draw.Draw(img, layout.Area, src, image.Point{}, draw.Over)
		return
	}
	draw.DrawMask(img, layout.Area, src, image.Point{}, image.NewUniform(color.Alpha{A: layout.Opacity}), image.Point{}, draw.Over)
}

// blendPremultiplied draws src, premultiplied and sized like area, over dst
// within area, scaled by opacity. It is what [draw.Over] does, without the
// per pixel color conversions that make it slow for a screen of emoji.
func blendPremultiplied(dst *image.NRGBA, area image.Rectangle, src *image.RGBA, opacity uint8) {
	// The opacity is multiplied in without rounding, so the source's color and
	// alpha, and its alpha scaled by opacity, are all kept in units of 1/255 of
	// 1/255: full alpha is one.
	const one = 255 * 255
	r := area.Intersect(dst.Bounds())
	if r.Empty() {
		return
	}
	op := int(opacity)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		sp := src.Pix[(y-area.Min.Y)*src.Stride+(r.Min.X-area.Min.X)*4:]
		dp := dst.Pix[dst.PixOffset(r.Min.X, y):]
		for x := range r.Dx() {
			s, d := sp[x*4:x*4+4:x*4+4], dp[x*4:x*4+4:x*4+4]
			cr, cg, cb, sa := int(s[0])*op, int(s[1])*op, int(s[2])*op, int(s[3])*op
			switch da := int(d[3]); {
			case sa == 0:
			case sa == one:
				d[0], d[1], d[2], d[3] = s[0], s[1], s[2], 255
			case da == 255:
				inv := one - sa
				d[0] = uint8((255*cr + int(d[0])*inv + one/2) / one) //nolint:gosec // At most 255.
				d[1] = uint8((255*cg + int(d[1])*inv + one/2) / one) //nolint:gosec // At most 255.
				d[2] = uint8((255*cb + int(d[2])*inv + one/2) / one) //nolint:gosec // At most 255.
			default:
				// The destination is not premultiplied, so weigh it by its
				// alpha before it shows through, then undo that for the result.
				inv := one - sa
				den := 255*sa + da*inv
				d[0] = uint8(min((one*cr+int(d[0])*da*inv+den/2)/den, 255)) //nolint:gosec // Clamped.
				d[1] = uint8(min((one*cg+int(d[1])*da*inv+den/2)/den, 255)) //nolint:gosec // Clamped.
				d[2] = uint8(min((one*cb+int(d[2])*da*inv+den/2)/den, 255)) //nolint:gosec // Clamped.
				d[3] = uint8((den + one/2) / one)                           //nolint:gosec // At most 255.
			}
		}
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
		base:   layout.Cluster.Base,
		marks:  layout.Cluster.Marks,
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
	case KindGrid, KindSprite, KindColor:
	case KindPowerline:
		return scaledGlyphLayoutMask(layout, dot, layout.ScaleMode)
	case KindFallback:
		dot.Y += fixed.I(idraw.GlyphVerticalAdjust(layout.Face, dot, layout.Cluster.Text, layout.Area))
		if dr, ok := idraw.GlyphRasterBounds(layout.Face, dot, layout.Cluster.Text); ok {
			if idraw.FallbackGlyphNeedsScale(dr, layout.Target) {
				return scaledGlyphLayoutMask(layout, dot, idraw.ScaleDefault)
			}
			shift := layout.spanShift(dr)
			dot = dot.Add(fixed.P(shift.X, shift.Y))
		}
	}
	if layout.Cluster.Marks != "" {
		return markedGlyphMask(layout, dot)
	}
	dr, mask, maskp, _, ok := layout.Face.Glyph(dot, layout.Cluster.Base)
	if !ok || dr.Empty() || mask == nil {
		return cachedGlyphMask{}, false
	}
	return cachedGlyphMask{
		offset: dr.Min.Sub(layout.Area.Min),
		alpha:  alpha.RasterGlyph(dr, mask, maskp),
	}, true
}

// markedGlyphMask rasterizes the base rune of layout together with its
// combining marks. Marks the face has no glyph for are skipped. A base without
// ink (a space) still draws its marks.
func markedGlyphMask(layout *Layout, dot fixed.Point26_6) (cachedGlyphMask, bool) {
	// A face reuses its glyph mask between calls, so each part is copied before
	// the next glyph is requested.
	type part struct {
		dr    image.Rectangle
		alpha *image.Alpha
	}
	parts := make([]part, 0, 1+len(layout.Cluster.Marks))
	var bounds image.Rectangle
	add := func(dr image.Rectangle, mask image.Image, maskp image.Point) {
		if dr.Empty() || mask == nil {
			return
		}
		parts = append(parts, part{dr, alpha.RasterGlyph(dr, mask, maskp)})
		bounds = bounds.Union(dr)
	}

	dr, mask, maskp, baseAdvance, ok := layout.Face.Glyph(dot, layout.Cluster.Base)
	if ok {
		add(dr, mask, maskp)
	}
	for _, m := range layout.Cluster.Marks {
		advance, has := layout.Face.GlyphAdvance(m)
		if !has {
			continue
		}
		// A zero-width mark is positioned relative to the pen after its base,
		// so its ink lies left of its own origin. A mark with an advance of
		// its own is drawn over the base cell.
		markDot := dot
		if advance == 0 {
			markDot.X += baseAdvance
		}
		if dr, mask, maskp, _, ok = layout.Face.Glyph(markDot, m); ok {
			add(dr, mask, maskp)
		}
	}
	if len(parts) == 0 {
		return cachedGlyphMask{}, false
	}
	out := image.NewAlpha(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	for _, p := range parts {
		draw.Draw(out, p.dr.Sub(bounds.Min), p.alpha, image.Point{}, draw.Over)
		alpha.ReleaseGlyphAlpha(p.alpha)
	}
	return cachedGlyphMask{offset: bounds.Min.Sub(layout.Area.Min), alpha: out}, true
}

func scaledGlyphLayoutMask(layout *Layout, dot fixed.Point26_6, mode idraw.ScaleMode) (cachedGlyphMask, bool) {
	dr, mask, ok := idraw.ScaledGlyphMask(layout.Target, layout.Face, layout.Cluster.Text, dot, mode, layout.AlignEnd)
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
