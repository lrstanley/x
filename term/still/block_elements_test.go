// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image"
	"image/color"
	"testing"

	"github.com/lrstanley/x/term/vt/screen"
)

func blockTestPalette() (fg, bg color.NRGBA, opt Option) {
	fg = color.NRGBA{R: 249, G: 226, B: 175, A: 0xff}
	bg = color.NRGBA{R: 30, G: 30, B: 46, A: 0xff}
	return fg, bg, WithPalette(Palette{DefaultForeground: fg, DefaultBackground: bg})
}

func blockRow(glyph string, n int, fg, bg color.NRGBA) testScreen {
	scr := newTestScreen(n, 1)
	for x := range n {
		scr.SetCell(x, 0, &screen.Cell{Content: glyph, Width: 1, Style: screen.Style{Fg: fg, Bg: bg}})
	}
	return scr
}

func TestBlockElementsFullBlockHasNoSeams(t *testing.T) {
	t.Parallel()

	fg, bg, palette := blockTestPalette()
	for _, size := range []int{10, 12, 14, 16, 20, 24} {
		d := MustNew(WithFontSizePt(Pt(size)), palette)
		scr := blockRow("█", 8, fg, bg)
		img := drawNRGBA(t, d, scr)
		snap := d.layoutSnapshot(t, image.Point{}, scr)
		first, last := snap.cellBounds(0, 0), snap.cellBounds(7, 0)
		row := image.Rect(first.Min.X, first.Min.Y, last.Max.X, first.Max.Y)
		for y := row.Min.Y; y < row.Max.Y; y++ {
			for x := row.Min.X; x < row.Max.X; x++ {
				if got := img.NRGBAAt(x, y); got != fg {
					t.Fatalf("size=%d: pixel (%d,%d) = %v, want fg %v", size, x, y, got, fg)
				}
			}
		}
	}
}

func TestBlockElementsHalfBlocksTile(t *testing.T) {
	t.Parallel()

	fg, bg, palette := blockTestPalette()
	for _, size := range []int{10, 11, 13, 17, 20} {
		// Separate renderers: Draw reuses its image between calls.
		dUp, dLo := MustNew(WithFontSizePt(Pt(size)), palette), MustNew(WithFontSizePt(Pt(size)), palette)
		upper, lower := blockRow("▀", 4, fg, bg), blockRow("▄", 4, fg, bg)
		upImg, loImg := drawNRGBA(t, dUp, upper), drawNRGBA(t, dLo, lower)
		d := dUp
		cell := d.layoutSnapshot(t, image.Point{}, upper).cellBounds(0, 0)

		var upRows, loRows int
		for y := cell.Min.Y; y < cell.Max.Y; y++ {
			// Every cell in the row agrees, and the pixel is fully fg or bg.
			wantUp, wantLo := upImg.NRGBAAt(cell.Min.X, y), loImg.NRGBAAt(cell.Min.X, y)
			for x := cell.Min.X; x < 4*cell.Dx(); x++ {
				if got := upImg.NRGBAAt(x, y); got != wantUp {
					t.Fatalf("size=%d: ▀ row %d differs at x=%d: %v vs %v", size, y, x, got, wantUp)
				}
				if got := loImg.NRGBAAt(x, y); got != wantLo {
					t.Fatalf("size=%d: ▄ row %d differs at x=%d: %v vs %v", size, y, x, got, wantLo)
				}
			}
			if wantUp != fg && wantUp != bg || wantLo != fg && wantLo != bg {
				t.Fatalf("size=%d: row %d has blended pixels: %v %v", size, y, wantUp, wantLo)
			}
			if wantUp == fg {
				upRows++
			}
			if wantLo == fg {
				loRows++
			}
			// ▀ and ▄ are complementary: exactly one of them paints each row.
			if (wantUp == fg) == (wantLo == fg) {
				t.Fatalf("size=%d: row %d of ▀/▄ overlaps or leaves a gap", size, y)
			}
		}
		if upRows+loRows != cell.Dy() {
			t.Fatalf("size=%d: ▀ rows %d + ▄ rows %d != cell height %d", size, upRows, loRows, cell.Dy())
		}
	}
}

func TestBlockElementsComplementaryHalvesTile(t *testing.T) {
	t.Parallel()

	fg, bg, palette := blockTestPalette()
	d := MustNew(WithFontSizePt(13), palette)

	scr := newTestScreen(2, 1)
	scr.SetCell(0, 0, &screen.Cell{Content: "▌", Width: 1, Style: screen.Style{Fg: fg, Bg: bg}})
	scr.SetCell(1, 0, &screen.Cell{Content: "▐", Width: 1, Style: screen.Style{Fg: fg, Bg: bg}})
	img := drawNRGBA(t, d, scr)
	snap := d.layoutSnapshot(t, image.Point{}, scr)
	left, right := snap.cellBounds(0, 0), snap.cellBounds(1, 0)

	for y := left.Min.Y; y < left.Max.Y; y++ {
		// ▌ fills the left of cell 0, ▐ the right of cell 1; the other halves are bg.
		var leftFg, rightFg int
		for x := left.Min.X; x < left.Max.X; x++ {
			if img.NRGBAAt(x, y) == fg {
				leftFg++
			}
		}
		for x := right.Min.X; x < right.Max.X; x++ {
			if img.NRGBAAt(x, y) == fg {
				rightFg++
			}
		}
		if leftFg+rightFg != left.Dx() {
			t.Fatalf("row %d: ▌ painted %d + ▐ painted %d, want total %d", y, leftFg, rightFg, left.Dx())
		}
	}
}

func TestBlockElementsQuadrantsAndShades(t *testing.T) {
	t.Parallel()

	fg, bg, palette := blockTestPalette()

	// Draw reuses its image between calls, so each glyph gets its own renderer.
	cellImg := func(glyph string) (*image.NRGBA, image.Rectangle) {
		d := MustNew(WithFontSizePt(14), palette)
		scr := blockRow(glyph, 1, fg, bg)
		return drawNRGBA(t, d, scr), d.layoutSnapshot(t, image.Point{}, scr).cellBounds(0, 0)
	}

	t.Run("quadrants", func(t *testing.T) {
		t.Parallel()

		cases := map[string][4]bool{ // UL, UR, LL, LR
			"▘": {true, false, false, false},
			"▝": {false, true, false, false},
			"▖": {false, false, true, false},
			"▗": {false, false, false, true},
			"▚": {true, false, false, true},
			"▞": {false, true, true, false},
			"▛": {true, true, true, false},
			"▜": {true, true, false, true},
			"▙": {true, false, true, true},
			"▟": {false, true, true, true},
		}
		for glyph, want := range cases {
			img, cell := cellImg(glyph)
			// Sample well inside each quadrant, away from the split lines.
			pts := [4]image.Point{
				{cell.Min.X + cell.Dx()/4, cell.Min.Y + cell.Dy()/4},
				{cell.Min.X + cell.Dx()*3/4, cell.Min.Y + cell.Dy()/4},
				{cell.Min.X + cell.Dx()/4, cell.Min.Y + cell.Dy()*3/4},
				{cell.Min.X + cell.Dx()*3/4, cell.Min.Y + cell.Dy()*3/4},
			}
			for i, p := range pts {
				if got := img.NRGBAAt(p.X, p.Y) == fg; got != want[i] {
					t.Errorf("%s quadrant %d filled=%v, want %v", glyph, i, got, want[i])
				}
			}
		}
	})

	t.Run("shades", func(t *testing.T) {
		t.Parallel()

		for glyph, a := range map[string]float64{"░": 0.25, "▒": 0.50, "▓": 0.75} {
			img, cell := cellImg(glyph)
			want := img.NRGBAAt(cell.Min.X, cell.Min.Y)
			for y := cell.Min.Y; y < cell.Max.Y; y++ {
				for x := cell.Min.X; x < cell.Max.X; x++ {
					if got := img.NRGBAAt(x, y); got != want {
						t.Fatalf("%s is not uniform: (%d,%d)=%v, want %v", glyph, x, y, got, want)
					}
				}
			}
			ratio := (float64(want.R) - float64(bg.R)) / (float64(fg.R) - float64(bg.R))
			if ratio < a-0.02 || ratio > a+0.02 {
				t.Errorf("%s coverage = %.3f, want ~%.2f", glyph, ratio, a)
			}
		}
	})
}
