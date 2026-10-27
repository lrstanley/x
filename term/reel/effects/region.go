// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"image"
	"iter"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/reel/vt/screen"
)

// Region selects cells of a terminal frame, for the effects that work on
// part of the screen, such as [Highlight]. Each terminal frame is matched on
// its own, so a region that matches text follows it as it moves.
//
// The package's regions are [Cells], [Row], and [Rows], which select fixed
// cells, [Text], [Regexp], and [Env], which find text on each frame, and
// [Any], which joins others. A region of a caller's own implements Rects.
type Region interface {
	// Rects returns the cells the region covers on the terminal of f, as
	// rectangles of cell coordinates, where (0, 0) is the top left cell.
	// They may overlap, and may reach past the screen, which clips them.
	Rects(f *reel.CellFrame) []image.Rectangle
}

// Cells returns a region of the cells in r, in cell coordinates, where
// (0, 0) is the top left cell. The part of r outside the screen is left
// out.
func Cells(r image.Rectangle) Region { return cellsRegion(r.Canon()) }

// Row returns a region of row y of the screen, counted from 0 at the top.
func Row(y int) Region { return Rows(y, y+1) }

// Rows returns a region of the rows from y0 up to but not including y1,
// counted from 0 at the top, so Rows(y, y+1) is [Row](y).
func Rows(y0, y1 int) Region {
	if y1 < y0 {
		y0, y1 = y1, y0
	}
	return rowsRegion{y0, y1}
}

// Text returns a region of the cells that show s, wherever it appears on a
// frame, every time it appears. Text is searched line by line, where a line
// is a row and the rows it soft-wrapped over, as a terminal wraps text
// written past the last column ([screen.Buffer.Wrapped]), so s matches
// across the end of a row it wrapped past, but never across a line break,
// and an s that holds a newline matches nothing. Text a program lays out
// over rows itself, moving the cursor or breaking lines, as full-screen
// programs do, is on separate lines. Blank cells read as spaces. An empty s
// matches nothing. See [Regexp] for how a match maps to cells.
func Text(s string) Region { return textRegion(s) }

// Regexp returns a region of the cells re matches, wherever and as often as
// it matches on a frame. re is matched against the text of each line on its
// own, as [Text] reads lines, so a match never spans a line break, and ^ and
// $ match at the ends of a line. Empty matches select nothing.
//
// A match covers every cell it holds any byte of, so a match of part of a
// character, or of one rune of a grapheme cluster such as a letter with an
// accent, covers its whole cell, and a match of a wide character, such as
// a CJK character or an emoji, covers both of its columns. A match that runs
// over a wrapped row covers its cells on each row.
func Regexp(re *regexp.Regexp) Region { return regexpRegion{re} }

// Env returns a region of the cells that show the values of the
// environment variables names, as they are when the region is matched,
// wherever they appear, as [Text] matches them, across wrapped rows too. A
// value with several lines matches each of them on its own. A variable that
// is unset, or a value or line that is only whitespace, matches nothing, so
// an empty variable never selects every cell.
func Env(names ...string) Region { return envRegion(slices.Clone(names)) }

// Any returns a region of the cells any of regions covers. Nil regions
// are left out.
func Any(regions ...Region) Region {
	return anyRegion(slices.DeleteFunc(slices.Clone(regions), func(r Region) bool { return r == nil }))
}

type cellsRegion image.Rectangle

func (r cellsRegion) Rects(*reel.CellFrame) []image.Rectangle {
	return []image.Rectangle{image.Rectangle(r)}
}

type rowsRegion struct{ y0, y1 int }

func (r rowsRegion) Rects(f *reel.CellFrame) []image.Rectangle {
	return []image.Rectangle{image.Rect(0, r.y0, f.Screen.Bounds().Dx(), r.y1)}
}

type textRegion string

func (r textRegion) Rects(f *reel.CellFrame) []image.Rectangle {
	return findText(f, []string{string(r)})
}

type regexpRegion struct{ re *regexp.Regexp }

func (r regexpRegion) Rects(f *reel.CellFrame) []image.Rectangle {
	if r.re == nil {
		return nil
	}
	var out []image.Rectangle
	for line := range screenLines(f) {
		for _, m := range r.re.FindAllStringIndex(line.text, -1) {
			out = line.appendMatch(out, m[0], m[1])
		}
	}
	return out
}

type envRegion []string

func (r envRegion) Rects(f *reel.CellFrame) []image.Rectangle {
	return findText(f, r.values())
}

// values returns the lines of the variables' values that are not only
// whitespace.
func (r envRegion) values() []string {
	var out []string
	for _, name := range r {
		for line := range strings.Lines(os.Getenv(name)) {
			line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
			if strings.TrimSpace(line) != "" {
				out = append(out, line)
			}
		}
	}
	return out
}

type anyRegion []Region

func (r anyRegion) Rects(f *reel.CellFrame) []image.Rectangle {
	var out []image.Rectangle
	for _, region := range r {
		out = append(out, region.Rects(f)...)
	}
	return out
}

// findText returns the cells of every match of each of needles in the lines
// of f. Empty needles and needles with a newline match nothing.
func findText(f *reel.CellFrame, needles []string) []image.Rectangle {
	needles = textNeedles(needles)
	if len(needles) == 0 {
		return nil
	}
	var out []image.Rectangle
	for line := range screenLines(f) {
		for _, s := range needles {
			for i := 0; ; {
				j := strings.Index(line.text[i:], s)
				if j < 0 {
					break
				}
				out = line.appendMatch(out, i+j, i+j+len(s))
				i += j + len(s)
			}
		}
	}
	return out
}

// textNeedles returns needles without those that match nothing: empty
// ones, and those with a newline.
func textNeedles(needles []string) []string {
	return slices.DeleteFunc(needles, func(s string) bool {
		return s == "" || strings.ContainsAny(s, "\r\n")
	})
}

// screenLine is the text of one line of a screen: a row, and the rows below
// it that it soft-wrapped over, with where each cell's text starts in it
// and the cell's place.
type screenLine struct {
	text string
	// starts is the offset into text of each cell's text, in order, and
	// cells is where each cell is: its first column and the column past
	// its last, on its row.
	starts []int
	cells  []lineCell
}

type lineCell struct{ x0, x1, y int }

// screenLines returns an iterator over the lines of f's screen, top to
// bottom. A row continues the line of the row above when that row is
// soft-wrapped ([screen.Buffer.Wrapped]). A cell is its grapheme cluster, or
// a space when blank, and spacer columns add nothing: those a wide cell
// covers, and the one left at the end of a row when a wide character
// wrapped to the next.
//
// The line yielded is only valid until the next: its slices are reused.
func screenLines(f *reel.CellFrame) iter.Seq[screenLine] {
	return func(yield func(screenLine) bool) {
		b := f.Screen.Bounds()
		w := b.Dx()
		line := screenLine{starts: make([]int, 0, w), cells: make([]lineCell, 0, w)}
		text := make([]byte, 0, w)
		for y := range b.Dy() {
			for x := range w {
				c := f.Screen.CellAt(b.Min.X+x, b.Min.Y+y)
				if c == nil || c.Width == 0 {
					continue
				}
				line.starts = append(line.starts, len(text))
				line.cells = append(line.cells, lineCell{x, min(x+max(c.Width, 1), w), y})
				if c.Content == "" {
					text = append(text, ' ')
				} else {
					text = append(text, c.Content...)
				}
			}
			if f.Screen.Wrapped(b.Min.Y+y) && y < b.Dy()-1 {
				continue
			}
			line.text = string(text)
			if !yield(line) {
				return
			}
			line.starts, line.cells, text = line.starts[:0], line.cells[:0], text[:0]
		}
	}
}

// offsetAt returns the offset into the line's text of the cell at column x
// of row y, or -1 when the line does not hold it.
func (l screenLine) offsetAt(x, y int) int {
	for i, c := range l.cells {
		if c.y == y && x >= c.x0 && x < c.x1 {
			return l.starts[i]
		}
	}
	return -1
}

// cellOf returns the index of the cell byte i of the line's text is in.
func (l screenLine) cellOf(i int) int {
	c, found := slices.BinarySearch(l.starts, i)
	if !found {
		c--
	}
	return c
}

// appendMatch appends the cells of the bytes from i up to j of the line's
// text to rects, a rectangle for each row they are on. An empty match
// appends nothing.
func (l screenLine) appendMatch(rects []image.Rectangle, i, j int) []image.Rectangle {
	if j <= i {
		return rects
	}
	cells := l.cells[l.cellOf(i) : l.cellOf(j-1)+1]
	for len(cells) > 0 {
		first := cells[0]
		n := 1
		for n < len(cells) && cells[n].y == first.y {
			n++
		}
		rects = append(rects, image.Rect(first.x0, first.y, cells[n-1].x1, first.y+1))
		cells = cells[n:]
	}
	return rects
}

// regionCells returns the cells region covers on f, clipped to the screen,
// and widened to the whole of each wide character it covers part of, as
// rectangles: one for each run of cells on a row, or for runs on rows below
// one another that start and end on the same columns.
func regionCells(region Region, f *reel.CellFrame) []image.Rectangle {
	covered := coveredCells(f, region.Rects(f))
	if covered == nil {
		return nil
	}
	b := f.Screen.Bounds()
	return cellRuns(covered, b.Dx(), b.Dy())
}

// coveredCells returns which cells of f rects cover, clipped to the screen,
// and widened to the whole of each wide character they cover part of, as a
// grid of the screen's size, row after row, or nil when they cover none.
func coveredCells(f *reel.CellFrame, rects []image.Rectangle) []bool {
	b := f.Screen.Bounds()
	w, h := b.Dx(), b.Dy()
	grid := image.Rect(0, 0, w, h)
	var covered []bool
	for _, r := range rects {
		r = r.Canon().Intersect(grid)
		if r.Empty() {
			continue
		}
		if covered == nil {
			covered = make([]bool, w*h)
		}
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				covered[y*w+x] = true
			}
		}
	}
	if covered != nil {
		widen(f, covered)
	}
	return covered
}

// widen covers the whole of each wide character covered has part of: the
// spacer columns of a wide cell, and the wide cell of a spacer.
func widen(f *reel.CellFrame, covered []bool) {
	b := f.Screen.Bounds()
	w := b.Dx()
	cell := func(x, y int) *screen.Cell { return f.Screen.CellAt(b.Min.X+x, b.Min.Y+y) }
	for i, on := range covered {
		if !on {
			continue
		}
		x, y := i%w, i/w
		c := cell(x, y)
		if c == nil {
			continue
		}
		if c.Width > 1 {
			for k := 1; k < c.Width && x+k < w; k++ {
				covered[i+k] = true
			}
			continue
		}
		// A spacer belongs to the wide cell before it, past any other
		// spacers.
		for hx := x - 1; c.Width == 0 && hx >= 0; hx-- {
			hc := cell(hx, y)
			if hc == nil || hc.Width == 1 {
				break
			}
			covered[y*w+hx] = true
			if hc.Width > 1 {
				break
			}
		}
	}
}

// cellRuns returns covered, a w by h grid, as rectangles: a run of covered
// cells on a row, grown down over the rows below that have the same run,
// in the order of their top left cells.
func cellRuns(covered []bool, w, h int) []image.Rectangle {
	var out []image.Rectangle
	// open maps the runs of the row above, as their first and past-last
	// columns, to the rectangles that end on it.
	open := map[[2]int]int{}
	for y := range h {
		next := make(map[[2]int]int, len(open))
		for x := 0; x < w; {
			if !covered[y*w+x] {
				x++
				continue
			}
			x0 := x
			for x < w && covered[y*w+x] {
				x++
			}
			run := [2]int{x0, x}
			if i, ok := open[run]; ok {
				out[i].Max.Y = y + 1
				next[run] = i
				continue
			}
			next[run] = len(out)
			out = append(out, image.Rect(x0, y, x, y+1))
		}
		open = next
	}
	return out
}

// regionPixels finds the pixels of a region on terminal frames, for an
// effect that draws on them, with the layout of the recording's rasterizer
// for each frame's size. Frames of one terminal share its screen, which is
// matched once.
type regionPixels struct {
	region Region
	rast   reel.Rasterizer

	layout render.Layout
	size   image.Point
	// screen is the screen rects were found on.
	screen *screen.Buffer
	rects  []image.Rectangle
}

// find returns the pixel rectangles of the cells the region covers on cf.
func (p *regionPixels) find(cf *reel.CellFrame) []image.Rectangle {
	if cf.Screen == p.screen {
		return p.rects
	}
	cells := regionCells(p.region, cf)
	if size := cf.Screen.Bounds().Size(); size != p.size {
		p.layout, p.size = p.rast.Layout(size.X, size.Y), size
	}
	rects := make([]image.Rectangle, 0, len(cells))
	for _, c := range cells {
		r := p.layout.CellRect(c.Min.X, c.Min.Y).Union(p.layout.CellRect(c.Max.X-1, c.Max.Y-1))
		if !r.Empty() {
			rects = append(rects, r)
		}
	}
	p.screen, p.rects = cf.Screen, rects
	return rects
}
