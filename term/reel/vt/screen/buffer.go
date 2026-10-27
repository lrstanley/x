// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package screen

import (
	"image"
	"strings"
	"sync/atomic"
)

// Screen is a cell grid. Coordinates are absolute: [Screen.Bounds] may
// start at an origin other than (0, 0).
type Screen interface {
	Bounds() image.Rectangle
	CellAt(x, y int) *Cell
	SetCell(x, y int, c *Cell)
}

// Buffer is a dense cell grid. Dirty reports which rows changed since the
// last [Buffer.ClearDirty] call. A nil Dirty slice means every row is dirty.
type Buffer struct {
	// cells is the grid in row-major order, index y*width+x.
	// Cells are values, so the buffer does not alias emulator memory.
	cells []Cell
	// width and height are the grid size in cells. Height 0 reports width 0,
	// matching a grid with no rows.
	width  int
	height int
	// origin is the absolute coordinate of cell (0, 0). Bounds start here.
	origin image.Point
	// Dirty reports which rows changed since the last ClearDirty. A nil
	// slice means every row is dirty. The index matches the row, not the
	// absolute Y coordinate.
	Dirty []bool
	// wrapped marks the rows that are soft-wrapped, by row index.
	wrapped []bool
	// gen identifies cell contents. It changes when cells change and stays
	// put across ClearDirty. Clones copy it.
	gen uint64
}

// bufGen issues content generations. Clones share the source generation
// until one side is edited.
var bufGen atomic.Uint64

// NewBuffer returns a width by height grid of empty cells. Every row starts dirty.
func NewBuffer(width, height int) *Buffer {
	dirty := make([]bool, height)
	w := width
	if height == 0 {
		w = 0
	}
	b := &Buffer{
		cells:   make([]Cell, w*height),
		width:   w,
		height:  height,
		Dirty:   dirty,
		wrapped: make([]bool, height),
	}
	for i := range b.cells {
		b.cells[i] = EmptyCell
	}
	for y := range b.Dirty {
		b.Dirty[y] = true
	}
	b.advanceGen()
	return b
}

// Generation identifies cell contents. Equal values mean equal cells,
// including across ClearDirty. Zero means the buffer was not built by
// NewBuffer or Clone.
func (b *Buffer) Generation() uint64 {
	if b == nil {
		return 0
	}
	return b.gen
}

// AdvanceGeneration records an in-place cell edit. SetCell and SetRaw
// already do this. ClearDirty does not.
func (b *Buffer) AdvanceGeneration() {
	if b == nil {
		return
	}
	b.advanceGen()
}

func (b *Buffer) advanceGen() {
	b.gen = bufGen.Add(1)
}

// Blinks reports whether any drawn cell has a blink attribute.
func (b *Buffer) Blinks() bool {
	if b == nil {
		return false
	}
	const mask = AttrBlink | AttrRapidBlink
	for i := range b.cells {
		c := &b.cells[i]
		if c.Width == 0 {
			continue
		}
		if c.Style.Attrs&mask != 0 {
			return true
		}
	}
	return false
}

// Bounds returns the grid rectangle.
func (b *Buffer) Bounds() image.Rectangle {
	if b == nil {
		return image.Rectangle{}
	}
	return image.Rectangle{
		Min: b.origin,
		Max: b.origin.Add(image.Pt(b.width, b.height)),
	}
}

// Width returns the grid width in cells.
func (b *Buffer) Width() int { return b.Bounds().Dx() }

// Height returns the grid height in cells.
func (b *Buffer) Height() int { return b.Bounds().Dy() }

// CellAt returns the cell at x, y, or nil when the point is outside the grid.
func (b *Buffer) CellAt(x, y int) *Cell {
	if b == nil {
		return nil
	}
	col := x - b.origin.X
	row := y - b.origin.Y
	if row < 0 || row >= b.height || col < 0 || col >= b.width {
		return nil
	}
	return &b.cells[row*b.width+col]
}

// SetRaw writes c exactly, including a width-0 spacer. The emulator uses
// this for cells it has already shaped. [Buffer.SetCell] repairs wide spans
// and is the one tests should call.
func (b *Buffer) SetRaw(x, y int, c Cell) {
	if b == nil {
		return
	}
	col := x - b.origin.X
	row := y - b.origin.Y
	if row < 0 || row >= b.height || col < 0 || col >= b.width {
		return
	}
	b.cells[row*b.width+col] = c
	if row < len(b.Dirty) {
		b.Dirty[row] = true
	}
	b.advanceGen()
}

// SetCell writes c at x, y. A nil cell becomes an empty cell. Wide cells
// occupy the following columns with width 0 spacers. Writing into a wide
// cell clears the rest of that span first so a later draw does not keep a
// stale tail.
func (b *Buffer) SetCell(x, y int, c *Cell) {
	if b == nil {
		return
	}
	col := x - b.origin.X
	row := y - b.origin.Y
	if row < 0 || row >= b.height || col < 0 || col >= b.width {
		return
	}
	line := b.cells[row*b.width : (row+1)*b.width]
	clearWide(line, col)
	if c == nil {
		line[col] = EmptyCell
	} else {
		line[col] = *c
		cw := c.Width
		if col+cw > len(line) {
			for i := 0; i < cw && col+i < len(line); i++ {
				line[col+i] = *c
				line[col+i].Empty()
			}
		} else if cw > 1 {
			for j := 1; j < cw && col+j < len(line); j++ {
				line[col+j] = Cell{}
			}
		}
	}
	if row < len(b.Dirty) {
		b.Dirty[row] = true
	}
	b.advanceGen()
}

// Wrapped reports whether row y is soft-wrapped: its line ran past the
// last column and goes on at the start of the row below, rather than
// ending there. Rows outside the grid are not.
func (b *Buffer) Wrapped(y int) bool {
	if b == nil {
		return false
	}
	row := y - b.origin.Y
	return row >= 0 && row < len(b.wrapped) && b.wrapped[row]
}

// SetWrapped sets whether row y is soft-wrapped (see [Buffer.Wrapped]). It
// changes no cell, so it leaves the dirty flags and the generation alone.
func (b *Buffer) SetWrapped(y int, on bool) {
	if b == nil {
		return
	}
	if row := y - b.origin.Y; row >= 0 && row < len(b.wrapped) {
		b.wrapped[row] = on
	}
}

// RowDirty reports whether row y should be redrawn. Rows are dirty when
// Dirty is nil or the flag is set.
func (b *Buffer) RowDirty(y int) bool {
	if b == nil || b.Dirty == nil {
		return true
	}
	row := y - b.origin.Y
	if row < 0 || row >= len(b.Dirty) {
		return false
	}
	return b.Dirty[row]
}

// Clone returns a deep copy. Dirty flags and the content generation are copied too.
func (b *Buffer) Clone() *Buffer {
	if b == nil {
		return nil
	}
	return &Buffer{
		origin:  b.origin,
		cells:   append([]Cell(nil), b.cells...),
		width:   b.width,
		height:  b.height,
		Dirty:   append([]bool(nil), b.Dirty...),
		wrapped: append([]bool(nil), b.wrapped...),
		gen:     b.gen,
	}
}

// ClearDirty marks every row unchanged. The next [Buffer.SetCell] marks
// that row dirty again.
func (b *Buffer) ClearDirty() {
	for i := range b.Dirty {
		b.Dirty[i] = false
	}
}

// Text returns the viewport as plain lines joined by newlines. Blank cells
// are spaces and trailing spaces on each line are kept, so the line width
// matches the grid.
func (b *Buffer) Text() string {
	if b == nil || b.height == 0 {
		return ""
	}
	n := b.height - 1
	for i := range b.cells {
		c := &b.cells[i]
		if c.Width == 0 {
			continue
		}
		if c.Content == "" {
			n++
			continue
		}
		n += len(c.Content)
	}
	var buf strings.Builder
	buf.Grow(n)
	for row := 0; row < b.height; row++ {
		if row > 0 {
			buf.WriteByte('\n')
		}
		line := b.cells[row*b.width : (row+1)*b.width]
		for _, c := range line {
			if c.Width == 0 {
				continue
			}
			if c.Content == "" {
				buf.WriteByte(' ')
				continue
			}
			buf.WriteString(c.Content)
		}
	}
	return buf.String()
}

func clearWide(line []Cell, col int) {
	prev := line[col]
	if prev.Width > 1 {
		for j := 0; j < prev.Width && col+j < len(line); j++ {
			line[col+j] = EmptyCell
		}
		return
	}
	if prev.Width != 0 {
		return
	}
	for j := 1; col-j >= 0; j++ {
		wide := line[col-j]
		if wide.Width > 1 && j < wide.Width {
			for k := 0; k < wide.Width && col-j+k < len(line); k++ {
				line[col-j+k] = EmptyCell
			}
			return
		}
	}
}
