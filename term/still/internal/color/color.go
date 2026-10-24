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
	"github.com/lrstanley/x/term/still/internal/config"
	"github.com/lrstanley/x/term/still/units"
	"github.com/lrstanley/x/term/vt/screen"
)

var (
	defaultForeground = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	defaultBackground = color.NRGBA{A: 0xff}
)

// resolveCellColors is the canonical fg/bg resolver (reverse, faint, conceal).
func resolveCellColors(src config.ColorSource, cell *screen.Cell) (fg, bg color.NRGBA) {
	var style *screen.Style
	if cell != nil {
		style = &cell.Style
	}

	fg = ResolveForegroundNRGBA(src, style)
	bg = ResolveBackgroundNRGBA(src, style)
	if style != nil && style.Attrs&screen.AttrReverse != 0 {
		fg, bg = bg, fg
	}
	if style != nil && style.Attrs&screen.AttrFaint != 0 {
		fg = BlendNRGBA(fg, bg, src.FaintFactor())
	}
	if style != nil && style.Attrs&screen.AttrConceal != 0 {
		fg = bg
	}
	return fg, bg
}

// ResolveCellColorsNRGBA returns foreground and background as concrete NRGBA values.
func ResolveCellColorsNRGBA(src config.ColorSource, cell *screen.Cell) (fg, bg color.NRGBA) {
	return resolveCellColors(src, cell)
}

// ResolveCellBackgroundNRGBA returns the NRGBA fill for a cell background.
func ResolveCellBackgroundNRGBA(src config.ColorSource, cell *screen.Cell) color.NRGBA {
	_, bg := resolveCellColors(src, cell)
	if CellHasExplicitBackground(cell) && !src.BackgroundOpacityCells() {
		return bg
	}
	return ApplyAlphaNRGBA(bg, src.BackgroundOpacity())
}

// ResolveForegroundNRGBA returns the color for text and decorations without interface boxing.
func ResolveForegroundNRGBA(src config.ColorSource, style *screen.Style) color.NRGBA {
	if style != nil && style.Fg != nil {
		return NRGBA(ResolvePaletteColor(src, style.Fg))
	}
	if state, ok := src.EmulatorState(); ok && state.FgColor != nil {
		return NRGBA(state.FgColor)
	}
	palette := src.Palette()
	if palette.DefaultForeground != nil {
		return NRGBA(palette.DefaultForeground)
	}
	return defaultForeground
}

// ResolveBackgroundNRGBA returns the cell background color before reverse-video swap.
func ResolveBackgroundNRGBA(src config.ColorSource, style *screen.Style) color.NRGBA {
	if style != nil && style.Bg != nil {
		return NRGBA(ResolvePaletteColor(src, style.Bg))
	}
	if state, ok := src.EmulatorState(); ok && state.BgColor != nil {
		return NRGBA(state.BgColor)
	}
	palette := src.Palette()
	if palette.DefaultBackground != nil {
		return NRGBA(palette.DefaultBackground)
	}
	return defaultBackground
}

// ResolvePaletteColor maps ANSI basic or indexed colors through the palette.
func ResolvePaletteColor(src config.ColorSource, c color.Color) color.Color {
	palette := src.Palette()
	switch v := c.(type) {
	case ansi.BasicColor:
		if mapped := palette.Indexed[int(v)]; mapped != nil {
			return mapped
		}
	case ansi.IndexedColor:
		if mapped := palette.Indexed[int(v)]; mapped != nil {
			return mapped
		}
	case screen.Indexed:
		if mapped := palette.Indexed[int(v)]; mapped != nil {
			return mapped
		}
	}
	return c
}

// ResolveCursorNRGBA returns the cursor fill color.
func ResolveCursorNRGBA(src config.ColorSource, defaultForeground color.NRGBA) color.NRGBA {
	if state, ok := src.EmulatorState(); ok && state.CursorColor != nil {
		return NRGBA(state.CursorColor)
	}
	palette := src.Palette()
	if palette.Cursor != nil {
		return NRGBA(palette.Cursor)
	}
	return defaultForeground
}

// CellHasExplicitBackground reports whether the cell carries an explicit background.
func CellHasExplicitBackground(cell *screen.Cell) bool {
	if cell == nil {
		return false
	}
	style := cell.Style
	if style.Bg != nil {
		return true
	}
	if style.Attrs&screen.AttrReverse != 0 && style.Fg != nil {
		return true
	}
	return false
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
