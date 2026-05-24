// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image/color"
	"maps"
	"time"

	"github.com/lrstanley/x/charm/still/fonts"
	"github.com/lrstanley/x/charm/still/internal/config"
)

// Option is a function that can be used to configure the Renderer. The mutex is
// held for the duration of the function call (it does not lock in-itself).
type Option func(*Renderer)

func (d *Renderer) markDirty(cats config.Dirty) {
	d.dirty |= cats
}

// WithCellWidth adjusts the base value of the cell width (clamped -1,1), as a
// percentage of the default value.
func WithCellWidth(adjust float64) Option {
	return func(d *Renderer) {
		d.opts.CellWidth = NewAdjustment(adjust)
		d.markDirty(config.DirtyMetrics)
	}
}

// WithCellHeight adjusts the base value of the cell height (clamped -1,1), as a
// percentage of the default value.
func WithCellHeight(adjust float64) Option {
	return func(d *Renderer) {
		d.opts.CellHeight = NewAdjustment(adjust)
		d.markDirty(config.DirtyMetrics)
	}
}

// WithFontBaseline adjusts the distance as a percentage from the bottom of the
// cell to the text baseline.
func WithFontBaseline(adjust float64) Option {
	return func(d *Renderer) {
		d.opts.FontBaseline = NewAdjustment(adjust)
		d.markDirty(config.DirtyMetrics)
	}
}

// WithUnderlinePosition adjusts the distance as a percentage from the top of the
// cell to the top of the underline.
func WithUnderlinePosition(adjust float64) Option {
	return func(d *Renderer) {
		d.opts.UnderlinePosition = NewAdjustment(adjust)
		d.markDirty(config.DirtyMetrics)
	}
}

// WithUnderlineThickness adjusts the thickness of the underline in pixels.
func WithUnderlineThickness(px int) Option {
	return func(d *Renderer) {
		d.opts.UnderlineThickness = config.RequiredPx(px, "underline thickness")
		d.markDirty(config.DirtyMetrics)
	}
}

// WithStrikethroughPosition adjusts the distance as a percentage from the top of
// the cell to the top of the strikethrough.
func WithStrikethroughPosition(adjust float64) Option {
	return func(d *Renderer) {
		d.opts.StrikethroughPosition = NewAdjustment(adjust)
		d.markDirty(config.DirtyMetrics)
	}
}

// WithStrikethroughThickness adjusts the thickness of the strikethrough in pixels.
func WithStrikethroughThickness(px int) Option {
	return func(d *Renderer) {
		d.opts.StrikethroughThickness = config.RequiredPx(px, "strikethrough thickness")
		d.markDirty(config.DirtyMetrics)
	}
}

// WithCursorThickness adjusts the thickness of the cursor in pixels.
func WithCursorThickness(px int) Option {
	return func(d *Renderer) {
		d.opts.CursorThickness = config.RequiredPx(px, "cursor thickness")
		d.markDirty(config.DirtyMetrics)
	}
}

// WithCursorHeight adjusts the height as a percentage of the cursor.
func WithCursorHeight(adjust float64) Option {
	return func(d *Renderer) {
		d.opts.CursorHeight = NewAdjustment(adjust)
		d.markDirty(config.DirtyMetrics)
	}
}

// WithBoxThickness sets the stroke thickness of box drawing characters in pixels.
func WithBoxThickness(px int) Option {
	return func(d *Renderer) {
		d.opts.BoxThickness = config.RequiredPx(px, "box thickness")
		d.markDirty(config.DirtyMetrics)
	}
}

// WithIconHeight scales the vertical target band for nerd-icon glyphs.
func WithIconHeight(scale float64) Option {
	return func(d *Renderer) {
		d.opts.IconHeightScale = config.NormalizeIconHeightScale(scale)
		if !d.opts.IconHeightSingleSet {
			d.opts.IconHeightSingleScale = d.opts.IconHeightScale
		}
		d.markDirty(config.DirtyMetrics)
	}
}

// WithIconHeightSingle scales the vertical target band for single-cell nerd icons.
func WithIconHeightSingle(scale float64) Option {
	return func(d *Renderer) {
		d.opts.IconHeightSingleScale = config.NormalizeIconHeightScale(scale)
		d.opts.IconHeightSingleSet = true
		d.markDirty(config.DirtyMetrics)
	}
}

// WithFontSize sets the font size in points. Defaults to 11 (min: 1).
func WithFontSize(size int) Option {
	return WithFontSizePt(Pt(size))
}

// WithFontSizePt sets the font size in points. Defaults to 11pt (min: 1pt).
func WithFontSizePt(size Pt) Option {
	if size < 1 {
		panic("font size must be >= 1pt")
	}
	return func(d *Renderer) {
		d.opts.FontSize = size
		d.markDirty(config.DirtyFonts | config.DirtyMetrics)
	}
}

// WithFontFamily sets the primary font family.
func WithFontFamily(family fonts.FontFamily) Option {
	return func(d *Renderer) {
		d.opts.FontFamily = family
		d.opts.FontFamilySet = true
		d.markDirty(config.DirtyFonts | config.DirtyMetrics)
	}
}

// WithCodepointMap forces Unicode codepoint ranges to map to a specific font family.
func WithCodepointMap(cp map[string]fonts.FontFamily) Option {
	return func(d *Renderer) {
		d.opts.CodepointMapSet = true
		if cp == nil {
			d.opts.CodepointMap = nil
		} else {
			d.opts.CodepointMap = make(map[string]fonts.FontFamily, len(cp))
			maps.Copy(d.opts.CodepointMap, cp)
		}
		d.markDirty(config.DirtyFonts | config.DirtyMetrics)
	}
}

// WithPalette replaces the renderer palette.
func WithPalette(palette Palette) Option {
	return func(d *Renderer) {
		d.opts.Palette = config.ClonePalette(palette)
	}
}

// WithEmulatorState replaces the renderer-owned emulator state.
func WithEmulatorState(state EmulatorState) Option {
	return func(d *Renderer) {
		d.opts.State = state
		d.opts.HasState = true
	}
}

// WithScrollbar sets whether to draw a scrollbar.
func WithScrollbar(enabled bool) Option {
	return func(d *Renderer) {
		d.opts.Scrollbar = enabled
	}
}

// WithBorderRadius sets the border radius in pixels.
func WithBorderRadius(px int) Option {
	return func(d *Renderer) {
		d.opts.BorderRadius = config.PositivePx(px, "border radius")
	}
}

// WithMargin sets the margin in pixels, using the given fill color for the background.
func WithMargin(px int, fill color.Color) Option {
	return func(d *Renderer) {
		d.opts.Margin = config.PositivePx(px, "margin")
		d.opts.MarginFill = fill
	}
}

// WithPadding sets the terminal window padding in pixels.
func WithPadding(px int) Option {
	return func(d *Renderer) {
		d.opts.Padding = config.PositivePx(px, "padding")
	}
}

// WithBackgroundOpacity sets terminal background opacity in the range [0,1].
func WithBackgroundOpacity(opacity float64) Option {
	if opacity < 0 || opacity > 1 {
		panic("background opacity must be between 0 and 1")
	}
	return func(d *Renderer) {
		d.opts.BackgroundOpacity = opacity
	}
}

// WithBackgroundOpacityCells applies background opacity to explicit cell backgrounds too.
func WithBackgroundOpacityCells(enabled bool) Option {
	return func(d *Renderer) {
		d.opts.BackgroundOpacityCells = enabled
	}
}

// WithFaintFactor sets how far faint foreground colors blend toward the background.
func WithFaintFactor(factor float64) Option {
	if factor < 0 || factor > 1 {
		panic("faint factor must be between 0 and 1")
	}
	return func(d *Renderer) {
		d.opts.FaintFactor = factor
	}
}

// WithFocusDimming sets the unfocused dimming overlay opacity in the range [0,1].
func WithFocusDimming(factor float64) Option {
	if factor < 0 || factor > 1 {
		panic("focus dimming must be between 0 and 1")
	}
	return func(d *Renderer) {
		d.opts.FocusDimming = factor
	}
}

// WithCursorBlinkSpeed sets the blink speed for the cursor when blinking is enabled.
func WithCursorBlinkSpeed(speed time.Duration) Option {
	if speed <= 0 {
		panic("cursor blink speed must be > 0")
	}
	return func(d *Renderer) {
		d.opts.CursorBlinkSpeed = speed
	}
}

// WithNow sets the clock used for blink phase calculation.
func WithNow(now func() time.Time) Option {
	if now == nil {
		panic("now function must not be nil")
	}
	return func(d *Renderer) {
		d.opts.Now = now
	}
}

// WithCellBgDrawer sets the cell background drawer for the first cell pass.
func WithCellBgDrawer(drawer CellDrawer) Option {
	return func(d *Renderer) {
		if drawer == nil {
			d.hooks.cellBgDrawer = DrawCellBg
		} else {
			d.hooks.cellBgDrawer = drawer
		}
	}
}

// WithCellFgDrawer sets the cell foreground drawer for the second cell pass.
func WithCellFgDrawer(drawer CellDrawer) Option {
	return func(d *Renderer) {
		if drawer == nil {
			d.hooks.cellFgDrawer = DrawCellFg
		} else {
			d.hooks.cellFgDrawer = drawer
		}
	}
}

// WithCursorDrawer sets the cursor drawer to be used by the renderer.
func WithCursorDrawer(drawer CursorDrawer) Option {
	return func(d *Renderer) {
		if drawer == nil {
			d.hooks.cursorDrawer = DrawCursor
		} else {
			d.hooks.cursorDrawer = drawer
		}
	}
}

// WithScrollbarDrawer sets the scrollbar drawer to be used by the renderer.
func WithScrollbarDrawer(drawer ScrollbarDrawer) Option {
	return func(d *Renderer) {
		if drawer == nil {
			d.hooks.scrollbarDrawer = DrawScrollbar
		} else {
			d.hooks.scrollbarDrawer = drawer
		}
	}
}

// WithBackgroundDrawer sets the background drawer to be used by the renderer.
func WithBackgroundDrawer(drawer BackgroundDrawer) Option {
	return func(d *Renderer) {
		if drawer == nil {
			d.hooks.backgroundDrawer = DrawBackground
		} else {
			d.hooks.backgroundDrawer = drawer
		}
	}
}
