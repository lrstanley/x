// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package raster

import "image"

func isBrailleRune(r rune) bool {
	return r >= 0x2800 && r <= 0x28FF
}

// brailleDots maps each pattern bit (dot 1..8) to its column and row.
var brailleDots = [8][2]int{
	{0, 0}, {0, 1}, {0, 2}, // Dots 1-3, left column.
	{1, 0}, {1, 1}, {1, 2}, // Dots 4-6, right column.
	{0, 3}, {1, 3}, // Dots 7-8, bottom row.
}

// brailleGrid returns the square dot size and the dot column and row offsets
// for a w×h cell. Integer-only, so every cell of a size gets the same dots and
// neighboring cells form one even grid.
//
// Ported from Ghostty's src/font/sprite/draw/braille.zig (MIT License,
// Copyright (c) 2024 Mitchell Hashimoto, Ghostty contributors).
func brailleGrid(w, h int) (dot int, xs [2]int, ys [4]int) {
	dot = min(w/4, h/8)
	xSpacing, ySpacing := w/4, h/8
	xMargin, yMargin := xSpacing/2, ySpacing/2
	xLeft := w - 2*xMargin - xSpacing - 2*dot
	yLeft := h - 2*yMargin - 3*ySpacing - 4*dot

	// Spend leftover pixels, in order: a non-zero dot, non-zero margins, wider
	// spacing, wider margins, and finally a bigger dot.
	if xLeft >= 2 && yLeft >= 4 && dot == 0 {
		dot++
		xLeft -= 2
		yLeft -= 4
	}
	if xLeft >= 2 && xMargin == 0 {
		xMargin = 1
		xLeft -= 2
	}
	if yLeft >= 2 && yMargin == 0 {
		yMargin = 1
		yLeft -= 2
	}
	if xLeft >= 1 {
		xSpacing++
		xLeft--
	}
	if yLeft >= 3 {
		ySpacing++
		yLeft -= 3
	}
	if xLeft >= 2 {
		xMargin++
		xLeft -= 2
	}
	if yLeft >= 2 {
		yMargin++
		yLeft -= 2
	}
	if xLeft >= 2 && yLeft >= 4 {
		dot++
	}

	xs = [2]int{xMargin, xMargin + dot + xSpacing}
	for i := range ys {
		ys[i] = yMargin + i*(dot+ySpacing)
	}
	return dot, xs, ys
}

// drawBraille fills a, which spans one cell, with the dots of braille pattern
// r. Bit n of the pattern is dot n+1.
func drawBraille(a *image.Alpha, r rune) {
	dot, xs, ys := brailleGrid(a.Rect.Dx(), a.Rect.Dy())
	bits := r - 0x2800
	for i, pos := range brailleDots {
		if bits&(1<<i) != 0 {
			x, y := xs[pos[0]], ys[pos[1]]
			fillAlpha(a, x, y, x+dot, y+dot, 0xff)
		}
	}
}
