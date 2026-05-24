// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image"
	"image/color"
	"image/draw"

	uv "github.com/charmbracelet/ultraviolet"
)

// glyphCoverage accumulates per-pixel glyph mask coverage before a single
// composite over the background. Max-alpha accumulation prevents antialiased
// ink from adjacent cells stacking via [draw.Over] at cell seams.
type glyphCoverage struct {
	bounds image.Rectangle
	alpha  []uint8
	fg     []color.NRGBA
}

func (c *glyphCoverage) reset(bounds image.Rectangle) {
	n := bounds.Dx() * bounds.Dy()
	if len(c.alpha) < n {
		c.alpha = make([]uint8, n)
		c.fg = make([]color.NRGBA, n)
	} else {
		clear(c.alpha[:n])
	}
	c.bounds = bounds
}

func (c *glyphCoverage) index(x, y int) int {
	return (y-c.bounds.Min.Y)*c.bounds.Dx() + (x - c.bounds.Min.X)
}

func (c *glyphCoverage) accumulate(x, y int, alpha uint8, fg color.NRGBA) {
	if alpha == 0 || !image.Pt(x, y).In(c.bounds) {
		return
	}
	i := c.index(x, y)
	if alpha > c.alpha[i] {
		c.alpha[i] = alpha
		c.fg[i] = fg
	}
}

func (c *glyphCoverage) composite(dst *image.NRGBA) {
	b := c.bounds.Intersect(dst.Bounds())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			i := c.index(x, y)
			if c.alpha[i] == 0 {
				continue
			}
			bg := dst.NRGBAAt(x, y)
			dst.SetNRGBA(x, y, blendOverBg(c.fg[i], bg, c.alpha[i]))
		}
	}
}

func blendOverBg(fg, bg color.NRGBA, alpha uint8) color.NRGBA {
	if alpha == 255 {
		return fg
	}
	if alpha == 0 {
		return bg
	}
	a := float64(alpha) / 255
	return color.NRGBA{
		R: uint8(float64(fg.R)*a + float64(bg.R)*(1-a)),
		G: uint8(float64(fg.G)*a + float64(bg.G)*(1-a)),
		B: uint8(float64(fg.B)*a + float64(bg.B)*(1-a)),
		A: 255,
	}
}

// fgDecoration holds cell underline/strike inputs deferred until after coverage
// compositing on *image.NRGBA destinations.
type fgDecoration struct {
	area image.Rectangle
	cell *uv.Cell
	fg   color.Color
}

// glyphCoveragePass accumulates [glyphGrid] masks during a foreground pass
// and defers [glyphPowerline]/[glyphNerdIcon] glyphs and decorations until
// after compositing.
type glyphCoveragePass struct {
	cov         *glyphCoverage
	glyphs      []glyphLayout
	decorations []fgDecoration
}

func (p *glyphCoveragePass) reset(cov *glyphCoverage, bounds image.Rectangle) {
	p.cov = cov
	cov.reset(bounds)
	p.glyphs = p.glyphs[:0]
	p.decorations = p.decorations[:0]
}

func (p *glyphCoveragePass) composite(dst *image.NRGBA) {
	p.cov.composite(dst)
}

func (p *glyphCoveragePass) drawPending(ctx Context, dst draw.Image) {
	for i := range p.glyphs {
		drawGlyphLayout(ctx, dst, p.glyphs[i])
	}
	for i := range p.decorations {
		dec := &p.decorations[i]
		drawDecorations(ctx, dst, dec.area, dec.cell, dec.fg)
	}
}
