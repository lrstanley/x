// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package raster

import (
	"image"
	"image/color"
	"image/draw"

	uv "github.com/charmbracelet/ultraviolet"
	icol "github.com/lrstanley/x/charm/still/internal/color"
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
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			i := c.index(x, y)
			if c.alpha[i] == 0 {
				continue
			}
			bg := dst.NRGBAAt(x, y)
			dst.SetNRGBA(x, y, icol.BlendOverBg(c.fg[i], bg, c.alpha[i]))
		}
	}
}

// FgDecoration holds deferred underline/strike inputs.
type FgDecoration struct {
	Area image.Rectangle
	Cell *uv.Cell
	Fg   color.Color
}

// Pass accumulates grid glyphs during a foreground pass and defers other glyphs.
type Pass struct {
	cov         Coverage
	glyphs      []Layout
	decorations []FgDecoration
}

// Coverage returns the pass coverage accumulator.
func (p *Pass) Coverage() *Coverage {
	return &p.cov
}

// AppendGlyph queues a deferred non-grid glyph draw.
func (p *Pass) AppendGlyph(layout Layout) {
	p.glyphs = append(p.glyphs, layout)
}

// AppendDecoration queues decorations to draw after coverage compositing.
func (p *Pass) AppendDecoration(area image.Rectangle, cell *uv.Cell, fg color.Color) {
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
		DrawGlyphLayout(ctx, dst, p.glyphs[i])
	}
	for i := range p.decorations {
		dec := &p.decorations[i]
		idraw.Decorations(ctx, dst, dec.Area, dec.Cell, dec.Fg)
	}
}
