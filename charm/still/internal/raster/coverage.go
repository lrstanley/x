// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package raster

import (
	"image"
	"image/color"
	"image/draw"

	uv "github.com/charmbracelet/ultraviolet"
	idraw "github.com/lrstanley/x/charm/still/internal/draw"
)

// Coverage accumulates per-pixel glyph mask coverage before compositing.
type Coverage struct {
	bounds image.Rectangle
	alpha  []uint8
	fg     []color.NRGBA
}

func (c *Coverage) reset(bounds image.Rectangle) {
	n := bounds.Dx() * bounds.Dy()
	if len(c.alpha) < n {
		c.alpha = make([]uint8, n)
		c.fg = make([]color.NRGBA, n)
	} else {
		clear(c.alpha[:n])
	}
	c.bounds = bounds
}

func (c *Coverage) index(x, y int) int {
	return (y-c.bounds.Min.Y)*c.bounds.Dx() + (x - c.bounds.Min.X)
}

// Accumulate records max-alpha glyph coverage at x,y.
func (c *Coverage) Accumulate(x, y int, alpha uint8, fg color.NRGBA) {
	if alpha == 0 || !image.Pt(x, y).In(c.bounds) {
		return
	}
	i := c.index(x, y)
	if alpha > c.alpha[i] {
		c.alpha[i] = alpha
		c.fg[i] = fg
	}
}

// Composite blends accumulated glyph coverage over dst.
func (c *Coverage) Composite(dst *image.NRGBA) {
	b := c.bounds.Intersect(dst.Bounds())
	if b.Empty() {
		return
	}
	dx := c.bounds.Dx()
	db := dst.Bounds()
	stride := dst.Stride
	pix := dst.Pix
	for y := b.Min.Y; y < b.Max.Y; y++ {
		rowBase := (y-c.bounds.Min.Y)*dx + (b.Min.X - c.bounds.Min.X)
		off := (y-db.Min.Y)*stride + (b.Min.X-db.Min.X)*4
		for x := b.Min.X; x < b.Max.X; x++ {
			i := rowBase + (x - b.Min.X)
			a := c.alpha[i]
			if a == 0 {
				off += 4
				continue
			}
			fg := c.fg[i]
			if a == 255 {
				pix[off] = fg.R
				pix[off+1] = fg.G
				pix[off+2] = fg.B
				pix[off+3] = 255
			} else {
				af := float64(a) / 255
				inv := 1 - af
				pix[off] = uint8(float64(fg.R)*af + float64(pix[off])*inv)
				pix[off+1] = uint8(float64(fg.G)*af + float64(pix[off+1])*inv)
				pix[off+2] = uint8(float64(fg.B)*af + float64(pix[off+2])*inv)
				pix[off+3] = 255
			}
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

// Coverage returns the pass coverage accumulator.
func (p *Pass) Coverage() *Coverage {
	return &p.cov
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
