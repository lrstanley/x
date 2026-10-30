// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package raster

import (
	"image"
	"slices"

	"github.com/lrstanley/x/term/reel/vt"
	"golang.org/x/image/vector"
)

// Registered glyphs are placed the way Rio, the Glyph Protocol's reference
// terminal, places them, not the way the protocol says to. Rio is what
// programs are written and tested against, and its own example programs
// send only upm, so under the spec's defaults one em is the whole cell
// height: font icons draw about a third bigger than the text beside them,
// and any that reach outside the em are clipped. Rio draws them smaller than
// the text instead, with an em the cell's width, and whole.
//
// Rio (raphamorim/rio at bf0f97aa) reads only the upm and width options and
// ignores aw, lh, size, align, and pad (see its parser,
// https://github.com/raphamorim/rio/blob/bf0f97aa4e628879458739708c90bf38234ff2f7/rio-vt/src/ansi/glyph_protocol.rs#L338-L356):
//
//   - The scale is uniform: one em fits the span's shorter side, then
//     shrinks until the outline's bounding box fits the span. See
//     glyph_cell_scale in
//     https://github.com/raphamorim/rio/blob/bf0f97aa4e628879458739708c90bf38234ff2f7/sugarloaf/src/renderer/image_cache/colr_raster.rs#L124-L126
//   - The bounding box is centered in the span, which is width cells wide
//     and one cell tall, on both axes. The font's baseline plays no part.
//     See
//     https://github.com/raphamorim/rio/blob/bf0f97aa4e628879458739708c90bf38234ff2f7/rio-grid/src/lib.rs#L2028-L2029
//
// Rio takes the bounding box from the glyf header. This takes it from the
// outline's points, so a header that disagrees cannot misplace the glyph.
//
// # Following the spec instead
//
// The spec's placement is §8.5 of
// https://github.com/raphamorim/rio/blob/bf0f97aa4e628879458739708c90bf38234ff2f7/specs/glyph-protocol.md#L429-L532
// and [vt.Glyph] already holds every option it needs. It was implemented in
// commit b6916f0 ("fix(reel): put a registered glyph's baseline on the text
// baseline"), and goes, in pixels down from the span's top-left corner with
// w and h the span's size:
//
//  1. Pad: with top, right, bottom, and left from Pad, reset all four to 0
//     when left+right >= 1 or top+bottom >= 1. The padded span is
//     x0 = w*left, x1 = w*(1-right), y0 = h*top, y1 = h*(1-bottom).
//  2. Size: with fx = (x1-x0)/Advance and fy = (y1-y0)/LineHeight, the
//     scale (sx, sy) is (fy, fy) for height, (fx, fx) for advance,
//     min(fx, fy) for contain, max(fx, fy) for cover, and (fx, fy) for
//     stretch.
//  3. Align: the authored extent is [0, Advance] by [-desc, LineHeight-desc],
//     Y up with y=0 on the baseline. The spec does not say what desc is, so
//     it is the font's: desc = LineHeight * (h-baseline)/h, where baseline is
//     the font baseline's distance down from the cell top (Dot.Y less
//     Area.Min.Y). That puts y=0 on the text baseline. The design origin
//     lands at ox, oy:
//     ox is (x0+x1-Advance*sx)/2 for center, x0 for start, x1-Advance*sx
//     for end. oy is (y0+y1)/2 + (LineHeight/2-desc)*sy for center,
//     y1-desc*sy for start, y0+(LineHeight-desc)*sy for end, and baseline
//     for baseline.
//  4. A point (x, y) draws at (ox + x*sx, oy - y*sy), clipped to the span.
//
// Under the spec, programs that want text-sized icons must send lh as the
// source font's line height (ascender minus descender, such as 1320 for
// JetBrains Mono's 1000 upm); Rio's example programs do not.

// registeredGlyphMask rasterizes a [KindRegistered] layout into its span the
// way Rio does (see above): scaled uniformly so one em fits the span's
// shorter side and the outline fits the span, and centered in it.
func registeredGlyphMask(l *Layout) (cachedGlyphMask, bool) {
	g := l.Glyph
	w, h := l.Target.Dx(), l.Target.Dy() // The span starts at the cell's corner.
	if w <= 0 || h <= 0 || g.UnitsPerEm <= 0 {
		return cachedGlyphMask{}, false
	}
	// The outline's bounding box, from its points.
	var x0, y0, x1, y1 float64
	first := true
	for _, c := range g.Contours {
		for _, p := range c {
			x, y := float64(p.X), float64(p.Y)
			if first {
				x0, y0, x1, y1, first = x, y, x, y, false
			}
			x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
		}
	}
	if first {
		return cachedGlyphMask{}, false
	}
	// A box with no width or height divides to +Inf, which min ignores.
	bw, bh := x1-x0, y1-y0
	s := min(float64(min(w, h))/float64(g.UnitsPerEm), float64(w)/bw, float64(h)/bh)
	// ox and oy are where the design origin lands, in pixels down from the
	// span's top-left corner, so the box's center is the span's.
	ox := (float64(w)-bw*s)/2 - x0*s
	oy := (float64(h)-bh*s)/2 + y1*s

	ras := vector.NewRasterizer(w, h)
	at := func(p vt.GlyphPoint) (float32, float32) {
		return float32(ox + float64(p.X)*s), float32(oy - float64(p.Y)*s)
	}
	for _, c := range g.Contours {
		traceContour(ras, c, at)
	}
	mask := image.NewAlpha(image.Rect(0, 0, w, h))
	ras.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})
	return cachedGlyphMask{alpha: mask}, true
}

// traceContour adds the closed TrueType contour pts to ras, mapped by at. Two
// off-curve points in a row have an on-curve point halfway between them.
func traceContour(ras *vector.Rasterizer, pts []vt.GlyphPoint, at func(vt.GlyphPoint) (float32, float32)) {
	n := len(pts)
	if n == 0 {
		return
	}
	// Start on an on-curve point, or, when there is none, halfway between
	// the last point and the first.
	var sx, sy float32
	next := slices.IndexFunc(pts, func(p vt.GlyphPoint) bool { return p.OnCurve })
	if next >= 0 {
		sx, sy = at(pts[next])
		next++
	} else {
		ax, ay := at(pts[n-1])
		bx, by := at(pts[0])
		sx, sy, next = (ax+bx)/2, (ay+by)/2, 0
	}
	ras.MoveTo(sx, sy)
	var cx, cy float32
	ctrl := false
	for i := range n {
		p := pts[(next+i)%n]
		px, py := at(p)
		switch {
		case p.OnCurve && ctrl:
			ras.QuadTo(cx, cy, px, py)
		case p.OnCurve:
			ras.LineTo(px, py)
		case ctrl:
			ras.QuadTo(cx, cy, (cx+px)/2, (cy+py)/2)
		}
		cx, cy, ctrl = px, py, !p.OnCurve
	}
	if ctrl {
		ras.QuadTo(cx, cy, sx, sy)
	}
	ras.ClosePath()
}
