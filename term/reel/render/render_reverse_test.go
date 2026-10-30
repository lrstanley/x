// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"image/color"
	"slices"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/reel/vt"
)

// TestDrawReverseColors checks that reverse video ([vt.State.ReverseColors])
// swaps the default colors but leaves the ones a cell sets, that SGR 7 swaps
// them back, and that one renderer, drawing the same screen again, repaints
// when only the mode changes.
func TestDrawReverseColors(t *testing.T) {
	t.Parallel()

	fg := color.NRGBA{R: 0xee, G: 0xdd, B: 0xcc, A: 0xff}
	bg := color.NRGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff}
	red := color.NRGBA{R: 0xaa, A: 0xff}
	d := newRenderer(t, WithPalette(Palette{
		DefaultForeground: fg,
		DefaultBackground: bg,
		Indexed:           map[int]color.Color{1: red},
	}))
	scr := vt.NewBuffer(3, 1)
	scr.SetCell(0, 0, &vt.Cell{Content: " ", Width: 1})
	scr.SetCell(1, 0, &vt.Cell{Content: " ", Width: 1, Style: vt.Style{Bg: ansi.BasicColor(1)}})
	scr.SetCell(2, 0, &vt.Cell{Content: " ", Width: 1, Style: vt.Style{Attrs: vt.AttrReverse}})

	reversed := focused
	reversed.ReverseColors = true

	for _, step := range []struct {
		name string
		st   vt.State
		want []color.NRGBA
	}{
		{"normal", focused, []color.NRGBA{bg, red, fg}},
		{"reversed", reversed, []color.NRGBA{fg, red, bg}},
		{"normal again", focused, []color.NRGBA{bg, red, fg}},
	} {
		if got := rowColors(t, d, scr, step.st); !slices.Equal(got, step.want) {
			t.Errorf("%s: cells = %v, want %v", step.name, got, step.want)
		}
	}
}
