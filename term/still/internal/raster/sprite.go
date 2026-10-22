// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package raster

import (
	"image"
	"sync"
)

// IsSpriteRune reports runes drawn entirely from the cell geometry and never
// from a font face: Block Elements (U+2580..U+259F), Braille Patterns
// (U+2800..U+28FF), block octants (U+1CD00..U+1CDE5), block sextants
// (U+1FB00..U+1FB3B), and the eighth blocks and half-cell shades of Symbols
// for Legacy Computing (U+1FB70..U+1FB94). Drawing them in code keeps them
// aligned across neighboring cells regardless of the font. Box drawing is not
// included: it still starts from the face's glyph.
func IsSpriteRune(r rune) bool {
	if r < 0x2580 {
		return false
	}
	if isBrailleRune(r) {
		return true
	}
	_, ok := blockSpecFromRune(r)
	return ok
}

type spriteMaskKey struct {
	r    rune
	w, h int
}

// spriteMasks caches immutable masks by rune and cell size.
var spriteMasks sync.Map // spriteMaskKey -> *image.Alpha

// buildSpriteGlyphMask returns the cell-geometry mask for sprite rune r
// covering area.
func buildSpriteGlyphMask(area image.Rectangle, r rune) (boxGlyphMask, bool) {
	if area.Empty() {
		return boxGlyphMask{}, false
	}
	w, h := area.Dx(), area.Dy()
	key := spriteMaskKey{r: r, w: w, h: h}
	if v, loaded := spriteMasks.Load(key); loaded {
		cached, _ := v.(*image.Alpha)
		return boxGlyphMask{dr: area, alpha: cached}, true
	}

	a := image.NewAlpha(image.Rect(0, 0, w, h))
	if isBrailleRune(r) {
		drawBraille(a, r)
	} else {
		spec, ok := blockSpecFromRune(r)
		if !ok {
			return boxGlyphMask{}, false
		}
		drawBlockSpec(a, spec)
	}
	v, _ := spriteMasks.LoadOrStore(key, a)
	shared, _ := v.(*image.Alpha)
	return boxGlyphMask{dr: area, alpha: shared}, true
}

// fillAlpha sets the pixels of a within [x0,x1)×[y0,y1) to v.
func fillAlpha(a *image.Alpha, x0, y0, x1, y1 int, v uint8) {
	r := image.Rect(x0, y0, x1, y1).Intersect(a.Rect)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		row := a.Pix[y*a.Stride:]
		for x := r.Min.X; x < r.Max.X; x++ {
			row[x] = v
		}
	}
}
