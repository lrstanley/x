// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"image"
	"image/color"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/reel/vt"
)

// TestDrawStatePalette checks that the indexed colors in [vt.State.Palette]
// are drawn over the theme's, and that one renderer, drawing the same screen
// again, repaints when only they change.
func TestDrawStatePalette(t *testing.T) {
	t.Parallel()

	theme := color.NRGBA{R: 0x10, G: 0x20, B: 0x30, A: 0xff}
	d := newRenderer(t, WithPalette(Palette{Indexed: map[int]color.Color{1: theme}}))
	scr := vt.NewBuffer(3, 1)
	scr.SetCell(0, 0, &vt.Cell{Content: " ", Width: 1, Style: vt.Style{Bg: vt.Indexed(1)}})
	scr.SetCell(1, 0, &vt.Cell{Content: " ", Width: 1, Style: vt.Style{Bg: ansi.BasicColor(1)}})
	scr.SetCell(2, 0, &vt.Cell{Content: " ", Width: 1, Style: vt.Style{Bg: ansi.IndexedColor(200)}})

	program := color.NRGBA{R: 0xaa, A: 0xff}
	pink := color.NRGBA{R: 0xff, B: 0xaa, A: 0xff}
	var pal vt.Palette
	pal.Set(1, program)
	pal.Set(200, pink)
	changed := focused
	changed.Palette = &pal

	cells := func(st vt.State) [3]color.NRGBA {
		t.Helper()
		// A clean buffer lets Draw reuse its last frame when it can.
		scr.ClearDirty()
		img := drawAt(t, d, scr, st, 0)
		f := d.layoutSnapshot(t, image.Point{}, scr)
		var out [3]color.NRGBA
		for x := range out {
			c := f.cellBounds(x, 0)
			out[x] = img.NRGBAAt((c.Min.X+c.Max.X)/2, (c.Min.Y+c.Max.Y)/2)
		}
		return out
	}
	stock := color.NRGBAModel.Convert(ansi.IndexedColor(200)).(color.NRGBA)
	for _, step := range []struct {
		name string
		st   vt.State
		want [3]color.NRGBA
	}{
		{"theme", focused, [3]color.NRGBA{theme, theme, stock}},
		{"program", changed, [3]color.NRGBA{program, program, pink}},
		{"reset", focused, [3]color.NRGBA{theme, theme, stock}},
	} {
		if got := cells(step.st); got != step.want {
			t.Errorf("%s: cells = %v, want %v", step.name, got, step.want)
		}
	}
}
