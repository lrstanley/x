// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"image"
	"testing"

	"github.com/lrstanley/x/term/reel/vt/screen"
)

// fgRuns returns the [start, end) spans of fg pixels along a line of n pixels.
func fgRuns(n int, isFg func(i int) bool) [][2]int {
	var runs [][2]int
	for i := 0; i < n; i++ {
		if !isFg(i) {
			continue
		}
		start := i
		for i < n && isFg(i) {
			i++
		}
		runs = append(runs, [2]int{start, i})
	}
	return runs
}

// assertEvenDotGrid checks that runs form two dots per cell, and that the gap
// between cells is the same everywhere.
func assertEvenDotGrid(t *testing.T, size int, axis string, runs [][2]int, perCell int) {
	t.Helper()
	cellGaps := map[int]bool{}
	for i := 1; i < len(runs); i++ {
		if i%perCell == 0 {
			cellGaps[runs[i][0]-runs[i-1][1]] = true
		}
	}
	if len(cellGaps) != 1 {
		t.Errorf("size=%d: %s gaps between cells differ: %v", size, axis, cellGaps)
	}
}

func TestBrailleAlignsAcrossCells(t *testing.T) {
	t.Parallel()

	fg, bg, palette := blockTestPalette()
	const cols, rows = 6, 2
	for _, size := range []int{10, 11, 13, 16, 20, 24} {
		d := MustNew(WithScale(scaleFor(size)), palette)
		scr := newTestScreen(cols, rows)
		for y := range rows {
			for x := range cols {
				scr.SetCell(x, y, &screen.Cell{Content: "⣿", Width: 1, Style: screen.Style{Fg: fg, Bg: bg}})
			}
		}
		img := drawNRGBA(t, d, scr)
		snap := d.layoutSnapshot(t, image.Point{}, scr)
		first := snap.cellBounds(0, 0)

		// Every cell has the same dots at the same offsets, fully fg or bg.
		for y := range rows {
			for x := range cols {
				cell := snap.cellBounds(x, y)
				for dy := range cell.Dy() {
					for dx := range cell.Dx() {
						got := img.NRGBAAt(cell.Min.X+dx, cell.Min.Y+dy)
						if want := img.NRGBAAt(first.Min.X+dx, first.Min.Y+dy); got != want {
							t.Fatalf("size=%d: cell (%d,%d) differs from cell (0,0) at +(%d,%d)", size, x, y, dx, dy)
						}
						if got != fg && got != bg {
							t.Fatalf("size=%d: cell (%d,%d) has blended pixel %v", size, x, y, got)
						}
					}
				}
			}
		}

		// Dots across the row and down the two rows form one even grid.
		last := snap.cellBounds(cols-1, rows-1)
		var dotRow, dotCol int
		for y := first.Min.Y; y < first.Max.Y && dotRow == 0; y++ {
			for x := first.Min.X; x < first.Max.X; x++ {
				if img.NRGBAAt(x, y) == fg {
					dotRow, dotCol = y, x
					break
				}
			}
		}
		if dotRow == 0 {
			t.Fatalf("size=%d: ⣿ has no ink", size)
		}
		hRuns := fgRuns(last.Max.X-first.Min.X, func(i int) bool { return img.NRGBAAt(first.Min.X+i, dotRow) == fg })
		vRuns := fgRuns(last.Max.Y-first.Min.Y, func(i int) bool { return img.NRGBAAt(dotCol, first.Min.Y+i) == fg })
		if len(hRuns) != 2*cols || len(vRuns) != 4*rows {
			t.Fatalf("size=%d: got %d dot columns and %d dot rows, want %d and %d", size, len(hRuns), len(vRuns), 2*cols, 4*rows)
		}
		assertEvenDotGrid(t, size, "horizontal", hRuns, 2)
		assertEvenDotGrid(t, size, "vertical", vRuns, 4)
		t.Logf("size=%d cell=%dx%d h=%v v=%v", size, first.Dx(), first.Dy(), hRuns[:4], vRuns[:6])
	}
}

func TestLegacyBlocksTileWithBlockElements(t *testing.T) {
	t.Parallel()

	fg, bg, palette := blockTestPalette()
	for _, size := range []int{11, 13, 17} {
		// A left sextant column and a right octant column next to ▌▐ and
		// quadrants: each pair splits the cell at the same column.
		d := MustNew(WithScale(scaleFor(size)), palette)
		glyphs := []string{"▌", "\U0001FB28", "▐", "\U0001CDE5", "▘", "\U0001FB00"} // ▌ 🬨 ▐ octant-2345678 ▘ 🬀
		scr := newTestScreen(len(glyphs), 1)
		for x, g := range glyphs {
			scr.SetCell(x, 0, &screen.Cell{Content: g, Width: 1, Style: screen.Style{Fg: fg, Bg: bg}})
		}
		img := drawNRGBA(t, d, scr)
		snap := d.layoutSnapshot(t, image.Point{}, scr)

		edges := make([]image.Rectangle, len(glyphs))
		for x := range glyphs {
			cell := snap.cellBounds(x, 0)
			ink, ok := inkBounds(img, cell, bg)
			if !ok {
				t.Fatalf("size=%d: %s has no ink", size, glyphs[x])
			}
			edges[x] = ink.Sub(cell.Min)
		}
		// ▌ and 🬨 (sextant-1246, the right column plus the top-left cell)
		// meet at the same split.
		if edges[0].Max.X != edges[2].Min.X || edges[0].Max.X != edges[4].Max.X || edges[0].Max.X != edges[5].Max.X {
			t.Errorf("size=%d: column splits differ: ▌ %v ▐ %v ▘ %v 🬀 %v", size, edges[0], edges[2], edges[4], edges[5])
		}
		if edges[5].Max.Y >= edges[4].Max.Y {
			t.Errorf("size=%d: 🬀 (a third) should be shorter than ▘ (a half): %v vs %v", size, edges[5], edges[4])
		}
		if edges[1] != (image.Rectangle{Max: snap.cellBounds(1, 0).Size()}) || edges[3] != edges[1] {
			t.Errorf("size=%d: 🬨 %v and octant-2345678 %v should span the cell", size, edges[1], edges[3])
		}
	}
}
