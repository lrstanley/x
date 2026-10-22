// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package raster

import (
	"image"
	"testing"
)

func TestBlockSpecCoversRange(t *testing.T) {
	t.Parallel()

	for r := rune(0x2580); r <= 0x259F; r++ {
		if _, ok := blockSpecFromRune(r); !ok {
			t.Errorf("U+%04X has no block spec", r)
		}
	}
	for _, r := range []rune{0x257F, 0x25A0, 'A'} {
		if _, ok := blockSpecFromRune(r); ok {
			t.Errorf("U+%04X should not have a block spec", r)
		}
	}
}

func TestBuildBlockGlyphMaskEighths(t *testing.T) {
	t.Parallel()

	area := image.Rect(100, 40, 111, 63) // odd size on purpose
	w, h := area.Dx(), area.Dy()

	count := func(r rune) int {
		m, _ := buildSpriteGlyphMask(area, r)
		n := 0
		for _, a := range m.alpha.Pix {
			if a != 0 {
				n++
			}
		}
		return n
	}

	if got := count('█'); got != w*h {
		t.Errorf("█ covers %d, want %d", got, w*h)
	}
	if up, lo := count('▀'), count('▄'); up+lo != w*h {
		t.Errorf("▀+▄ cover %d, want %d", up+lo, w*h)
	}
	if l, r := count('▌'), count('▐'); l+r != w*h {
		t.Errorf("▌+▐ cover %d, want %d", l+r, w*h)
	}
	// ▁ is strictly shorter than ▂, ... up to █; ▏ narrower than ▎, ... up to █.
	for r := rune(0x2581); r < 0x2588; r++ {
		if count(r) >= count(r+1) {
			t.Errorf("U+%04X not smaller than U+%04X", r, r+1)
		}
	}
	for r := rune(0x258F); r > 0x2589; r-- {
		if count(r) >= count(r-1) {
			t.Errorf("U+%04X not smaller than U+%04X", r, r-1)
		}
	}
	// Quadrant pairs that partition the cell.
	if a, b := count('▘'), count('▟'); a+b != w*h {
		t.Errorf("▘+▟ cover %d, want %d", a+b, w*h)
	}
	if a, b := count('▚'), count('▞'); a+b != w*h {
		t.Errorf("▚+▞ cover %d, want %d", a+b, w*h)
	}
}
