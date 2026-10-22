// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package raster

import "image"

// Shade alphas for light, medium, and dark shade (U+2591..U+2593): a uniform
// 25%, 50%, and 75% fill rather than a dither pattern.
const (
	shadeLight  uint8 = 64
	shadeMedium uint8 = 128
	shadeDark   uint8 = 191
)

// blockQuad selects quarter rects of the cell. The bits follow the row-major
// grid order of [blockSpec.cells].
type blockQuad uint8

const (
	quadUL blockQuad = 1 << iota
	quadUR
	quadLL
	quadLR
)

// blockRect is a rectangle in eighth-cell edges, in the range 0..8, filled
// with alpha. A zero alpha marks an unused rect.
type blockRect struct {
	x0, y0, x1, y1 int
	alpha          uint8
}

// solid returns an opaque blockRect.
func solid(x0, y0, x1, y1 int) blockRect {
	return blockRect{x0: x0, y0: y0, x1: x1, y1: y1, alpha: 0xff}
}

// blockSpec describes a sprite as up to four eighth-based rects or, when rows
// is non-zero, as a two-column grid (quadrants, sextants, octants) whose
// filled cells are set in row-major order from bit 0 at the top left.
type blockSpec struct {
	rects [4]blockRect
	rows  int
	cells uint8
}

func rectSpec(rects ...blockRect) blockSpec {
	var spec blockSpec
	copy(spec.rects[:], rects)
	return spec
}

func blockSpecFromRune(r rune) (blockSpec, bool) {
	switch {
	case r == 0x2580: // ▀
		return rectSpec(solid(0, 0, 8, 4)), true
	case r >= 0x2581 && r <= 0x2588: // ▁..█
		return rectSpec(solid(0, 8-int(r-0x2580), 8, 8)), true
	case r >= 0x2589 && r <= 0x258F: // ▉..▏
		return rectSpec(solid(0, 0, 8-int(r-0x2588), 8)), true
	case r == 0x2590: // ▐
		return rectSpec(solid(4, 0, 8, 8)), true
	case r == 0x2591:
		return rectSpec(blockRect{x1: 8, y1: 8, alpha: shadeLight}), true
	case r == 0x2592:
		return rectSpec(blockRect{x1: 8, y1: 8, alpha: shadeMedium}), true
	case r == 0x2593:
		return rectSpec(blockRect{x1: 8, y1: 8, alpha: shadeDark}), true
	case r == 0x2594: // ▔
		return rectSpec(solid(0, 0, 8, 1)), true
	case r == 0x2595: // ▕
		return rectSpec(solid(7, 0, 8, 8)), true
	case r >= 0x2596 && r <= 0x259F:
		return blockSpec{rows: 2, cells: uint8(blockQuadrants[r-0x2596])}, true
	}
	return legacySpecFromRune(r)
}

// blockQuadrants is indexed by rune - U+2596 (▖▗▘▙▚▛▜▝▞▟).
var blockQuadrants = [...]blockQuad{
	quadLL,
	quadLR,
	quadUL,
	quadUL | quadLL | quadLR,
	quadUL | quadLR,
	quadUL | quadUR | quadLL,
	quadUL | quadUR | quadLR,
	quadUR,
	quadUR | quadLL,
	quadUR | quadLL | quadLR,
}

// blockEdge maps an eighth (0..8) onto a pixel offset within n pixels, rounding
// half up. Every cell of a given size gets the same split, and complementary
// glyphs (▀/▄, ▌/▐) share it, so neighbors and halves tile without gaps.
func blockEdge(n, eighth int) int {
	return (n*eighth + 4) / 8
}

// blockRowEdge returns the top edge of grid row i (0..rows) within n pixels.
// Two and four rows use the eighth edges, so quadrants and octants line up
// with ▀▄▂▆; three rows use thirds, rounded like blockEdge (an exact half
// cannot occur).
func blockRowEdge(n, rows, i int) int {
	if rows == 3 {
		return (n*i + 1) / 3
	}
	return blockEdge(n, i*8/rows)
}

// drawBlockSpec fills a, which spans one cell, with spec.
func drawBlockSpec(a *image.Alpha, spec blockSpec) {
	w, h := a.Rect.Dx(), a.Rect.Dy()
	if spec.rows == 0 {
		for _, r := range spec.rects {
			if r.alpha != 0 {
				fillAlpha(a, blockEdge(w, r.x0), blockEdge(h, r.y0), blockEdge(w, r.x1), blockEdge(h, r.y1), r.alpha)
			}
		}
		return
	}
	cols := [3]int{0, blockEdge(w, 4), w}
	for i := range 2 * spec.rows {
		if spec.cells&(1<<i) == 0 {
			continue
		}
		row, col := i/2, i%2
		fillAlpha(a, cols[col], blockRowEdge(h, spec.rows, row), cols[col+1], blockRowEdge(h, spec.rows, row+1), 0xff)
	}
}
