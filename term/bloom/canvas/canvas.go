// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package canvas holds the cell grids the layout engine composites: a
// [Canvas] of cells, parsed once from styled strings and cached by [Cache],
// copied onto a frame with [Canvas.Blit], and handed to effects as a
// [Surface].
package canvas

import (
	"image"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// Canvas is a grid of terminal cells, stored as one slice so that copying
// rows is a memmove.
type Canvas struct {
	w, h     int
	cells    []uv.Cell
	lines    []uv.Line // views into cells, one per row
	method   ansi.Method
	rendered int // length of the last Render, to size the next one
}

var _ uv.Screen = (*Canvas)(nil)

// New returns a canvas of w×h blank cells, measuring graphemes with method.
func New(w, h int, method ansi.Method) *Canvas {
	c := &Canvas{method: method}
	c.Resize(w, h)
	return c
}

// Resize changes the size of the canvas, reusing its storage when it is big
// enough, and blanks every cell.
func (c *Canvas) Resize(w, h int) {
	w, h = max(w, 0), max(h, 0)
	if cap(c.cells) < w*h {
		c.cells = make([]uv.Cell, w*h)
	}
	c.cells = c.cells[:w*h]
	if c.w != w || c.h != h || len(c.lines) != h {
		c.w, c.h = w, h
		c.lines = c.lines[:0]
		for y := range h {
			c.lines = append(c.lines, uv.Line(c.cells[y*w:(y+1)*w:(y+1)*w]))
		}
	}
	c.Clear()
}

// Clear blanks every cell.
func (c *Canvas) Clear() {
	for i := range c.cells {
		c.cells[i] = uv.EmptyCell
	}
}

// Width returns the number of columns.
func (c *Canvas) Width() int { return c.w }

// Height returns the number of rows.
func (c *Canvas) Height() int { return c.h }

// Bounds implements [uv.Screen].
func (c *Canvas) Bounds() image.Rectangle { return image.Rect(0, 0, c.w, c.h) }

// CellAt implements [uv.Screen]. It returns nil outside the canvas.
func (c *Canvas) CellAt(x, y int) *uv.Cell {
	if x < 0 || y < 0 || x >= c.w || y >= c.h {
		return nil
	}
	return &c.cells[y*c.w+x]
}

// SetCell implements [uv.Screen]. Overwriting part of a wide glyph blanks the
// rest of it, as [uv.Line.Set] does.
func (c *Canvas) SetCell(x, y int, cell *uv.Cell) {
	if y >= 0 && y < c.h {
		c.lines[y].Set(x, cell)
	}
}

// WidthMethod implements [uv.Screen].
func (c *Canvas) WidthMethod() uv.WidthMethod { return c.method }

// Render returns the canvas as a styled string, one line per row, as
// [uv.Lines.Render] does.
func (c *Canvas) Render() string {
	var b strings.Builder
	b.Grow(c.rendered)
	for y, l := range c.lines {
		if y > 0 {
			b.WriteByte('\n')
		}
		renderLine(&b, l)
	}
	c.rendered = b.Len()
	return b.String()
}

// renderLine writes l as [uv.Line.Render] does, into a builder the caller
// sized, which saves the copies of growing one per line.
func renderLine(b *strings.Builder, l uv.Line) {
	var pen uv.Style
	var link uv.Link
	spaces := 0 // blanks not yet written, dropped at the end of the line
	for i := range l {
		c := &l[i]
		if c.Width == 0 && c.Content == "" {
			continue // the tail of a wide glyph
		}
		if c.Content == " " && c.Width == 1 && c.Style.IsZero() && c.Link.IsZero() {
			if !pen.IsZero() {
				b.WriteString(ansi.ResetStyle)
				pen = uv.Style{}
			}
			if !link.IsZero() {
				b.WriteString(ansi.ResetHyperlink())
				link = uv.Link{}
			}
			spaces++
			continue
		}
		for ; spaces > 0; spaces-- {
			b.WriteByte(' ')
		}
		if c.Style.IsZero() && !pen.IsZero() {
			b.WriteString(ansi.ResetStyle)
			pen = uv.Style{}
		}
		if !c.Style.Equal(&pen) {
			b.WriteString(uv.StyleDiff(&pen, &c.Style))
			pen = c.Style
		}
		if c.Link != link && link.URL != "" {
			b.WriteString(ansi.ResetHyperlink())
			link = uv.Link{}
		}
		if c.Link != link {
			b.WriteString(ansi.SetHyperlink(c.Link.URL, c.Link.Params))
			link = c.Link
		}
		b.WriteString(c.Content)
	}
	for ; spaces > 0; spaces-- {
		b.WriteByte(' ')
	}
	if link.URL != "" {
		b.WriteString(ansi.ResetHyperlink())
	}
	if !pen.IsZero() {
		b.WriteString(ansi.ResetStyle)
	}
}

// String returns the text of the canvas without styles, with trailing spaces
// trimmed from each line.
func (c *Canvas) String() string { return uv.Lines(c.lines).String() }

// Blit copies src onto c with src's top-left cell at r.Min, and blanks the
// cells of r that src does not cover, writing only inside clip. Cells without
// a background of their own keep the one already in c, so text drawn over a
// colored box sits on the box's color, as it would inside a Lip Gloss style
// with a background. A wide glyph cut by clip becomes blanks in its own
// style, and a wide glyph already in c that the copy cuts is blanked too, so
// the canvas never holds half a glyph.
func (c *Canvas) Blit(src *Canvas, r, clip image.Rectangle) {
	area := r.Intersect(clip).Intersect(c.Bounds())
	if area.Empty() {
		return
	}
	at := r.Min
	for y := area.Min.Y; y < area.Max.Y; y++ {
		dst := c.lines[y]
		x0, x1 := area.Min.X, area.Max.X

		// A copy that starts on the tail of a wide glyph in c leaves its
		// lead behind, which must not survive half covered.
		if dst[x0].Width == 0 {
			blankLead(dst, x0)
		}
		var row uv.Line
		if y-at.Y < src.h {
			row = src.lines[y-at.Y]
		}
		copied := min(max(at.X+len(row), x0), x1)
		if !hasBg(dst[x0:x1]) {
			// Nothing beneath has a background to keep: a plain copy.
			if copied > x0 {
				copy(dst[x0:copied], row[x0-at.X:])
			}
			for x := copied; x < x1; x++ {
				dst[x] = uv.EmptyCell
			}
		} else {
			for x := x0; x < x1; x++ {
				bg := dst[x].Style.Bg
				if x < copied {
					dst[x] = row[x-at.X]
				} else {
					dst[x] = uv.EmptyCell
				}
				if dst[x].Style.Bg == nil {
					dst[x].Style.Bg = bg
				}
			}
		}
		fixEdges(dst, row, x0, x1, at.X)
	}
}

func hasBg(l uv.Line) bool {
	for i := range l {
		if l[i].Style.Bg != nil {
			return true
		}
	}
	return false
}

// ClearArea blanks the cells of r, as a fresh canvas is, blanking any wide
// glyph it cuts.
func (c *Canvas) ClearArea(r image.Rectangle) {
	r = r.Intersect(c.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		dst := c.lines[y]
		if dst[r.Min.X].Width == 0 {
			blankLead(dst, r.Min.X)
		}
		for x := r.Min.X; x < r.Max.X; x++ {
			dst[x] = uv.EmptyCell
		}
		fixEdges(dst, nil, r.Min.X, r.Max.X, 0)
	}
}

// fixEdges repairs the glyphs a row copy into dst[x0:x1] cut at either end:
// tails copied without their lead, a lead copied without its whole tail,
// and tails left in dst after the span whose lead the copy overwrote.
func fixEdges(dst, src uv.Line, x0, x1, offset int) {
	for x := x0; x < x1 && dst[x].Width == 0; x++ {
		dst[x] = blankAt(src, leadIndex(src, x-offset))
	}
	for x := x1 - 1; x >= x0; x-- {
		if w := dst[x].Width; w > 0 {
			if x+w > x1 {
				blank := blankLike(&dst[x])
				for i := x; i < x1; i++ {
					dst[i] = blank
				}
			}
			break
		}
	}
	for x := x1; x < len(dst) && dst[x].Width == 0; x++ {
		dst[x] = blankLike(&dst[x])
	}
}

// blankLead blanks the wide glyph whose tail is at x, from its lead up to x.
func blankLead(l uv.Line, x int) {
	lead := leadIndex(l, x)
	if lead < 0 {
		return
	}
	blank := blankAt(l, lead)
	for i := lead; i < x; i++ {
		l[i] = blank
	}
}

// leadIndex returns the index in l of the glyph covering x, which is x
// itself unless x is the tail of a wide glyph, or -1 if there is none.
func leadIndex(l uv.Line, x int) int {
	for x = min(x, len(l)-1); x >= 0; x-- {
		if l[x].Width > 0 {
			return x
		}
	}
	return -1
}

// blankAt returns a blank cell in the style and link of l[i], or a plain
// blank when i is -1.
func blankAt(l uv.Line, i int) uv.Cell {
	if i < 0 {
		return uv.EmptyCell
	}
	return blankLike(&l[i])
}

// blankLike returns a blank cell in the style and link of c.
func blankLike(c *uv.Cell) uv.Cell {
	blank := *c
	blank.Empty()
	return blank
}

// Screen returns a [uv.Screen] for drawing onto c as if its top-left cell
// were at origin, writing only inside clip. Glyphs that would cross clip are
// drawn as blanks, so a [uv.Drawable] cannot write outside its area.
func (c *Canvas) Screen(origin, size image.Point, clip image.Rectangle) uv.Screen {
	return &screen{c: c, origin: origin, size: size, clip: clip.Intersect(c.Bounds())}
}

type screen struct {
	c      *Canvas
	origin image.Point
	size   image.Point
	clip   image.Rectangle
}

func (s *screen) Bounds() image.Rectangle     { return image.Rectangle{Max: s.size} }
func (s *screen) WidthMethod() uv.WidthMethod { return s.c.method }

func (s *screen) CellAt(x, y int) *uv.Cell {
	p := s.origin.Add(image.Pt(x, y))
	if !p.In(s.clip) {
		return nil
	}
	return s.c.CellAt(p.X, p.Y)
}

func (s *screen) SetCell(x, y int, cell *uv.Cell) {
	p := s.origin.Add(image.Pt(x, y))
	if !p.In(s.clip) {
		return
	}
	if cell != nil && p.X+cell.Width > s.clip.Max.X {
		blank := blankLike(cell)
		for i := p.X; i < s.clip.Max.X; i++ {
			s.c.SetCell(i, p.Y, &blank)
		}
		return
	}
	s.c.SetCell(p.X, p.Y, cell)
}
