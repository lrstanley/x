// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"image"
	"image/color"
	"math"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi/kitty"
)

// Unicode placeholders follow libghostty's src/terminal/kitty/graphics_unicode.zig,
// which its C API does not export: cells of U+10EEEE draw a virtual placement.
// The cell's foreground color is the image id, its underline color the
// placement id, and up to three combining marks from kittyDiacritics give the
// image row, the image column, and the image id's high byte.

// placeholderRun is a run of placeholder cells on one row that draw
// consecutive columns of one image row.
type placeholderRun struct {
	x, y          int
	id, placement uint32
	col, row      int
	width         int
}

// placeholderPlacements returns the placements drawn by grid's placeholder
// cells, for cells of cw by ch pixels.
func placeholderPlacements(grid *Buffer, virtual []kittyEntry, cw, ch int) []Placement {
	if grid == nil || len(virtual) == 0 || cw <= 0 || ch <= 0 {
		return nil
	}
	var out []Placement
	for y := range grid.Height() {
		var run *placeholderRun
		flush := func() {
			if run == nil {
				return
			}
			if v, ok := placeholderTarget(virtual, run.id, run.placement); ok {
				if p, drawn := renderPlaceholder(v, *run, cw, ch); drawn {
					out = append(out, p)
				}
			}
			run = nil
		}
		for x := range grid.Width() {
			cell := grid.CellAt(x, y)
			cur, ok := parsePlaceholder(cell)
			if !ok {
				flush()
				continue
			}
			cur.x, cur.y = x, y
			if run != nil && run.extends(cur) {
				run.width++
				continue
			}
			flush()
			if cur.row < 0 {
				cur.row = 0
			}
			if cur.col < 0 {
				cur.col = 0
			}
			run = &cur
		}
		flush()
	}
	return out
}

// parsePlaceholder reads a placeholder cell. row, col, and the high byte are
// -1 when the cell leaves them out, which continues the run before it.
func parsePlaceholder(cell *Cell) (placeholderRun, bool) {
	if !cell.KittyPlaceholder() {
		return placeholderRun{}, false
	}
	_, size := utf8.DecodeRuneInString(cell.Content)
	p := placeholderRun{id: colorID(cell.Style.Fg), placement: colorID(cell.Style.UnderlineColor), row: -1, col: -1, width: 1}
	marks := cell.Content[size:]
	high := -1
	for i := 0; i < 3 && marks != ""; i++ {
		m, n := utf8.DecodeRuneInString(marks)
		marks = marks[n:]
		idx := diacriticIndex(m)
		switch i {
		case 0:
			p.row = idx
		case 1:
			p.col = idx
		case 2:
			if idx <= 0xff {
				high = idx
			}
		}
	}
	if high > 0 {
		p.id |= uint32(high) << 24 //nolint:gosec // high is a byte.
	}
	return p, true
}

// extends reports whether next continues r on the same row.
func (r *placeholderRun) extends(next placeholderRun) bool {
	return next.id == r.id && next.placement == r.placement &&
		(next.row < 0 || next.row == r.row) &&
		(next.col < 0 || next.col == r.col+r.width)
}

// colorID is the 24-bit id a placeholder cell's color encodes.
func colorID(c color.Color) uint32 {
	switch c := c.(type) {
	case nil:
		return 0
	case Indexed:
		return uint32(c)
	case color.NRGBA:
		return uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
	}
	r, g, b, _ := c.RGBA()
	return (r>>8)<<16 | (g>>8)<<8 | b>>8
}

// diacriticIndex is the value of combining mark r, or -1.
func diacriticIndex(r rune) int {
	if i, ok := kittyDiacritics[r]; ok {
		return i
	}
	return -1
}

// placeholderTarget finds the virtual placement placeholder cells for image
// id and placement pid draw. With no placement id, a placement with an id is
// preferred, the lowest first, as libghostty does.
func placeholderTarget(virtual []kittyEntry, id, pid uint32) (kittyEntry, bool) {
	var best kittyEntry
	found := false
	for _, v := range virtual {
		if v.image.ID != id {
			continue
		}
		if pid > 0 {
			if v.placement == pid {
				return v, true
			}
			continue
		}
		if !found || (v.placement != 0 && (best.placement == 0 || v.placement < best.placement)) {
			best, found = v, true
		}
	}
	return best, found
}

// renderPlaceholder returns the placement of run: v's image fit into v's box
// of cells, keeping its aspect ratio, centered, and clipped to the run's
// cells. The run's image row and column say where in the box it is.
func renderPlaceholder(v kittyEntry, run placeholderRun, cw, ch int) (Placement, bool) {
	iw, ih := float64(v.image.Pixels.Rect.Dx()), float64(v.image.Pixels.Rect.Dy())
	cols, rows := v.cols, v.rows
	if rows == 0 {
		rows = (int(ih) + ch - 1) / ch
	}
	if cols == 0 {
		cols = (int(iw) + cw - 1) / cw
	}
	if cols <= 0 || rows <= 0 || iw <= 0 || ih <= 0 || run.col >= cols || run.row >= rows {
		return Placement{}, false
	}
	boxW, boxH := float64(cols*cw), float64(rows*ch)
	scale := min(boxW/iw, boxH/ih)
	w, h := int(math.Round(iw*scale)), int(math.Round(ih*scale))
	box := image.Pt((run.x-run.col)*cw, (run.y-run.row)*ch)
	at := box.Add(image.Pt((cols*cw-w)/2, (rows*ch-h)/2))
	return Placement{
		Image:       v.image,
		PlacementID: v.placement,
		Z:           v.z,
		Rect:        image.Rectangle{Min: at, Max: at.Add(image.Pt(w, h))},
		Source:      v.image.Pixels.Rect,
		Clip:        image.Rect(run.x*cw, run.y*ch, (run.x+run.width)*cw, (run.y+1)*ch),
	}, true
}

// kittyDiacriticCount is the length of the protocol's table of combining
// marks, which [kitty.Diacritic] indexes.
const kittyDiacriticCount = 297

// kittyDiacritics maps each of the combining marks that number placeholder
// rows and columns to its value.
var kittyDiacritics = func() map[rune]int {
	m := make(map[rune]int, kittyDiacriticCount)
	for i := range kittyDiacriticCount {
		m[kitty.Diacritic(i)] = i
	}
	return m
}()

// KittyPlaceholder reports whether c is a Kitty graphics Unicode placeholder
// cell, which draws part of an image (see [Buffer.Images]) instead of text.
func (c *Cell) KittyPlaceholder() bool {
	r, _ := utf8.DecodeRuneInString(c.Content)
	return r == kittyPlaceholder
}
