// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package screen

import "image"

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
	// lines are the rows, top to bottom. Each row has the same length.
	// Cells are values, so the buffer does not alias emulator memory.
	lines [][]Cell
	// origin is the absolute coordinate of lines[0][0]. Bounds start here.
	origin image.Point
	// Dirty reports which rows changed since the last ClearDirty. A nil
	// slice means every row is dirty. The index matches lines, not the
	// absolute Y coordinate.
	Dirty []bool
}

// NewBuffer returns a width by height grid of empty cells. Every row starts dirty.
func NewBuffer(width, height int) *Buffer {
	b := &Buffer{
		lines: make([][]Cell, height),
		Dirty: make([]bool, height),
	}
	for y := range b.lines {
		b.lines[y] = make([]Cell, width)
		for x := range b.lines[y] {
			b.lines[y][x] = EmptyCell
		}
		b.Dirty[y] = true
	}
	return b
}

// Bounds returns the grid rectangle.
func (b *Buffer) Bounds() image.Rectangle {
	if b == nil {
		return image.Rectangle{}
	}
	h := len(b.lines)
	w := 0
	if h > 0 {
		w = len(b.lines[0])
	}
	return image.Rectangle{
		Min: b.origin,
		Max: b.origin.Add(image.Pt(w, h)),
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
	if row < 0 || row >= len(b.lines) || col < 0 || col >= len(b.lines[row]) {
		return nil
	}
	return &b.lines[row][col]
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
	if row < 0 || row >= len(b.lines) || col < 0 || col >= len(b.lines[row]) {
		return
	}
	b.lines[row][col] = c
	if row < len(b.Dirty) {
		b.Dirty[row] = true
	}
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
	if row < 0 || row >= len(b.lines) {
		return
	}
	line := b.lines[row]
	if col < 0 || col >= len(line) {
		return
	}
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

// Clone returns a deep copy. Dirty flags are copied too.
func (b *Buffer) Clone() *Buffer {
	if b == nil {
		return nil
	}
	out := &Buffer{
		origin: b.origin,
		lines:  make([][]Cell, len(b.lines)),
		Dirty:  append([]bool(nil), b.Dirty...),
	}
	for y, line := range b.lines {
		out.lines[y] = append([]Cell(nil), line...)
	}
	return out
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
	if b == nil || len(b.lines) == 0 {
		return ""
	}
	n := 0
	for _, line := range b.lines {
		for _, c := range line {
			n += len(c.Content)
		}
		n++
	}
	buf := make([]byte, 0, n)
	for i, line := range b.lines {
		if i > 0 {
			buf = append(buf, '\n')
		}
		for _, c := range line {
			if c.Width == 0 {
				continue
			}
			if c.Content == "" {
				buf = append(buf, ' ')
				continue
			}
			buf = append(buf, c.Content...)
		}
	}
	return string(buf)
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
