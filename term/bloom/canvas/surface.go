// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package canvas

import (
	"image"
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"
)

// Surface is the area of cells an effect transforms. It is either a node's
// own cells, drawn on their own before being composited onto the frame, or a
// region of the frame itself, as with a backdrop. Coordinates are relative
// to the surface's top-left cell.
type Surface struct {
	c      *Canvas
	origin image.Point // where the surface's (0,0) is in c
	w, h   int
	mask   []bool // which cells are shown; nil when hiding is not possible
	maskAt int    // index in mask of the surface's (0,0)
	stride int    // length of a row of mask, the width of the surface it came from

	under *Canvas         // the frame beneath a node's cells; nil for a region of the frame
	rect  image.Rectangle // where the surface sits in the frame

	// Offset moves the surface when it is composited onto the frame. Effects
	// that move a node, like a slide, set it; the next effect sees [Surface.Under]
	// at the moved position.
	Offset image.Point
	// Viewport is the area of the frame the surface's node is placed in,
	// such as the screen for a dialog, which tells effects how far a node can
	// move before it is out of sight.
	Viewport image.Rectangle
	// Env describes the terminal, for resolving cells without a color.
	Env Env

	mixer *Mixer
}

// Region returns a surface over rect of c, whose cells effects change in
// place. Cells of a region cannot be hidden.
func (c *Canvas) Region(rect image.Rectangle, env Env, mixer *Mixer) *Surface {
	rect = rect.Intersect(c.Bounds())
	return &Surface{
		c: c, origin: rect.Min, w: rect.Dx(), h: rect.Dy(),
		rect: rect, Viewport: c.Bounds(), Env: env, mixer: mixer,
	}
}

// Layer returns a surface over all of c, a node's cells drawn on their own,
// which will be composited at rect of under, inside viewport. All of its
// cells start out shown.
func (c *Canvas) Layer(rect, viewport image.Rectangle, under *Canvas, env Env, mixer *Mixer) *Surface {
	mask := make([]bool, c.w*c.h)
	for i := range mask {
		mask[i] = true
	}
	return &Surface{
		c: c, w: c.w, h: c.h, mask: mask, stride: c.w, under: under,
		rect: rect, Viewport: viewport, Env: env, mixer: mixer,
	}
}

// Width returns the number of columns.
func (s *Surface) Width() int { return s.w }

// Height returns the number of rows.
func (s *Surface) Height() int { return s.h }

// Rect returns where the surface sits in the frame, before [Surface.Offset].
func (s *Surface) Rect() image.Rectangle { return s.rect }

// Cell returns the cell at x, y, or nil outside the surface. Effects may
// change the cell's content and style, but must keep its width.
func (s *Surface) Cell(x, y int) *uv.Cell {
	if x < 0 || y < 0 || x >= s.w || y >= s.h {
		return nil
	}
	return s.c.CellAt(s.origin.X+x, s.origin.Y+y)
}

// Under returns the frame's cell beneath x, y at the surface's current
// offset, or nil when there is none: off the frame, or for a region of the
// frame itself. It must not be changed.
func (s *Surface) Under(x, y int) *uv.Cell {
	if s.under == nil {
		return nil
	}
	p := s.rect.Min.Add(s.Offset).Add(image.Pt(x, y))
	return s.under.CellAt(p.X, p.Y)
}

// Visible reports whether the cell at x, y will be composited.
func (s *Surface) Visible(x, y int) bool {
	if x < 0 || y < 0 || x >= s.w || y >= s.h {
		return false
	}
	if s.mask == nil {
		return true
	}
	return s.mask[s.maskAt+y*s.stride+x]
}

// Hide stops the cell at x, y from being composited, so the frame beneath
// shows through. It does nothing on a region of the frame.
func (s *Surface) Hide(x, y int) {
	if s.mask != nil && x >= 0 && y >= 0 && x < s.w && y < s.h {
		s.mask[s.maskAt+y*s.stride+x] = false
	}
}

// Sub returns a surface over r, in s's coordinates, clipped to s. It shares
// s's cells and which of them are shown, so what an effect does to it shows
// in s. Its [Surface.Rect] is the matching part of the frame, and its
// offset starts out as s's, which the effect may change without changing s's.
func (s *Surface) Sub(r image.Rectangle) *Surface {
	r = r.Intersect(image.Rect(0, 0, s.w, s.h))
	if r.Empty() {
		r = image.Rectangle{}
	}
	sub := *s
	sub.origin = s.origin.Add(r.Min)
	sub.w, sub.h = r.Dx(), r.Dy()
	sub.maskAt = s.maskAt + r.Min.Y*s.stride + r.Min.X
	sub.rect = r.Add(s.rect.Min)
	return &sub
}

// Move shifts the cells of r by d, and which of them are shown with them.
// Cells shifted out of r are dropped, and cells of r left uncovered are
// blanked, and also hidden on a layer, so the frame beneath shows through; on
// a region of the frame, which cannot hide cells, they are only blanked.
//
// Wide glyphs stay whole: one cut by an edge of r, before or after the move,
// becomes blanks in its style.
func (s *Surface) Move(r image.Rectangle, d image.Point) {
	r = r.Intersect(image.Rect(0, 0, s.w, s.h))
	if r.Empty() || d == (image.Point{}) {
		return
	}
	// Rows are written toward where they came from, so none is overwritten
	// before it has been read.
	y, end, step := r.Min.Y, r.Max.Y, 1
	if d.Y > 0 {
		y, end, step = r.Max.Y-1, r.Min.Y-1, -1
	}
	covered := image.Rect(r.Min.X+d.X, r.Min.Y+d.Y, r.Max.X+d.X, r.Max.Y+d.Y).Intersect(r)
	for ; y != end; y += step {
		row, mask := s.row(y)
		if y >= covered.Min.Y && y < covered.Max.Y && !covered.Empty() {
			src, srcMask := s.row(y - d.Y)
			from := covered.Min.X - d.X
			copy(row[covered.Min.X:covered.Max.X], src[from:from+covered.Dx()])
			if mask != nil {
				copy(mask[covered.Min.X:covered.Max.X], srcMask[from:from+covered.Dx()])
			}
			blankSpan(row, mask, r.Min.X, covered.Min.X)
			blankSpan(row, mask, covered.Max.X, r.Max.X)
		} else {
			blankSpan(row, mask, r.Min.X, r.Max.X)
		}
		// Repaired against the whole row, as the halves of a glyph cut by
		// the edge of r may lie outside the surface.
		x0 := s.origin.X
		cutGlyphs(s.c.lines[s.origin.Y+y], x0+r.Min.X, x0+r.Max.X)
	}
}

// row returns the cells and the shown flags (nil on a region) of row y.
func (s *Surface) row(y int) (cells []uv.Cell, shown []bool) {
	x0 := s.origin.X
	cells = s.c.lines[s.origin.Y+y][x0 : x0+s.w]
	if s.mask == nil {
		return cells, nil
	}
	i := s.maskAt + y*s.stride
	return cells, s.mask[i : i+s.w]
}

// blankSpan blanks row[x0:x1] and hides it.
func blankSpan(row []uv.Cell, mask []bool, x0, x1 int) {
	for x := x0; x < x1; x++ {
		row[x] = uv.EmptyCell
		if mask != nil {
			mask[x] = false
		}
	}
}

// cutGlyphs blanks the wide glyphs in row[x0:x1] that are not whole, after
// a move, along with the halves just outside it that lost the other.
func cutGlyphs(row []uv.Cell, x0, x1 int) {
	for x := x0; x < x1; {
		w := row[x].Width
		whole := w > 0 && x+w <= x1
		for i := 1; whole && i < w; i++ {
			whole = row[x+i].Width == 0
		}
		if !whole {
			row[x] = blankLike(&row[x])
			x++
			continue
		}
		x += w
	}
	if x0 > 0 && row[x0-1].Width > 1 {
		row[x0-1] = blankLike(&row[x0-1])
	}
	for x := x1; x < len(row) && row[x].Width == 0; x++ {
		row[x] = blankLike(&row[x])
	}
}

// Fg returns the foreground color c shows, resolving no color to the
// terminal's default.
func (s *Surface) Fg(c *uv.Cell) color.Color {
	if c == nil || c.Style.Fg == nil {
		return s.Env.Fg
	}
	return c.Style.Fg
}

// Bg returns the background color c shows, resolving no color to the
// terminal's default.
func (s *Surface) Bg(c *uv.Cell) color.Color {
	if c == nil || c.Style.Bg == nil {
		return s.Env.Bg
	}
	return c.Style.Bg
}

// Mix returns the color t of the way from a to b; see [Mixer.Mix].
func (s *Surface) Mix(a, b color.Color, t float64) color.Color { return s.mixer.Mix(a, b, t) }

// Gray returns c moved t of the way toward gray; see [Mixer.Gray].
func (s *Surface) Gray(c color.Color, t float64) color.Color { return s.mixer.Gray(c, t) }

// Composite copies the shown cells of s, a surface from [Canvas.Layer], onto
// c at its rect moved by its offset, writing only inside clip. Wide glyphs
// are copied whole or not at all, by the visibility of their first cell.
func (c *Canvas) Composite(s *Surface, clip image.Rectangle) {
	at := s.rect.Min.Add(s.Offset)
	area := image.Rect(at.X, at.Y, at.X+s.w, at.Y+s.h).Intersect(clip).Intersect(c.Bounds())
	for y := area.Min.Y; y < area.Max.Y; y++ {
		row, mask := s.row(y - at.Y)
		for x := area.Min.X; x < area.Max.X; x++ {
			sx := x - at.X
			cell := &row[sx]
			switch {
			case cell.Width == 0:
				// The tail of a glyph whose lead is left of the clip shows
				// as a blank in its style, if its lead is shown.
				if lead := leadIndex(row, sx); x == area.Min.X && lead >= 0 && mask[lead] {
					blank := blankAt(row, lead)
					c.lines[y].Set(x, &blank)
				}
			case !mask[sx]:
			case x+cell.Width > area.Max.X:
				blank := blankLike(cell)
				for i := x; i < area.Max.X; i++ {
					c.lines[y].Set(i, &blank)
				}
			default:
				c.lines[y].Set(x, cell)
			}
		}
	}
}
