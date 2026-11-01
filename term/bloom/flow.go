// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package bloom

import (
	"image"
	"slices"

	"github.com/lrstanley/x/term/bloom/flex"
)

// GridNode lays children out in equal-width columns. Build one with [Grid].
type GridNode struct {
	children         []Child
	cols, gapX, gapY int
}

// Grid fills cols columns row by row; cols below one means one column.
// A short last row keeps the column widths. Each row is as tall as its
// tallest cell; rows share extra height equally, and give up height in
// proportion to their own when short of it.
// A slot child sizes its content horizontally within its cell.
func Grid(cols int, children ...Child) GridNode {
	return GridNode{cols: max(cols, 1), children: children}
}

// Gap sets the horizontal and vertical space between cells.
func (n GridNode) Gap(x, y int) GridNode {
	n.gapX, n.gapY = max(x, 0), max(y, 0)
	return n
}

// View implements [Child], drawing the grid at its natural size.
func (n GridNode) View() string { return Render(n, 0, 0) }

// WrapNode flows children at their natural sizes onto successive lines.
// Build one with [Wrap].
type WrapNode struct {
	children   []Child
	gapX, gapY int
}

// Wrap places children left to right, starting a new line when the next
// child does not fit. Each line is as tall as its tallest child. An overwide
// child gets a line of its own, clipped to the container. Slot sizing rules
// do not apply: every child keeps its natural size.
func Wrap(children ...Child) WrapNode { return WrapNode{children: children} }

// Gap sets the horizontal space between children and vertical space between lines.
func (n WrapNode) Gap(x, y int) WrapNode {
	n.gapX, n.gapY = max(x, 0), max(y, 0)
	return n
}

// View implements [Child], drawing the flow at its natural size.
func (n WrapNode) View() string { return Render(n, 0, 0) }

// flow measures or arranges using the same cached natural sizes. Height is
// unbounded so an Auto slot in a Column gets every wrapped line.
func (e *engine) flow(i int, n WrapNode, width int, origin image.Point, arrange bool) Size {
	x, y, lineH, widest := 0, 0, 0, 0
	first := true
	for _, k := range e.kidsOf(i) {
		s := e.measure(k, Size{W: unbounded, H: unbounded})
		if !first && (x+n.gapX+s.W > width) {
			y += lineH + n.gapY
			x, lineH, first = 0, 0, true
		}
		if !first {
			x += n.gapX
		}
		if arrange {
			at := origin.Add(image.Pt(x, y))
			e.arrange(k, image.Rectangle{Min: at, Max: at.Add(s.point())})
		}
		x += s.W
		lineH, widest = max(lineH, s.H), max(widest, x)
		first = false
	}
	return Size{W: widest, H: y + lineH}
}

// gridSpan distributes indivisible cells to the first tracks.
func gridSpan(total, count, gap, index int) (offset, length int) {
	available := max(total-gap*(count-1), 0)
	base, extra := available/count, available%count
	return index*(base+gap) + min(index, extra), base + min(max(extra-index, 0), 1)
}

// gridRows appends a track to e.tracks for each row of grid i when it is
// width wide: its tallest cell, growing by an equal share of extra height
// and shrinking in proportion to its height.
func (e *engine) gridRows(i int, n GridNode, width int) {
	kids := e.kidsOf(i)
	for row := 0; row*n.cols < len(kids); row++ {
		h := 0
		for c, k := range kids[row*n.cols : min((row+1)*n.cols, len(kids))] {
			_, w := gridSpan(width, n.cols, n.gapX, c)
			h = max(h, e.measure(k, Size{W: w, H: unbounded}).H)
		}
		e.tracks = append(e.tracks, flex.Track{Basis: h, Grow: 1, Shrink: 1})
	}
}

func (e *engine) measureGrid(i int, n GridNode, avail Size) Size {
	n.cols = max(n.cols, 1)
	kids := e.kidsOf(i)
	if len(kids) == 0 {
		return Size{}
	}
	width := avail.W
	if width >= unbounded {
		cellW := 0
		for _, k := range kids {
			cellW = max(cellW, e.measure(k, Size{W: unbounded, H: unbounded}).W)
		}
		width = cellW*n.cols + n.gapX*(n.cols-1)
	}
	at := len(e.tracks)
	e.gridRows(i, n, width)
	height := n.gapY * (len(e.tracks) - at - 1)
	for _, t := range e.tracks[at:] {
		height += t.Basis
	}
	e.tracks = e.tracks[:at]
	return Size{W: width, H: height}
}

func (e *engine) arrangeGrid(i int, n GridNode, r image.Rectangle) {
	n.cols = max(n.cols, 1)
	at := len(e.tracks)
	e.gridRows(i, n, r.Dx())
	rows := e.tracks[at:]
	mark := len(e.ints)
	e.ints = slices.Grow(e.ints, 2*len(rows))[:mark+2*len(rows)]
	heights := flex.Split(e.ints[mark:mark], r.Dy(), n.gapY, rows)
	tops := flex.Place(e.ints[mark+len(rows):mark+len(rows)], heights, r.Dy(), n.gapY, flex.Start)
	e.tracks = e.tracks[:at]
	for j, k := range e.kidsOf(i) {
		x, w := gridSpan(r.Dx(), n.cols, n.gapX, j%n.cols)
		at := r.Min.Add(image.Pt(x, tops[j/n.cols]))
		e.arrange(k, image.Rectangle{Min: at, Max: at.Add(image.Pt(w, heights[j/n.cols]))})
	}
	e.ints = e.ints[:mark]
}
