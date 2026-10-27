// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package raster

import (
	"cmp"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"slices"

	idraw "github.com/lrstanley/x/term/reel/render/internal/draw"
	"github.com/lrstanley/x/term/reel/vt"
)

// Coverage accumulates per-pixel glyph mask coverage before compositing.
type Coverage struct {
	bounds image.Rectangle
	pixels []uint32 // alpha | R<<8 | G<<16 | B<<24

	// rowLo and rowHi are inclusive ink spans for each row of bounds.
	// rowLo[y] < 0 means the row has no ink. Composite visits only those
	// spans, so line-gap rows between glyphs are not scanned. reset clears
	// just those spans when bounds are unchanged.
	rowLo  []int
	rowHi  []int
	hasInk bool
}

func packCoveragePixel(alpha uint8, fg color.NRGBA) uint32 {
	return uint32(alpha) | uint32(fg.R)<<8 | uint32(fg.G)<<16 | uint32(fg.B)<<24
}

// reset prepares c for bounds. It never allocates: ensure does that on the
// first overflowing glyph.
func (c *Coverage) reset(bounds image.Rectangle) {
	n := bounds.Dx() * bounds.Dy()
	h := bounds.Dy()
	// Same rectangle and same buffer: only the previous ink is dirty.
	if bounds == c.bounds && len(c.pixels) == n && len(c.rowLo) == h {
		if c.hasInk {
			dx := bounds.Dx()
			minX := bounds.Min.X
			pix := c.pixels
			for yi, x0 := range c.rowLo {
				if x0 < 0 {
					continue
				}
				i := yi*dx + (x0 - minX)
				clear(pix[i : i+(c.rowHi[yi]-x0+1)])
				c.rowLo[yi] = -1
			}
			c.hasInk = false
		}
		return
	}
	c.pixels, c.rowLo = c.pixels[:0], c.rowLo[:0]
	c.bounds = bounds
	c.hasInk = false
}

// ensure allocates (or clears) the buffers for the current bounds.
func (c *Coverage) ensure() {
	n := c.bounds.Dx() * c.bounds.Dy()
	h := c.bounds.Dy()
	if len(c.pixels) == n && len(c.rowLo) == h {
		return
	}
	if cap(c.pixels) < n {
		c.pixels = make([]uint32, n)
	} else {
		c.pixels = c.pixels[:n]
		clear(c.pixels)
	}
	if cap(c.rowLo) < h {
		c.rowLo = make([]int, h)
		c.rowHi = make([]int, h)
	} else {
		c.rowLo = c.rowLo[:h]
		c.rowHi = c.rowHi[:h]
	}
	for i := range c.rowLo {
		c.rowLo[i] = -1
	}
}

// div255 returns floor(v/255). It is exact for every v in [0, 65025], the
// range of fg*a+dst*(255-a) when fg, dst, and a are 8-bit. Let q = v>>8 and
// r = v&255. Then v/255 = q+floor((q+r)/255), and for this range q+r <= 508
// so the floor is 0 or 1. (v+1+(v>>8))>>8 = q+floor((q+r+1)/256), which is
// the same correction: 1 exactly when q+r >= 255.
func div255(v int) uint8 {
	return uint8((v + 1 + (v >> 8)) >> 8)
}

// markRow records an inclusive ink span on y. x0 and x1 are absolute pixels.
func (c *Coverage) markRow(y, x0, x1 int) {
	yi := y - c.bounds.Min.Y
	if yi < 0 || yi >= len(c.rowLo) {
		return
	}
	if c.rowLo[yi] < 0 {
		c.rowLo[yi] = x0
		c.rowHi[yi] = x1
	} else {
		if x0 < c.rowLo[yi] {
			c.rowLo[yi] = x0
		}
		if x1 > c.rowHi[yi] {
			c.rowHi[yi] = x1
		}
	}
	c.hasInk = true
}

func (c *Coverage) index(x, y int) int {
	return (y-c.bounds.Min.Y)*c.bounds.Dx() + (x - c.bounds.Min.X)
}

// Accumulate records max-alpha glyph coverage at x,y.
func (c *Coverage) Accumulate(x, y int, alpha uint8, fg color.NRGBA) {
	if alpha == 0 {
		return
	}
	b := c.bounds
	if x < b.Min.X || x >= b.Max.X || y < b.Min.Y || y >= b.Max.Y {
		return
	}
	i := c.index(x, y)
	if alpha > uint8(c.pixels[i]) {
		c.pixels[i] = packCoveragePixel(alpha, fg)
		c.markRow(y, x, x)
	}
}

// Composite blends accumulated glyph coverage over dst.
func (c *Coverage) Composite(dst *image.NRGBA) {
	if !c.hasInk || dst == nil {
		return
	}
	b := c.bounds.Intersect(dst.Bounds())
	if b.Empty() {
		return
	}
	dx := c.bounds.Dx()
	db := dst.Bounds()
	stride := dst.Stride
	pix := dst.Pix
	minX, minY := c.bounds.Min.X, c.bounds.Min.Y
	for y := b.Min.Y; y < b.Max.Y; y++ {
		yi := y - minY
		x0 := c.rowLo[yi]
		if x0 < 0 {
			continue
		}
		x1 := c.rowHi[yi] + 1
		if x0 < b.Min.X {
			x0 = b.Min.X
		}
		if x1 > b.Max.X {
			x1 = b.Max.X
		}
		if x0 >= x1 {
			continue
		}
		i := (y-minY)*dx + (x0 - minX)
		off := (y-db.Min.Y)*stride + (x0-db.Min.X)*4
		n := x1 - x0
		pixels := c.pixels
		for k := 0; k < n; {
			p := pixels[i]
			a := uint8(p)
			if a == 0 {
				i++
				off += 4
				k++
				for k+4 <= n && pixels[i]|pixels[i+1]|pixels[i+2]|pixels[i+3] == 0 {
					i += 4
					off += 16
					k += 4
				}
				continue
			}
			if a == 255 {
				binary.LittleEndian.PutUint32(pix[off:off+4], (p>>8)|0xff000000)
			} else {
				inv := 255 - int(a)
				ai := int(a)
				pix[off] = div255(int(uint8(p>>8))*ai + int(pix[off])*inv)
				pix[off+1] = div255(int(uint8(p>>16))*ai + int(pix[off+1])*inv)
				pix[off+2] = div255(int(uint8(p>>24))*ai + int(pix[off+2])*inv)
				pix[off+3] = 255
			}
			i++
			off += 4
			k++
		}
	}
}

// FgDecoration holds deferred underline/strike inputs.
type FgDecoration struct {
	Area image.Rectangle
	Cell *vt.Cell
	Fg   color.NRGBA
}

// queuedGlyph is a resolved glyph mask awaiting compositing.
type queuedGlyph struct {
	dr     image.Rectangle
	pix    []uint8
	stride int
	fg     color.NRGBA
	over   bool // dr overflows the cell area.
}

// overGlyph is an overflowing glyph and the top of its cell.
type overGlyph struct {
	dr image.Rectangle
	y  int
}

// Pass queues grid glyphs during a foreground pass and defers other glyphs.
type Pass struct {
	cov         Coverage
	glyphMasks  glyphMaskCache
	queue       []queuedGlyph
	over        []overGlyph // queued glyphs that overflow their cell.
	reach       int         // furthest any over glyph extends vertically from its cell top.
	unsorted    bool        // over is not ordered by cell top, so scan it fully.
	glyphs      []Layout
	decorations []FgDecoration
}

// AddGlyph adds layout to this pass: grid glyphs and sprites are queued for
// [Pass.Composite] using the pass's mask cache, and other glyphs are deferred
// to [Pass.DrawPending].
func (p *Pass) AddGlyph(ctx GlyphContext, layout *Layout) {
	switch layout.Kind {
	case KindGrid, KindSprite:
		dr, pix, stride, ok := layoutMaskPixels(ctx, layout, &p.glyphMasks)
		if !ok {
			return
		}
		over := !dr.In(layout.Area)
		if over {
			y := layout.Area.Min.Y
			p.unsorted = p.unsorted || (len(p.over) > 0 && y < p.over[len(p.over)-1].y)
			p.reach = max(p.reach, y-dr.Min.Y, dr.Max.Y-y)
			p.over = append(p.over, overGlyph{dr, y})
		}
		p.queue = append(p.queue, queuedGlyph{dr, pix, stride, layout.Fg, over})
	case KindPowerline, KindFallback, KindColor:
		p.glyphs = append(p.glyphs, *layout)
	}
}

// AppendDecoration queues decorations to draw after coverage compositing.
func (p *Pass) AppendDecoration(area image.Rectangle, cell *vt.Cell, fg color.NRGBA) {
	p.decorations = append(p.decorations, FgDecoration{Area: area, Cell: cell, Fg: fg})
}

// Reset prepares pass for a new foreground draw over bounds.
func (p *Pass) Reset(bounds image.Rectangle) {
	p.cov.reset(bounds)
	p.glyphMasks.releaseEvicted()
	p.queue = p.queue[:0]
	p.over = p.over[:0]
	p.reach, p.unsorted = 0, false
	p.glyphs = p.glyphs[:0]
	p.decorations = p.decorations[:0]
}

// Composite applies queued grid glyphs. Glyphs inside their cell cannot overlap
// each other, so those that miss every overflowing glyph blend straight into
// dst. The rest accumulate in queue order into coverage (max-alpha overlaps).
func (p *Pass) Composite(dst *image.NRGBA) {
	if dst == nil {
		return
	}
	for i := range p.queue {
		q := &p.queue[i]
		if q.over || p.overlapsOver(q.dr) {
			p.cov.ensure()
			accumulateGlyphMask(&p.cov, q.dr, q.pix, q.stride, q.fg)
			continue
		}
		blendMask(dst, p.cov.bounds, q)
	}
	p.cov.Composite(dst)
}

// overlapsOver reports whether r overlaps an over glyph. Those are in cell
// order, so only ones whose cell top is within reach of r can overlap.
func (p *Pass) overlapsOver(r image.Rectangle) bool {
	i := 0
	if !p.unsorted {
		i, _ = slices.BinarySearchFunc(p.over, r.Min.Y-p.reach, func(o overGlyph, y int) int { return cmp.Compare(o.y, y) })
	}
	for ; i < len(p.over) && (p.unsorted || p.over[i].y < r.Max.Y+p.reach); i++ {
		if p.over[i].dr.Overlaps(r) {
			return true
		}
	}
	return false
}

// blendMask blends q over dst, clipped to clip and the bounds of dst.
func blendMask(dst *image.NRGBA, clip image.Rectangle, q *queuedGlyph) {
	r := q.dr.Intersect(clip).Intersect(dst.Bounds())
	if r.Empty() {
		return
	}
	w := r.Dx()
	fr, fg, fb := int(q.fg.R), int(q.fg.G), int(q.fg.B)
	opaque := uint32(q.fg.R) | uint32(q.fg.G)<<8 | uint32(q.fg.B)<<16 | 0xff000000
	for y := r.Min.Y; y < r.Max.Y; y++ {
		i := (y-q.dr.Min.Y)*q.stride + r.Min.X - q.dr.Min.X
		row := q.pix[i : i+w]
		off := dst.PixOffset(r.Min.X, y)
		px := dst.Pix[off : off+4*w]
		for x := 0; x < w; x++ {
			// Most mask bytes are 0. A zero uint64 is eight zero alphas.
			if x&7 == 0 && x+8 <= w && binary.LittleEndian.Uint64(row[x:]) == 0 {
				x += 7
				continue
			}
			a := int(row[x])
			switch a {
			case 0:
			case 255:
				binary.LittleEndian.PutUint32(px[x*4:], opaque)
			default:
				inv := 255 - a
				px[x*4] = div255(fr*a + int(px[x*4])*inv)
				px[x*4+1] = div255(fg*a + int(px[x*4+1])*inv)
				px[x*4+2] = div255(fb*a + int(px[x*4+2])*inv)
				px[x*4+3] = 255
			}
		}
	}
}

// DrawPending renders deferred glyphs and decorations.
func (p *Pass) DrawPending(ctx GlyphContext, dst draw.Image) {
	for i := range p.glyphs {
		layout := &p.glyphs[i]
		if layout.Kind == KindColor {
			drawColorLayout(dst, layout)
			continue
		}
		if mask, ok := glyphLayoutMask(layout, &p.glyphMasks); ok {
			drawGlyphLayoutMask(dst, layout, mask)
		}
	}
	for i := range p.decorations {
		dec := &p.decorations[i]
		idraw.Decorations(ctx, dst, dec.Area, dec.Cell, dec.Fg)
	}
}
