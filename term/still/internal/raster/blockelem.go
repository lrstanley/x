// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package raster

import (
	"image"
	"sync"
)

// Shade alphas for light, medium, and dark shade (U+2591..U+2593): a uniform
// 25%, 50%, and 75% fill rather than a dither pattern.
const (
	shadeLight  uint8 = 64
	shadeMedium uint8 = 128
	shadeDark   uint8 = 191
)

// blockQuad selects quarter rects of the cell.
type blockQuad uint8

const (
	quadUL blockQuad = 1 << iota
	quadUR
	quadLL
	quadLR
)

// blockSpec describes a block element as eighth-cell edges, in the range
// 0..8, plus an optional alpha for shades.
type blockSpec struct {
	x0, y0, x1, y1 int
	quads          blockQuad
	alpha          uint8
}

// IsBlockElementRune reports Block Elements (U+2580..U+259F), which are drawn
// as exact pixel rectangles instead of from the font.
func IsBlockElementRune(r rune) bool {
	return r >= 0x2580 && r <= 0x259F
}

func blockSpecFromRune(r rune) (blockSpec, bool) {
	switch {
	case r == 0x2580: // ▀
		return blockSpec{x1: 8, y1: 4, alpha: 0xff}, true
	case r >= 0x2581 && r <= 0x2588: // ▁..█
		return blockSpec{y0: 8 - int(r-0x2580), x1: 8, y1: 8, alpha: 0xff}, true
	case r >= 0x2589 && r <= 0x258F: // ▉..▏
		return blockSpec{x1: 8 - int(r-0x2588), y1: 8, alpha: 0xff}, true
	case r == 0x2590: // ▐
		return blockSpec{x0: 4, x1: 8, y1: 8, alpha: 0xff}, true
	case r == 0x2591:
		return blockSpec{x1: 8, y1: 8, alpha: shadeLight}, true
	case r == 0x2592:
		return blockSpec{x1: 8, y1: 8, alpha: shadeMedium}, true
	case r == 0x2593:
		return blockSpec{x1: 8, y1: 8, alpha: shadeDark}, true
	case r == 0x2594: // ▔
		return blockSpec{x1: 8, y1: 1, alpha: 0xff}, true
	case r == 0x2595: // ▕
		return blockSpec{x0: 7, x1: 8, y1: 8, alpha: 0xff}, true
	case r >= 0x2596 && r <= 0x259F:
		return blockSpec{quads: blockQuadrants[r-0x2596], alpha: 0xff}, true
	}
	return blockSpec{}, false
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

type blockMaskKey struct {
	r    rune
	w, h int
}

// blockMasks caches immutable masks by rune and cell size.
var blockMasks sync.Map // blockMaskKey -> *image.Alpha

// buildBlockGlyphMask returns the exact-rectangle mask for block element r
// covering area.
func buildBlockGlyphMask(area image.Rectangle, r rune) (boxGlyphMask, bool) {
	spec, ok := blockSpecFromRune(r)
	if !ok || area.Empty() {
		return boxGlyphMask{}, false
	}
	w, h := area.Dx(), area.Dy()
	key := blockMaskKey{r: r, w: w, h: h}
	if v, loaded := blockMasks.Load(key); loaded {
		if cached, isAlpha := v.(*image.Alpha); isAlpha {
			return boxGlyphMask{dr: area, alpha: cached}, true
		}
	}

	a := image.NewAlpha(image.Rect(0, 0, w, h))
	fill := func(x0, y0, x1, y1 int) {
		for y := y0; y < y1; y++ {
			row := a.Pix[y*a.Stride : y*a.Stride+w]
			for x := x0; x < x1; x++ {
				row[x] = spec.alpha
			}
		}
	}
	if spec.quads == 0 {
		fill(blockEdge(w, spec.x0), blockEdge(h, spec.y0), blockEdge(w, spec.x1), blockEdge(h, spec.y1))
	} else {
		mx, my := blockEdge(w, 4), blockEdge(h, 4)
		if spec.quads&quadUL != 0 {
			fill(0, 0, mx, my)
		}
		if spec.quads&quadUR != 0 {
			fill(mx, 0, w, my)
		}
		if spec.quads&quadLL != 0 {
			fill(0, my, mx, h)
		}
		if spec.quads&quadLR != 0 {
			fill(mx, my, w, h)
		}
	}
	v, _ := blockMasks.LoadOrStore(key, a)
	if shared, isAlpha := v.(*image.Alpha); isAlpha {
		a = shared
	}
	return boxGlyphMask{dr: area, alpha: a}, true
}
