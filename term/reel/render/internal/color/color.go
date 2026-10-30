// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package color resolves terminal palette entries and style attributes into
// draw colors for cell foreground, background, and cursor rendering.
package color //nolint:revive // terminal palette resolution, distinct from image/color

import (
	"image/color"
	"math"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/reel/render/internal/config"
	"github.com/lrstanley/x/term/reel/render/units"
	"github.com/lrstanley/x/term/reel/vt"
)

var (
	defaultForeground = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	defaultBackground = color.NRGBA{A: 0xff}
)

// ResolveCellColorsNRGBA returns foreground and background as concrete NRGBA
// values, after reverse, faint, and conceal.
func ResolveCellColorsNRGBA(src config.ColorSource, cell *vt.Cell) (fg, bg color.NRGBA) {
	var style *vt.Style
	if cell != nil {
		style = &cell.Style
	}

	fg = ResolveForegroundNRGBA(src, style)
	bg = ResolveBackgroundNRGBA(src, style)
	if style != nil && style.Attrs&vt.AttrReverse != 0 {
		fg, bg = bg, fg
	}
	if style != nil && style.Attrs&vt.AttrFaint != 0 {
		fg = BlendNRGBA(fg, bg, src.FaintFactor())
	}
	if style != nil && style.Attrs&vt.AttrConceal != 0 {
		fg = bg
	}
	return fg, bg
}

// ResolveCellBackgroundNRGBA returns the NRGBA fill for a cell background.
func ResolveCellBackgroundNRGBA(src config.ColorSource, cell *vt.Cell) color.NRGBA {
	_, bg := ResolveCellColorsNRGBA(src, cell)
	if CellHasExplicitBackground(cell) && !src.BackgroundOpacityCells() {
		return bg
	}
	return ApplyAlphaNRGBA(bg, src.BackgroundOpacity())
}

// ResolveForegroundNRGBA returns the color for text and decorations without interface boxing.
func ResolveForegroundNRGBA(src config.ColorSource, style *vt.Style) color.NRGBA {
	if style != nil && style.Fg != nil {
		return NRGBA(ResolvePaletteColor(src, style.Fg))
	}
	fg, _ := defaultColors(src)
	return fg
}

// ResolveBackgroundNRGBA returns the cell background color before the SGR 7
// swap.
func ResolveBackgroundNRGBA(src config.ColorSource, style *vt.Style) color.NRGBA {
	if style != nil && style.Bg != nil {
		return NRGBA(ResolvePaletteColor(src, style.Bg))
	}
	_, bg := defaultColors(src)
	return bg
}

// defaultColors returns the colors of cells that set none: the program's,
// else the palette's, swapped under reverse video (DECSCNM).
func defaultColors(src config.ColorSource) (fg, bg color.NRGBA) {
	st, palette := src.State(), src.Palette()
	fg, bg = st.Foreground, st.Background
	if fg == (color.NRGBA{}) {
		fg = defaultForeground
		if palette.DefaultForeground != nil {
			fg = NRGBA(palette.DefaultForeground)
		}
	}
	if bg == (color.NRGBA{}) {
		bg = defaultBackground
		if palette.DefaultBackground != nil {
			bg = NRGBA(palette.DefaultBackground)
		}
	}
	if st.ReverseColors {
		fg, bg = bg, fg
	}
	return fg, bg
}

// ResolvePaletteColor maps ANSI basic or indexed colors through the colors
// the program set, then the palette.
func ResolvePaletteColor(src config.ColorSource, c color.Color) color.Color {
	var i uint8
	switch v := c.(type) {
	case ansi.BasicColor:
		i = uint8(v)
	case ansi.IndexedColor:
		i = uint8(v)
	case vt.Indexed:
		i = uint8(v)
	default:
		return c
	}
	if mapped, ok := src.State().Palette.Color(i); ok {
		return mapped
	}
	if mapped := src.Palette().Indexed[int(i)]; mapped != nil {
		return mapped
	}
	return c
}

// ResolveCursorNRGBA returns the cursor fill color.
func ResolveCursorNRGBA(src config.ColorSource, defaultForeground color.NRGBA) color.NRGBA {
	if st := src.State(); st.CursorColorSet {
		return st.CursorColor
	}
	palette := src.Palette()
	if palette.Cursor != nil {
		return NRGBA(palette.Cursor)
	}
	return defaultForeground
}

// CellHasExplicitBackground reports whether the cell carries an explicit
// background: its own color, or SGR 7, whose fill is the foreground. Like in
// Ghostty, such cells are drawn opaque.
func CellHasExplicitBackground(cell *vt.Cell) bool {
	return cell != nil && (cell.Style.Bg != nil || cell.Style.Attrs&vt.AttrReverse != 0)
}

// ApplyAlphaNRGBA multiplies the alpha of c by opacity without interface boxing.
func ApplyAlphaNRGBA(c color.NRGBA, opacity float64) color.NRGBA {
	n := c
	n.A = uint8(math.Round(float64(n.A) * units.Clamp(opacity, 0.0, 1.0)))
	return n
}

// ApplyAlpha multiplies the alpha of c by opacity (clamped to [0,1]).
func ApplyAlpha(c color.Color, opacity float64) color.Color {
	if c == nil {
		return nil
	}
	return ApplyAlphaNRGBA(NRGBA(c), opacity)
}

// Blend linearly interpolates each NRGBA channel of from toward to by factor.
func Blend(from, to color.Color, factor float64) color.Color {
	return BlendNRGBA(NRGBA(from), NRGBA(to), factor)
}

// BlendNRGBA linearly interpolates each channel of a toward b by k, clamped
// to 0 through 1, without boxing.
func BlendNRGBA(a, b color.NRGBA, k float64) color.NRGBA {
	k = units.Clamp(k, 0.0, 1.0)
	lerp := func(x, y uint8) uint8 {
		return uint8(math.Round(float64(x) + (float64(y)-float64(x))*k))
	}
	return color.NRGBA{R: lerp(a.R, b.R), G: lerp(a.G, b.G), B: lerp(a.B, b.B), A: lerp(a.A, b.A)}
}

// NRGBA converts c to [color.NRGBA], accepting nil as transparent zero.
func NRGBA(c color.Color) color.NRGBA {
	if c == nil {
		return color.NRGBA{}
	}
	if n, ok := c.(color.NRGBA); ok {
		return n
	}
	converted, ok := color.NRGBAModel.Convert(c).(color.NRGBA)
	if !ok {
		return color.NRGBA{}
	}
	return converted
}

// BlendOverBg alpha-composites fg over bg using alpha.
func BlendOverBg(fg, bg color.NRGBA, alpha uint8) color.NRGBA {
	if alpha == 255 {
		return fg
	}
	if alpha == 0 {
		return bg
	}
	a := float64(alpha) / 255
	return color.NRGBA{
		R: uint8(float64(fg.R)*a + float64(bg.R)*(1-a)),
		G: uint8(float64(fg.G)*a + float64(bg.G)*(1-a)),
		B: uint8(float64(fg.B)*a + float64(bg.B)*(1-a)),
		A: 255,
	}
}
