// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package raster

import (
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"

	uv "github.com/charmbracelet/ultraviolet"
	idraw "github.com/lrstanley/x/charm/still/internal/draw"
)

// Coverage accumulates per-pixel glyph mask coverage before compositing.
type Coverage struct {
	bounds image.Rectangle
	pixels []uint32 // alpha | R<<8 | G<<16 | B<<24

	// rowLo and rowHi are inclusive ink spans for each row of bounds.
	// rowLo[y] < 0 means the row has no ink. Composite visits only those
	// spans, so line-gap rows between glyphs are not scanned.
	rowLo  []int
	rowHi  []int
	hasInk bool
}

func packCoveragePixel(alpha uint8, fg color.NRGBA) uint32 {
	return uint32(alpha) | uint32(fg.R)<<8 | uint32(fg.G)<<16 | uint32(fg.B)<<24
}

func (c *Coverage) reset(bounds image.Rectangle) {
	n := bounds.Dx() * bounds.Dy()
	if cap(c.pixels) < n {
		c.pixels = make([]uint32, n)
	} else {
		c.pixels = c.pixels[:n]
		clear(c.pixels)
	}
	h := bounds.Dy()
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
	c.bounds = bounds
	c.hasInk = false
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
		end := off + (x1-x0-1)*4
		for {
			p := c.pixels[i]
			a := uint8(p)
			if a != 0 {
				if a == 255 {
					binary.LittleEndian.PutUint32(pix[off:off+4], (p>>8)|0xff000000)
				} else {
					inv := 255 - int(a)
					pix[off] = uint8((int(uint8(p>>8))*int(a) + int(pix[off])*inv) / 255)
					pix[off+1] = uint8((int(uint8(p>>16))*int(a) + int(pix[off+1])*inv) / 255)
					pix[off+2] = uint8((int(uint8(p>>24))*int(a) + int(pix[off+2])*inv) / 255)
					pix[off+3] = 255
				}
			}
			if off == end {
				break
			}
			i++
			off += 4
		}
	}
}

// FgDecoration holds deferred underline/strike inputs.
type FgDecoration struct {
	Area image.Rectangle
	Cell *uv.Cell
	Fg   color.NRGBA
}

// Pass accumulates grid glyphs during a foreground pass and defers other glyphs.
type Pass struct {
	cov         Coverage
	glyphMasks  glyphMaskCache
	glyphs      []Layout
	decorations []FgDecoration
}

// AccumulateGlyphLayout adds layout ink to this pass using its mask cache.
func (p *Pass) AccumulateGlyphLayout(ctx GlyphContext, layout *Layout) {
	accumulateGlyphLayout(ctx, &p.cov, layout, &p.glyphMasks)
}

// AppendGlyph queues a deferred non-grid glyph draw.
func (p *Pass) AppendGlyph(layout Layout) {
	p.glyphs = append(p.glyphs, layout)
}

// AppendDecoration queues decorations to draw after coverage compositing.
func (p *Pass) AppendDecoration(area image.Rectangle, cell *uv.Cell, fg color.NRGBA) {
	p.decorations = append(p.decorations, FgDecoration{Area: area, Cell: cell, Fg: fg})
}

// Reset prepares pass for a new foreground draw over bounds.
func (p *Pass) Reset(bounds image.Rectangle) {
	p.cov.reset(bounds)
	p.glyphs = p.glyphs[:0]
	p.decorations = p.decorations[:0]
}

// Composite applies accumulated grid glyph coverage.
func (p *Pass) Composite(dst *image.NRGBA) {
	p.cov.Composite(dst)
}

// DrawPending renders deferred glyphs and decorations.
func (p *Pass) DrawPending(ctx GlyphContext, dst draw.Image) {
	for i := range p.glyphs {
		DrawGlyphLayout(ctx, dst, &p.glyphs[i])
	}
	for i := range p.decorations {
		dec := &p.decorations[i]
		idraw.Decorations(ctx, dst, dec.Area, dec.Cell, dec.Fg)
	}
}
