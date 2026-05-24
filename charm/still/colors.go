// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image/color"
	"math"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// Package-level VT fallbacks when no per-draw defaults exist; see [EmulatorState]
// and [Palette].
//
// https://ghostty.org/docs/vt/osc/1x
var (
	// defaultForeground is the fallback text color (xterm-style white).
	defaultForeground = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}

	// defaultBackground is the fallback cell background (opaque black: R,G,B 0).
	defaultBackground = color.NRGBA{A: 0xff}
)

// resolveCellColors returns the foreground and background colors for cell
// drawing after palette resolution, global/emulator colors, and SGR-style
// attributes.
//
// Processing order matches typical VT composition: resolve fg/bg, apply
// reverse video by swapping colors (see [uv.AttrReverse]; Ghostty "reversed"
// styling: [ghostty-vt-colors]), blend faint foreground toward background using
// ctx.cfg.faintFactor ([uv.AttrFaint]; SGR parsing in Ghostty: [ghostty-sgr-zig]),
// and conceal by painting glyphs like the background ([uv.AttrConceal]).
//
// [ghostty-vt-colors]: https://ghostty.org/docs/vt/concepts/colors
// [ghostty-sgr-zig]: https://github.com/ghostty-org/ghostty/blob/main/src/terminal/sgr.zig
func resolveCellColors(ctx Context, cell *uv.Cell) (fg, bg color.Color) {
	var style *uv.Style
	if cell != nil {
		style = &cell.Style
	}

	fg = resolveForeground(ctx, style)
	bg = resolveBackground(ctx, style)
	if style != nil && style.Attrs&uv.AttrReverse != 0 {
		fg, bg = bg, fg
	}
	if style != nil && style.Attrs&uv.AttrFaint != 0 {
		fg = blendColor(fg, bg, ctx.cfg.faintFactor)
	}
	if style != nil && style.Attrs&uv.AttrConceal != 0 {
		fg = bg
	}
	return fg, bg
}

// resolveCellBackground returns the color to use when filling the cell
// background. When the cell has no explicit background (and reverse does not
// synthesize one from foreground), this applies ctx.cfg.backgroundOpacity to the
// resolved background alpha so the window/terminal background can appear
// translucent, mirroring Ghostty's background-opacity, which normally affects
// only the default window background unless background-opacity-cells is set:
// [ghostty-bg-opacity], [ghostty-bg-opacity-cells].
//
// Compositing onto the destination image follows [image/draw.Draw] (Porter-Duff
// "source over destination") with sRGB-encoded values in NRGBA, not linear-light
// blending.
//
// [ghostty-bg-opacity]: https://ghostty.org/docs/config/reference#background-opacity
// [ghostty-bg-opacity-cells]: https://ghostty.org/docs/config/reference#background-opacity-cells
func resolveCellBackground(ctx Context, cell *uv.Cell) color.Color {
	_, bg := resolveCellColors(ctx, cell)
	if cellHasExplicitBackground(cell) && !ctx.cfg.backgroundOpacityCells {
		return bg
	}
	return applyAlpha(bg, ctx.cfg.backgroundOpacity)
}

// resolveForeground returns the color for text and decorations. Indexed and
// ANSI basic colors are mapped through ctx.cfg.palette.Indexed when present;
// otherwise palette entries and direct true-color values follow Ghostty's color
// specification model (palette, OSC 4, etc.): [ghostty-vt-colors], [ghostty-osc-4].
//
// [ghostty-vt-colors]: https://ghostty.org/docs/vt/concepts/colors
// [ghostty-osc-4]: https://ghostty.org/docs/vt/osc/4
func resolveForeground(ctx Context, style *uv.Style) color.Color {
	if style != nil && style.Fg != nil {
		return resolvePaletteColor(ctx, style.Fg)
	}
	if ctx.cfg.hasState && ctx.cfg.state.FgColor != nil {
		return ctx.cfg.state.FgColor
	}
	if ctx.cfg.palette.DefaultForeground != nil {
		return ctx.cfg.palette.DefaultForeground
	}
	return defaultForeground
}

// resolveBackground returns the cell background color before reverse-video swap
// and faint/conceal adjustments. Resolution order matches resolveForeground;
// emulator default background corresponds to OSC 11 in Ghostty: [ghostty-osc-1x].
//
// [ghostty-osc-1x]: https://ghostty.org/docs/vt/osc/1x
func resolveBackground(ctx Context, style *uv.Style) color.Color {
	if style != nil && style.Bg != nil {
		return resolvePaletteColor(ctx, style.Bg)
	}
	if ctx.cfg.hasState && ctx.cfg.state.BgColor != nil {
		return ctx.cfg.state.BgColor
	}
	if ctx.cfg.palette.DefaultBackground != nil {
		return ctx.cfg.palette.DefaultBackground
	}
	return defaultBackground
}

// resolvePaletteColor maps ANSI basic or indexed colors through ctx.cfg.palette,
// falling back to c when unmapped. Indexed slots follow the terminal 256-color
// model described in Ghostty's color concepts and OSC 4: [ghostty-vt-colors],
// [ghostty-osc-4].
//
// [ghostty-vt-colors]: https://ghostty.org/docs/vt/concepts/colors
// [ghostty-osc-4]: https://ghostty.org/docs/vt/osc/4
func resolvePaletteColor(ctx Context, c color.Color) color.Color {
	switch v := c.(type) {
	case ansi.BasicColor:
		if mapped := ctx.cfg.palette.Indexed[int(v)]; mapped != nil {
			return mapped
		}
	case ansi.IndexedColor:
		if mapped := ctx.cfg.palette.Indexed[int(v)]; mapped != nil {
			return mapped
		}
	}
	return c
}

// resolveCursor returns the cursor fill color, preferring emulator state, then
// [Palette.Cursor], then the resolved default foreground. Ghostty documents
// dynamic cursor color via OSC 12: [ghostty-osc-1x].
//
// [ghostty-osc-1x]: https://ghostty.org/docs/vt/osc/1x
func resolveCursor(ctx Context) color.Color {
	if ctx.cfg.hasState && ctx.cfg.state.CursorColor != nil {
		return ctx.cfg.state.CursorColor
	}
	if ctx.cfg.palette.Cursor != nil {
		return ctx.cfg.palette.Cursor
	}
	return ctx.ForegroundColor()
}

// cellHasExplicitBackground reports whether the cell carries a background that
// Ghostty would treat as an explicit cell background (solid color not inherited
// only from the window), which affects whether background-opacity applies when
// background-opacity-cells is false: [ghostty-bg-opacity-cells].
//
// Reverse video with a set foreground implies the visible background comes
// from the foreground color after swap, so it is treated as explicit here.
//
// [ghostty-bg-opacity-cells]: https://ghostty.org/docs/config/reference#background-opacity-cells
func cellHasExplicitBackground(cell *uv.Cell) bool {
	if cell == nil {
		return false
	}
	style := cell.Style
	if style.Bg != nil {
		return true
	}
	if style.Attrs&uv.AttrReverse != 0 && style.Fg != nil {
		return true
	}
	return false
}

// applyAlpha multiplies the alpha of c by opacity (clamped to [0,1]). RGB is
// unchanged; premultiplication is not applied here; that happens when converting
// for draw. This models a single opacity factor layered with Ghostty's notion
// of background transparency (background-opacity as "opacity level of the
// background"): [ghostty-bg-opacity].
//
// [ghostty-bg-opacity]: https://ghostty.org/docs/config/reference#background-opacity
func applyAlpha(c color.Color, opacity float64) color.Color {
	if c == nil {
		return nil
	}
	n := nrgba(c)
	n.A = uint8(math.Round(float64(n.A) * clamp(opacity, 0.0, 1.0)))
	return n
}

// blendColor linearly interpolates each NRGBA channel of from toward to by
// factor (clamped to [0,1]). Used for faint text; channels are in sRGB space,
// consistent with typical terminal emulation and [image/draw.Draw] composition,
// not linear-light blending.
func blendColor(from, to color.Color, factor float64) color.Color {
	a := nrgba(from)
	b := nrgba(to)
	factor = clamp(factor, 0.0, 1.0)
	return color.NRGBA{
		R: uint8(math.Round(float64(a.R) + (float64(b.R)-float64(a.R))*factor)),
		G: uint8(math.Round(float64(a.G) + (float64(b.G)-float64(a.G))*factor)),
		B: uint8(math.Round(float64(a.B) + (float64(b.B)-float64(a.B))*factor)),
		A: uint8(math.Round(float64(a.A) + (float64(b.A)-float64(a.A))*factor)),
	}
}

// nrgba converts c to [color.NRGBA], accepting nil as transparent zero.
func nrgba(c color.Color) color.NRGBA {
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
