// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"github.com/lrstanley/x/term/reel/vt"
)

// iconRow is a row of cells with the icon first, followed by text.
func iconRow(icon, rest string, fg, bg color.NRGBA) testScreen {
	scr := newTestScreen(len([]rune(rest))+1, 1)
	style := vt.Style{Fg: fg, Bg: bg}
	scr.SetCell(0, 0, &vt.Cell{Content: icon, Width: 1, Style: style})
	for i, r := range []rune(rest) {
		scr.SetCell(i+1, 0, &vt.Cell{Content: string(r), Width: 1, Style: style})
	}
	return scr
}

// TestIconFollowedBySpaceSpansTwoCells checks that a Nerd Font icon that is too
// wide for a cell grows into a space after it, but not into a letter, without
// being squashed.
func TestIconFollowedBySpaceSpansTwoCells(t *testing.T) {
	t.Parallel()

	fg, bg, palette := blockTestPalette()
	// fa-question-circle and fa-key are both wider than one cell.
	for _, r := range []rune{'\uf059', '\uf084'} {
		icon := string(r)
		d := MustNew(WithScale(scaleFor(20)), palette)
		t.Cleanup(func() { _ = d.Close() })

		letter := iconRow(icon, "x", fg, bg)
		space := iconRow(icon, " ", fg, bg)
		lctx := d.layoutSnapshot(t, image.Point{}, letter)
		cell := lctx.cellBounds(0, 0)

		before, ok := inkBounds(drawNRGBA(t, d, letter), cell, bg)
		if !ok {
			t.Fatalf("%U: no ink", r)
		}
		sctx := d.layoutSnapshot(t, image.Point{}, space)
		both := cell.Union(sctx.cellBounds(1, 0))
		after, ok := inkBounds(drawNRGBA(t, d, space), both, bg)
		if !ok {
			t.Fatalf("%U: no ink with a space after", r)
		}

		if after.Dx() <= before.Dx() || after.Dy() <= before.Dy() {
			t.Errorf("%U: ink %v followed by a space, want larger than %v followed by a letter", r, after, before)
		}
		if got, want := float64(after.Dx())/float64(after.Dy()), float64(before.Dx())/float64(before.Dy()); got < want*0.9 || got > want*1.1 {
			t.Errorf("%U: aspect %.2f followed by a space, want about %.2f", r, got, want)
		}
		if after.Max.X > both.Max.X || after.Min.X < both.Min.X {
			t.Errorf("%U: ink %v outside its two cells %v", r, after, both)
		}
		// Wider than the first cell, so it starts at the cell's left edge
		// instead of being centered across both cells.
		if after.Dx() > cell.Dx() && after.Min.X != cell.Min.X {
			t.Errorf("%U: ink starts at x=%d, want the cell's left edge %d", r, after.Min.X, cell.Min.X)
		}
		mid := cell.Min.Y + cell.Dy()/2
		if c := (after.Min.Y + after.Max.Y) / 2; c < mid-1 || c > mid+1 {
			t.Errorf("%U: ink rows %d..%d, want centered on row %d", r, after.Min.Y, after.Max.Y, mid)
		}
	}
}

// TestIconSpanStaysOneCell checks the cases that keep the one-cell fit.
func TestIconSpanStaysOneCell(t *testing.T) {
	t.Parallel()

	fg, bg, palette := blockTestPalette()
	d := MustNew(WithScale(scaleFor(20)), palette)
	t.Cleanup(func() { _ = d.Close() })

	const icon = "\uf059"
	ink := func(scr testScreen) image.Rectangle {
		t.Helper()
		b, ok := inkBounds(drawNRGBA(t, d, scr), d.layoutSnapshot(t, image.Point{}, scr).cellBounds(0, 0), bg)
		if !ok {
			t.Fatal("no ink")
		}
		return b
	}
	want := ink(iconRow(icon, "", fg, bg)) // The row end.

	if got := ink(iconRow(icon, "x", fg, bg)); got != want {
		t.Errorf("icon before a letter ink = %v, want the row end fit %v", got, want)
	}
	// A run of icons stays aligned: the first icon sits beside another.
	scr := iconRow(icon, icon+" ", fg, bg)
	if got := ink(scr); got != want {
		t.Errorf("icon before an icon ink = %v, want the one-cell fit %v", got, want)
	}
}

// TestIconSpanFollowingCellRepaints checks that changing only the cell after
// an icon repaints the icon, whether drawn from scratch or incrementally.
func TestIconSpanFollowingCellRepaints(t *testing.T) {
	t.Parallel()

	fg, bg, palette := blockTestPalette()
	d, ref := MustNew(WithScale(scaleFor(20)), palette), MustNew(WithScale(scaleFor(20)), palette)
	t.Cleanup(func() { _ = d.Close(); _ = ref.Close() })

	const cols, rows = 6, 3
	buf := vt.NewBuffer(cols, rows)
	style := vt.Style{Fg: fg, Bg: bg}
	buf.SetCell(1, 1, &vt.Cell{Content: "\uf059", Width: 1, Style: style})
	buf.SetCell(2, 1, &vt.Cell{Content: "x", Width: 1, Style: style})
	letter := bytes.Clone(drawNRGBA(t, d, buf).Pix)

	buf.SetCell(2, 1, &vt.Cell{Content: " ", Width: 1, Style: style})
	got := drawNRGBA(t, d, buf)
	if bytes.Equal(letter, got.Pix) {
		t.Fatal("changing only the cell after the icon did not change the image")
	}
	if want := drawFresh(ref, buf, focused, 0); !bytes.Equal(got.Pix, want.Pix) {
		t.Fatal("incremental draw after the following cell changed differs from a full repaint")
	}

	buf.SetCell(2, 1, &vt.Cell{Content: "x", Width: 1, Style: style})
	if !bytes.Equal(letter, drawNRGBA(t, d, buf).Pix) {
		t.Fatal("restoring the following cell did not restore the image")
	}
}

// TestIconSpanTitle checks that the title bar gives an icon followed by a space
// the same extra room as the grid.
func TestIconSpanTitle(t *testing.T) {
	t.Parallel()

	_, _, palette := blockTestPalette()
	d := MustNew(WithScale(scaleFor(20)), WithWindowChrome(true), palette)
	t.Cleanup(func() { _ = d.Close() })
	scr := newTestScreen(30, 3)

	// width is the first run of ink columns in the middle of the bar, which
	// is the icon: what follows it is a gap of at least a cell.
	width := func(title string) int {
		t.Helper()
		img := drawAt(t, d, scr, vt.State{Title: title}, 0)
		bar := d.layoutSnapshot(t, image.Point{}, scr).chromeBounds
		run := 0
		for x := bar.Min.X + bar.Dx()/4; x < bar.Max.X-bar.Dx()/4; x++ {
			ink := false
			for y := bar.Min.Y; y < bar.Max.Y-1 && !ink; y++ {
				ink = img.NRGBAAt(x, y) != img.NRGBAAt(bar.Max.X-3, y)
			}
			switch {
			case ink:
				run++
			case run > 0:
				return run
			}
		}
		return run
	}

	// Next to a letter the icon stays in one cell.
	if one, two := width("\uf059x"), width("\uf059 x"); two <= one {
		t.Fatalf("icon followed by a space is %dpx wide, want wider than %dpx followed by non-empty", two, one)
	}
}
