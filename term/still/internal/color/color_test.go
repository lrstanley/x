// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package color_test

import (
	"image/color"
	"testing"

	"github.com/charmbracelet/x/ansi"
	icol "github.com/lrstanley/x/term/still/internal/color"
	"github.com/lrstanley/x/term/still/types"
	"github.com/lrstanley/x/term/vt/screen"
)

type testColorSource struct {
	faintFactor            float64
	backgroundOpacity      float64
	backgroundOpacityCells bool
	palette                types.Palette
	state                  types.EmulatorState
	hasState               bool
}

func (t testColorSource) Palette() types.Palette                     { return t.palette }
func (t testColorSource) EmulatorState() (types.EmulatorState, bool) { return t.state, t.hasState }
func (t testColorSource) FaintFactor() float64                       { return t.faintFactor }
func (t testColorSource) BackgroundOpacity() float64                 { return t.backgroundOpacity }
func (t testColorSource) BackgroundOpacityCells() bool               { return t.backgroundOpacityCells }

func testColorSourceDefault() testColorSource {
	return testColorSource{
		faintFactor: 0.5,
		palette: types.Palette{
			DefaultForeground: color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff},
			DefaultBackground: color.NRGBA{A: 0xff},
			Indexed: map[int]color.Color{
				int(ansi.Red): color.NRGBA{R: 0xff, A: 0xff},
			},
		},
	}
}

func TestNRGBA(t *testing.T) {
	t.Parallel()

	if got := icol.NRGBA(nil); got != (color.NRGBA{}) {
		t.Fatalf("NRGBA(nil) = %+v, want zero", got)
	}
	in := color.NRGBA{R: 1, G: 2, B: 3, A: 4}
	if got := icol.NRGBA(in); got != in {
		t.Fatalf("NRGBA(NRGBA) = %+v, want %+v", got, in)
	}
}

func TestApplyAlpha(t *testing.T) {
	t.Parallel()

	if icol.ApplyAlpha(nil, 0.5) != nil {
		t.Fatal("ApplyAlpha(nil) should return nil")
	}
	got := icol.NRGBA(icol.ApplyAlpha(color.NRGBA{A: 200}, 0.5))
	if got.A != 100 {
		t.Fatalf("ApplyAlpha A = %d, want 100", got.A)
	}
}

func TestBlend(t *testing.T) {
	t.Parallel()

	got := icol.NRGBA(icol.Blend(
		color.NRGBA{R: 0, A: 255},
		color.NRGBA{R: 100, A: 255},
		0.5,
	))
	if got.R != 50 {
		t.Fatalf("Blend R = %d, want 50", got.R)
	}
}

func TestBlendOverBg(t *testing.T) {
	t.Parallel()

	fg := color.NRGBA{R: 255, A: 255}
	bg := color.NRGBA{G: 255, A: 255}

	if got := icol.BlendOverBg(fg, bg, 255); got != fg {
		t.Fatalf("full alpha should return fg: %+v", got)
	}
	if got := icol.BlendOverBg(fg, bg, 0); got != bg {
		t.Fatalf("zero alpha should return bg: %+v", got)
	}
}

func TestResolvePaletteColor(t *testing.T) {
	t.Parallel()

	src := testColorSourceDefault()
	mapped := icol.ResolvePaletteColor(src, ansi.Red)
	if got := icol.NRGBA(mapped).R; got != 0xff {
		t.Fatalf("indexed red R = %d, want 255", got)
	}
	fallback := color.NRGBA{B: 7, A: 255}
	if got := icol.ResolvePaletteColor(src, fallback); got != fallback {
		t.Fatalf("unmapped color = %+v, want %+v", got, fallback)
	}
}

func TestResolveCellColorsNRGBA(t *testing.T) {
	t.Parallel()

	src := testColorSourceDefault()
	fg, bg := icol.ResolveCellColorsNRGBA(src, nil)
	if fg != icol.NRGBA(src.palette.DefaultForeground) {
		t.Fatalf("default fg = %+v", fg)
	}
	if bg != icol.NRGBA(src.palette.DefaultBackground) {
		t.Fatalf("default bg = %+v", bg)
	}
}

func TestResolveCursorNRGBA(t *testing.T) {
	t.Parallel()

	cursor := color.NRGBA{R: 1, A: 255}
	src := testColorSource{
		palette: types.Palette{Cursor: cursor},
	}
	if got := icol.ResolveCursorNRGBA(src, color.NRGBA{}); got != icol.NRGBA(cursor) {
		t.Fatalf("cursor = %+v, want %+v", got, cursor)
	}
}

func TestResolveCellColorsNRGBAAttrs(t *testing.T) {
	t.Parallel()

	src := testColorSourceDefault()
	fg, bg := icol.ResolveCellColorsNRGBA(src, nil)
	if fg.R != 0xff || bg.A != 0xff {
		t.Fatalf("defaults fg=%+v bg=%+v", fg, bg)
	}

	cell := &screen.Cell{Style: screen.Style{
		Fg:    color.NRGBA{R: 10, A: 255},
		Bg:    color.NRGBA{G: 20, A: 255},
		Attrs: screen.AttrReverse,
	}}
	fg, bg = icol.ResolveCellColorsNRGBA(src, cell)
	if fg.G != 20 || bg.R != 10 {
		t.Fatalf("reverse swap fg=%+v bg=%+v", fg, bg)
	}

	cell.Style.Attrs = screen.AttrFaint
	fg, bg = icol.ResolveCellColorsNRGBA(src, cell)
	if fg == icol.NRGBA(cell.Style.Fg) {
		t.Fatal("faint should blend fg toward bg")
	}

	cell.Style.Attrs = screen.AttrConceal
	fg, _ = icol.ResolveCellColorsNRGBA(src, cell)
	if fg != bg {
		t.Fatalf("conceal fg = %+v, want bg %+v", fg, bg)
	}
}

func TestCellHasExplicitBackground(t *testing.T) {
	t.Parallel()

	if icol.CellHasExplicitBackground(nil) {
		t.Fatal("nil cell should not have explicit background")
	}
	if !icol.CellHasExplicitBackground(&screen.Cell{Style: screen.Style{Bg: ansi.Red}}) {
		t.Fatal("explicit bg should be detected")
	}
	if !icol.CellHasExplicitBackground(&screen.Cell{Style: screen.Style{
		Fg: ansi.Red, Attrs: screen.AttrReverse,
	}}) {
		t.Fatal("reverse with fg should count as explicit background")
	}
}

func TestBlendNRGBA(t *testing.T) {
	t.Parallel()

	a, b := color.NRGBA{R: 0, G: 100, B: 200, A: 255}, color.NRGBA{R: 100, G: 0, B: 200, A: 55}
	for _, tc := range []struct {
		k    float64
		want color.NRGBA
	}{
		{0, a},
		{1, b},
		{0.5, color.NRGBA{R: 50, G: 50, B: 200, A: 155}},
		{-1, a},
		{2, b},
	} {
		if got := icol.BlendNRGBA(a, b, tc.k); got != tc.want {
			t.Errorf("BlendNRGBA(%v, %v, %v) = %v, want %v", a, b, tc.k, got, tc.want)
		}
		if got := icol.NRGBA(icol.Blend(a, b, tc.k)); got != icol.BlendNRGBA(a, b, tc.k) {
			t.Errorf("Blend and BlendNRGBA differ at %v", tc.k)
		}
	}
}
