// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package raster

import (
	"image"
	"testing"
)

// spriteMask returns the cached w×h mask for sprite rune r.
func spriteMask(t *testing.T, r rune, w, h int) *image.Alpha {
	t.Helper()
	m, ok := buildSpriteGlyphMask(image.Rect(0, 0, w, h), r)
	if !ok {
		t.Fatalf("U+%04X has no sprite mask", r)
	}
	return m.alpha
}

func TestIsSpriteRune(t *testing.T) {
	t.Parallel()

	cases := []struct {
		r    rune
		want bool
	}{
		{'A', false},
		{0x2500, false},  // ─ box drawing uses the face.
		{0x257F, false},  // Last box drawing rune.
		{0x2580, true},   // ▀ first block element.
		{0x259F, true},   // ▟ last block element.
		{0x25A0, false},  // ■ geometric shapes.
		{0x27FF, false},  // Before braille.
		{0x2800, true},   // ⠀ blank braille.
		{0x28FF, true},   // ⣿
		{0x2900, false},  // After braille.
		{0x1CCFF, false}, // Before octants.
		{0x1CD00, true},  // First octant.
		{0x1CDE5, true},  // Last octant.
		{0x1CDE6, false}, // After octants.
		{0x1CEA8, false}, // Left half upper one quarter block (supplement).
		{0x1FAFF, false}, // Before sextants.
		{0x1FB00, true},  // 🬀 first sextant.
		{0x1FB3B, true},  // 🬻 last sextant.
		{0x1FB3C, false}, // 🬼 smooth mosaic wedge.
		{0x1FB6F, false}, // Last triangular block.
		{0x1FB70, true},  // 🭰 vertical one eighth block-2.
		{0x1FB8B, true},  // 🮋 right seven eighths.
		{0x1FB90, true},  // 🮐 inverse medium shade.
		{0x1FB93, false}, // Unassigned.
		{0x1FB94, true},  // 🮔
		{0x1FB95, false}, // 🮕 checker board.
		{0x1FBE6, false}, // Middle left one quarter block.
		{0x1FBF0, false}, // 🯰 segmented digit zero.
	}
	for _, tc := range cases {
		if got := IsSpriteRune(tc.r); got != tc.want {
			t.Errorf("IsSpriteRune(U+%04X) = %v, want %v", tc.r, got, tc.want)
		}
	}
}

func TestBuildSpriteGlyphMaskCached(t *testing.T) {
	t.Parallel()

	for _, r := range []rune{'█', '⣿', 0x1FB00, 0x1CD00, 0x1FB81} {
		a, _ := buildSpriteGlyphMask(image.Rect(0, 0, 9, 18), r)
		b, _ := buildSpriteGlyphMask(image.Rect(50, 90, 59, 108), r)
		if a.alpha != b.alpha {
			t.Errorf("U+%04X: same rune and cell size should share one mask", r)
		}
		if c, _ := buildSpriteGlyphMask(image.Rect(0, 0, 10, 18), r); c.alpha == a.alpha {
			t.Errorf("U+%04X: different cell sizes should not share a mask", r)
		}
	}
}
