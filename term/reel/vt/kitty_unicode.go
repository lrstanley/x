// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"image"
	"image/color"
	"math"
	"slices"
	"unicode/utf8"
)

// Unicode placeholders follow libghostty's src/terminal/kitty/graphics_unicode.zig,
// which its C API does not export: cells of U+10EEEE draw a virtual placement.
// The cell's foreground color is the image id, its underline color the
// placement id, and up to three combining marks from kittyDiacritics give the
// image row, the image column, and the image id's high byte.

// kittyVirtual is one virtual placement: the image, and the size in cells of
// the box its placeholder cells cover. Zero takes the size from the image.
type kittyVirtual struct {
	image      *Image
	id         uint32
	placement  uint32
	z          int32
	cols, rows int
}

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
func placeholderPlacements(grid *Buffer, virtual []kittyVirtual, cw, ch int) []Placement {
	if grid == nil || cw <= 0 || ch <= 0 {
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
				if p, drawn := v.render(*run, cw, ch); drawn {
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
	r, size := utf8.DecodeRuneInString(cell.Content)
	if r != kittyPlaceholder {
		return placeholderRun{}, false
	}
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
	i, ok := slices.BinarySearch(kittyDiacritics, r)
	if !ok {
		return -1
	}
	return i
}

// placeholderTarget finds the virtual placement placeholder cells for image
// id and placement pid draw. With no placement id, a placement with an id is
// preferred, the lowest first, as libghostty does.
func placeholderTarget(virtual []kittyVirtual, id, pid uint32) (kittyVirtual, bool) {
	var best kittyVirtual
	found := false
	for _, v := range virtual {
		if v.id != id {
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

// render returns the placement of run: v's image fit into the placement's
// box of cells, keeping its aspect ratio, centered, and clipped to the run's
// cells. The run's image row and column say where in the box it is.
func (v kittyVirtual) render(run placeholderRun, cw, ch int) (Placement, bool) {
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

// kittyDiacritics are the combining marks that number placeholder rows and
// columns, in order: the mark at index i means i.
var kittyDiacritics = []rune{
	0x0305, 0x030D, 0x030E, 0x0310, 0x0312, 0x033D, 0x033E, 0x033F,
	0x0346, 0x034A, 0x034B, 0x034C, 0x0350, 0x0351, 0x0352, 0x0357,
	0x035B, 0x0363, 0x0364, 0x0365, 0x0366, 0x0367, 0x0368, 0x0369,
	0x036A, 0x036B, 0x036C, 0x036D, 0x036E, 0x036F, 0x0483, 0x0484,
	0x0485, 0x0486, 0x0487, 0x0592, 0x0593, 0x0594, 0x0595, 0x0597,
	0x0598, 0x0599, 0x059C, 0x059D, 0x059E, 0x059F, 0x05A0, 0x05A1,
	0x05A8, 0x05A9, 0x05AB, 0x05AC, 0x05AF, 0x05C4, 0x0610, 0x0611,
	0x0612, 0x0613, 0x0614, 0x0615, 0x0616, 0x0617, 0x0657, 0x0658,
	0x0659, 0x065A, 0x065B, 0x065D, 0x065E, 0x06D6, 0x06D7, 0x06D8,
	0x06D9, 0x06DA, 0x06DB, 0x06DC, 0x06DF, 0x06E0, 0x06E1, 0x06E2,
	0x06E4, 0x06E7, 0x06E8, 0x06EB, 0x06EC, 0x0730, 0x0732, 0x0733,
	0x0735, 0x0736, 0x073A, 0x073D, 0x073F, 0x0740, 0x0741, 0x0743,
	0x0745, 0x0747, 0x0749, 0x074A, 0x07EB, 0x07EC, 0x07ED, 0x07EE,
	0x07EF, 0x07F0, 0x07F1, 0x07F3, 0x0816, 0x0817, 0x0818, 0x0819,
	0x081B, 0x081C, 0x081D, 0x081E, 0x081F, 0x0820, 0x0821, 0x0822,
	0x0823, 0x0825, 0x0826, 0x0827, 0x0829, 0x082A, 0x082B, 0x082C,
	0x082D, 0x0951, 0x0953, 0x0954, 0x0F82, 0x0F83, 0x0F86, 0x0F87,
	0x135D, 0x135E, 0x135F, 0x17DD, 0x193A, 0x1A17, 0x1A75, 0x1A76,
	0x1A77, 0x1A78, 0x1A79, 0x1A7A, 0x1A7B, 0x1A7C, 0x1B6B, 0x1B6D,
	0x1B6E, 0x1B6F, 0x1B70, 0x1B71, 0x1B72, 0x1B73, 0x1CD0, 0x1CD1,
	0x1CD2, 0x1CDA, 0x1CDB, 0x1CE0, 0x1DC0, 0x1DC1, 0x1DC3, 0x1DC4,
	0x1DC5, 0x1DC6, 0x1DC7, 0x1DC8, 0x1DC9, 0x1DCB, 0x1DCC, 0x1DD1,
	0x1DD2, 0x1DD3, 0x1DD4, 0x1DD5, 0x1DD6, 0x1DD7, 0x1DD8, 0x1DD9,
	0x1DDA, 0x1DDB, 0x1DDC, 0x1DDD, 0x1DDE, 0x1DDF, 0x1DE0, 0x1DE1,
	0x1DE2, 0x1DE3, 0x1DE4, 0x1DE5, 0x1DE6, 0x1DFE, 0x20D0, 0x20D1,
	0x20D4, 0x20D5, 0x20D6, 0x20D7, 0x20DB, 0x20DC, 0x20E1, 0x20E7,
	0x20E9, 0x20F0, 0x2CEF, 0x2CF0, 0x2CF1, 0x2DE0, 0x2DE1, 0x2DE2,
	0x2DE3, 0x2DE4, 0x2DE5, 0x2DE6, 0x2DE7, 0x2DE8, 0x2DE9, 0x2DEA,
	0x2DEB, 0x2DEC, 0x2DED, 0x2DEE, 0x2DEF, 0x2DF0, 0x2DF1, 0x2DF2,
	0x2DF3, 0x2DF4, 0x2DF5, 0x2DF6, 0x2DF7, 0x2DF8, 0x2DF9, 0x2DFA,
	0x2DFB, 0x2DFC, 0x2DFD, 0x2DFE, 0x2DFF, 0xA66F, 0xA67C, 0xA67D,
	0xA6F0, 0xA6F1, 0xA8E0, 0xA8E1, 0xA8E2, 0xA8E3, 0xA8E4, 0xA8E5,
	0xA8E6, 0xA8E7, 0xA8E8, 0xA8E9, 0xA8EA, 0xA8EB, 0xA8EC, 0xA8ED,
	0xA8EE, 0xA8EF, 0xA8F0, 0xA8F1, 0xAAB0, 0xAAB2, 0xAAB3, 0xAAB7,
	0xAAB8, 0xAABE, 0xAABF, 0xAAC1, 0xFE20, 0xFE21, 0xFE22, 0xFE23,
	0xFE24, 0xFE25, 0xFE26, 0x10A0F, 0x10A38, 0x1D185, 0x1D186, 0x1D187,
	0x1D188, 0x1D189, 0x1D1AA, 0x1D1AB, 0x1D1AC, 0x1D1AD, 0x1D242, 0x1D243,
	0x1D244,
}
