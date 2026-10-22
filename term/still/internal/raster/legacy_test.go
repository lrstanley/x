// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package raster

import (
	"bytes"
	"image"
	"testing"

	"github.com/lrstanley/x/term/still/internal/alpha"
)

// legacySizes are odd cell sizes, where rounding of the splits matters.
var legacySizes = [][2]int{{7, 15}, {9, 19}, {11, 23}, {13, 27}, {15, 33}}

// sextantRune and octantRune return the rune for a row-major cell pattern.
func sextantRune(p uint8) rune {
	r := 0x1FB00 + rune(p) - 1
	if p > 21 {
		r--
	}
	if p > 42 {
		r--
	}
	return r
}

func octantRune(p uint8) (rune, bool) {
	for i, q := range octantPatterns {
		if q == p {
			return 0x1CD00 + rune(i), true
		}
	}
	return 0, false
}

// solidRect builds a w×h mask with rect filled at alpha.
func solidRect(w, h int, rect image.Rectangle, a uint8) *image.Alpha {
	m := image.NewAlpha(image.Rect(0, 0, w, h))
	fillAlpha(m, rect.Min.X, rect.Min.Y, rect.Max.X, rect.Max.Y, a)
	return m
}

func sameMask(a, b *image.Alpha) bool {
	return a.Rect == b.Rect && bytes.Equal(a.Pix, b.Pix)
}

func inkRect(t *testing.T, r rune, w, h int) image.Rectangle {
	t.Helper()
	ink, ok := alpha.InkBounds(spriteMask(t, r, w, h))
	if !ok {
		t.Fatalf("U+%04X has no ink at %dx%d", r, w, h)
	}
	return ink
}

// assertTiles checks that the masks of a and b cover every pixel exactly once.
func assertTiles(t *testing.T, a, b rune, w, h int) {
	t.Helper()
	ma, mb := spriteMask(t, a, w, h), spriteMask(t, b, w, h)
	for i := range ma.Pix {
		if (ma.Pix[i] != 0) == (mb.Pix[i] != 0) {
			t.Errorf("%dx%d: U+%04X and U+%04X overlap or leave a gap at pixel %d", w, h, a, b, i)
			return
		}
	}
}

func TestSextantFormula(t *testing.T) {
	t.Parallel()

	// The names run BLOCK SEXTANT-1 (U+1FB00) .. BLOCK SEXTANT-23456 (U+1FB3B).
	cases := map[rune]uint8{
		0x1FB00: 0b000001, // 1
		0x1FB01: 0b000010, // 2
		0x1FB13: 0b010100, // 35
		0x1FB14: 0b010110, // 235
		0x1FB27: 0b101001, // 146
		0x1FB28: 0b101011, // 1246
		0x1FB3B: 0b111110, // 23456
	}
	for r, want := range cases {
		spec, ok := blockSpecFromRune(r)
		if !ok || spec.rows != 3 || spec.cells != want {
			t.Errorf("U+%04X: spec %+v, want sextant %06b", r, spec, want)
		}
		if got := sextantRune(want); got != r {
			t.Errorf("sextantRune(%06b) = U+%04X, want U+%04X", want, got, r)
		}
	}
}

func TestSextantAndOctantRects(t *testing.T) {
	t.Parallel()

	for _, sz := range legacySizes {
		w, h := sz[0], sz[1]
		mx := blockEdge(w, 4)
		t1, t2 := blockRowEdge(h, 3, 1), blockRowEdge(h, 3, 2)
		e2, e4, e6 := blockEdge(h, 2), blockEdge(h, 4), blockEdge(h, 6)

		cases := []struct {
			name string
			r    rune
			want image.Rectangle
		}{
			{"sextant-1", 0x1FB00, image.Rect(0, 0, mx, t1)},
			{"sextant-4", 0x1FB07, image.Rect(mx, t1, w, t2)},
			{"sextant-56", 0x1FB2D, image.Rect(0, t2, w, h)},
			{"sextant-12", 0x1FB02, image.Rect(0, 0, w, t1)},
			{"octant-3", 0x1CD00, image.Rect(0, e2, mx, e4)},
			{"octant-4", 0x1CD03, image.Rect(mx, e2, w, e4)},
			{"octant-34", 0x1CD06, image.Rect(0, e2, w, e4)},
			{"octant-5", 0x1CD09, image.Rect(0, e4, mx, e6)},
			{"octant-56", 0x1CD27, image.Rect(0, e4, w, e6)},
		}
		for _, tc := range cases {
			if got := spriteMask(t, tc.r, w, h); !sameMask(got, solidRect(w, h, tc.want, 0xff)) {
				ink, _ := alpha.InkBounds(got)
				t.Errorf("%dx%d: %s (U+%04X) ink %v, want exactly %v", w, h, tc.name, tc.r, ink, tc.want)
			}
		}

		// octant-18: opposite corners, top-left and bottom-right.
		want := solidRect(w, h, image.Rect(0, 0, mx, e2), 0xff)
		fillAlpha(want, mx, e6, w, h, 0xff)
		if !sameMask(spriteMask(t, 0x1CD71, w, h), want) {
			t.Errorf("%dx%d: octant-18 does not match", w, h)
		}
	}
}

func TestSextantsAndOctantsTileWithBlocks(t *testing.T) {
	t.Parallel()

	for _, sz := range legacySizes {
		w, h := sz[0], sz[1]

		// Complementary patterns fill the cell exactly once.
		for p := uint8(1); p < 63; p++ {
			if q := 63 ^ p; p != 21 && p != 42 && q != 21 && q != 42 {
				assertTiles(t, sextantRune(p), sextantRune(q), w, h)
			}
		}
		for i, p := range octantPatterns {
			if q, ok := octantRune(^p); ok {
				assertTiles(t, 0x1CD00+rune(i), q, w, h)
			}
		}

		// Columns line up with ▌ and quadrants; rows with ▀▄▂▆.
		col := inkRect(t, '▌', w, h).Max.X
		if got := inkRect(t, '▘', w, h).Max.X; got != col {
			t.Fatalf("%dx%d: ▘ column %d != ▌ column %d", w, h, got, col)
		}
		for _, r := range []rune{0x1FB00, 0x1FB03, 0x1CD00, 0x1CD09} { // sextant-1, -3, octant-3, -5
			if got := inkRect(t, r, w, h).Max.X; got != col {
				t.Errorf("%dx%d: U+%04X column %d != ▌ column %d", w, h, r, got, col)
			}
		}
		e2, e4, e6 := inkRect(t, '▆', w, h).Min.Y, inkRect(t, '▄', w, h).Min.Y, inkRect(t, '▂', w, h).Min.Y
		if got := inkRect(t, '▀', w, h).Max.Y; got != e4 {
			t.Fatalf("%dx%d: ▀ bottom %d != ▄ top %d", w, h, got, e4)
		}
		rows := []struct {
			r      rune
			y0, y1 int
		}{
			{0x1CD00, e2, e4}, // octant-3
			{0x1CD06, e2, e4}, // octant-34
			{0x1CD09, e4, e6}, // octant-5
			{0x1CD27, e4, e6}, // octant-56
			{0x1CD70, 0, h},   // octant-1234567
		}
		for _, row := range rows {
			if ink := inkRect(t, row.r, w, h); ink.Min.Y != row.y0 || ink.Max.Y != row.y1 {
				t.Errorf("%dx%d: U+%04X rows %d..%d, want %d..%d", w, h, row.r, ink.Min.Y, ink.Max.Y, row.y0, row.y1)
			}
		}

		// Sextant rows are thirds that differ by at most one pixel.
		r0, r1, r2 := inkRect(t, 0x1FB02, w, h), inkRect(t, 0x1FB0B, w, h), inkRect(t, 0x1FB2D, w, h) // 12, 34, 56
		if r0.Max.Y != r1.Min.Y || r1.Max.Y != r2.Min.Y || r0.Min.Y != 0 || r2.Max.Y != h {
			t.Fatalf("%dx%d: sextant rows %v %v %v do not tile", w, h, r0, r1, r2)
		}
		hs := []int{r0.Dy(), r1.Dy(), r2.Dy()}
		if max(hs[0], hs[1], hs[2])-min(hs[0], hs[1], hs[2]) > 1 {
			t.Errorf("%dx%d: sextant row heights %v are uneven", w, h, hs)
		}
	}
}

func TestOctantTable(t *testing.T) {
	t.Parallel()

	if len(octantPatterns) != 230 {
		t.Fatalf("octant table has %d entries, want 230", len(octantPatterns))
	}
	// Spot checks against the character names.
	named := map[rune]uint8{
		0x1CD00: 0b00000100, // OCTANT-3
		0x1CD0F: 0b00010111, // OCTANT-1235
		0x1CD27: 0b00110000, // OCTANT-56
		0x1CD70: 0b01111111, // OCTANT-1234567
		0x1CD71: 0b10000001, // OCTANT-18
		0x1CDE5: 0b11111110, // OCTANT-2345678
	}
	for r, want := range named {
		if got := octantPatterns[r-0x1CD00]; got != want {
			t.Errorf("U+%04X pattern %08b, want %08b", r, got, want)
		}
	}
	// Patterns encoded as other characters: blank, █, half blocks, quadrant
	// combinations, and the upper/lower quarter and three-quarter rows.
	existing := map[uint8]string{0x00: "space", 0xFF: "█", 0x03: "🮂", 0xC0: "▂", 0x3F: "🮅", 0xFC: "▆"}
	for q := range 16 {
		var p uint8
		for i, bits := range [4]uint8{0x05, 0x0A, 0x50, 0xA0} { // UL, UR, LL, LR
			if q&(1<<i) != 0 {
				p |= bits
			}
		}
		existing[p] = "quadrants"
	}
	seen := map[uint8]rune{}
	for i, p := range octantPatterns {
		r := 0x1CD00 + rune(i)
		if prev, dup := seen[p]; dup {
			t.Errorf("U+%04X repeats pattern %08b of U+%04X", r, p, prev)
		}
		seen[p] = r
		if name, ok := existing[p]; ok {
			t.Errorf("U+%04X pattern %08b is already %s", r, p, name)
		}
	}
	if len(existing) != 20 {
		t.Fatalf("expected 20 pre-existing patterns, got %d", len(existing))
	}
}

func TestEighthsAndShadeHalves(t *testing.T) {
	t.Parallel()

	for _, sz := range legacySizes {
		w, h := sz[0], sz[1]
		ex := func(e int) int { return blockEdge(w, e) }
		ey := func(e int) int { return blockEdge(h, e) }

		solids := []struct {
			name string
			r    rune
			want image.Rectangle
		}{
			{"vertical eighth-2", 0x1FB70, image.Rect(ex(1), 0, ex(2), h)},
			{"vertical eighth-7", 0x1FB75, image.Rect(ex(6), 0, ex(7), h)},
			{"horizontal eighth-2", 0x1FB76, image.Rect(0, ey(1), w, ey(2))},
			{"horizontal eighth-7", 0x1FB7B, image.Rect(0, ey(6), w, ey(7))},
			{"upper quarter", 0x1FB82, image.Rect(0, 0, w, ey(2))},
			{"upper seven eighths", 0x1FB86, image.Rect(0, 0, w, ey(7))},
			{"right quarter", 0x1FB87, image.Rect(ex(6), 0, w, h)},
			{"right seven eighths", 0x1FB8B, image.Rect(ex(1), 0, w, h)},
		}
		for _, tc := range solids {
			if got := spriteMask(t, tc.r, w, h); !sameMask(got, solidRect(w, h, tc.want, 0xff)) {
				ink, _ := alpha.InkBounds(got)
				t.Errorf("%dx%d: %s ink %v, want exactly %v", w, h, tc.name, ink, tc.want)
			}
		}

		// Multi-rect shapes: the union of their edge strips.
		multi := []struct {
			name  string
			r     rune
			rects []image.Rectangle
		}{
			{"left and lower", 0x1FB7C, []image.Rectangle{image.Rect(0, 0, ex(1), h), image.Rect(0, ey(7), w, h)}},
			{"right and upper", 0x1FB7E, []image.Rectangle{image.Rect(ex(7), 0, w, h), image.Rect(0, 0, w, ey(1))}},
			{"upper and lower", 0x1FB80, []image.Rectangle{image.Rect(0, 0, w, ey(1)), image.Rect(0, ey(7), w, h)}},
			{"1358", 0x1FB81, []image.Rectangle{
				image.Rect(0, 0, w, ey(1)), image.Rect(0, ey(2), w, ey(3)),
				image.Rect(0, ey(4), w, ey(5)), image.Rect(0, ey(7), w, h),
			}},
		}
		for _, tc := range multi {
			want := image.NewAlpha(image.Rect(0, 0, w, h))
			for _, rect := range tc.rects {
				fillAlpha(want, rect.Min.X, rect.Min.Y, rect.Max.X, rect.Max.Y, 0xff)
			}
			if !sameMask(spriteMask(t, tc.r, w, h), want) {
				t.Errorf("%dx%d: %s does not match its eighth strips", w, h, tc.name)
			}
		}

		// Partial blocks tile with their Block Elements complements.
		assertTiles(t, 0x1FB82, '▆', w, h) // upper 1/4 + lower 3/4
		assertTiles(t, 0x1FB85, '▂', w, h) // upper 3/4 + lower 1/4
		assertTiles(t, 0x1FB87, '▊', w, h) // right 1/4 + left 3/4
		assertTiles(t, 0x1FB89, '▍', w, h) // right 5/8 + left 3/8

		// Shade halves are uniform medium alpha; "inverse" shades too.
		mx, my := ex(4), ey(4)
		shades := []struct {
			name     string
			r        rune
			top, bot image.Rectangle // shaded, then solid (may be empty)
		}{
			{"left half medium", 0x1FB8C, image.Rect(0, 0, mx, h), image.Rectangle{}},
			{"lower half medium", 0x1FB8F, image.Rect(0, my, w, h), image.Rectangle{}},
			{"inverse medium", 0x1FB90, image.Rect(0, 0, w, h), image.Rectangle{}},
			{"upper block lower inverse", 0x1FB91, image.Rect(0, my, w, h), image.Rect(0, 0, w, my)},
			{"left inverse right block", 0x1FB94, image.Rect(0, 0, mx, h), image.Rect(mx, 0, w, h)},
		}
		for _, tc := range shades {
			want := solidRect(w, h, tc.top, shadeMedium)
			fillAlpha(want, tc.bot.Min.X, tc.bot.Min.Y, tc.bot.Max.X, tc.bot.Max.Y, 0xff)
			if !sameMask(spriteMask(t, tc.r, w, h), want) {
				t.Errorf("%dx%d: %s does not match", w, h, tc.name)
			}
		}
	}
}
