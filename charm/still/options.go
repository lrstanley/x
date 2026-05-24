// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image/color"
	"maps"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/charm/still/fonts"
)

// Palette contains sparse terminal colors used when rendered cells or emulator
// state do not provide more specific colors.
type Palette struct {
	Indexed           map[int]color.Color
	DefaultForeground color.Color
	DefaultBackground color.Color
	Cursor            color.Color
	CursorText        color.Color
	Margin            color.Color
}

// EmulatorState contains optional terminal state that is not carried by
// [uv.Screen].
type EmulatorState struct {
	Title           string
	Focused         bool
	AltScreen       bool
	CursorVisible   bool
	CursorX         int
	CursorY         int
	CursorColor     color.Color
	CursorStyle     uv.CursorShape
	CursorBlink     bool
	FgColor         color.Color
	BgColor         color.Color
	ScrollbackCount int
}

// Option is a function that can be used to configure the Renderer. The mutex is
// held for the duration of the function call (it does not lock in-itself).
//
// Options mutate [rendererOptions], then OR bits into the renderer's dirty set so
// [Renderer.Apply] / [Renderer.Draw] can rebuild fonts and/or metrics without redoing
// unrelated work. Palette, geometry, emulator state, and render hooks apply on
// the next draw without triggering font or metric rebuilds.
type Option func(*Renderer)

// rendererOptions is the merged, user-visible configuration snapshot owned by a
// [Renderer]. [Metrics] related fields follow the same conceptual layout as Ghostty's
// font metric pipeline (see Zig implementation [ghostty-metrics-zig]) and the
// user-facing names in the Ghostty configuration reference [ghostty-config]
// (headings adjust-* and font-*).
//
// Several bool pair (*Set) fields distinguish "unset use builtin default" from an
// explicit empty or zero value ([rendererOptions.fontFamilySet],
// [rendererOptions.codepointMapSet]). [rendererOptions.hasState] is true after
// [WithEmulatorState] has been applied.
//
// [ghostty-metrics-zig]: https://github.com/ghostty-org/ghostty/blob/main/src/font/Metrics.zig
// [ghostty-config]: https://ghostty.org/docs/config/reference
type rendererOptions struct {
	dpi      DPI
	fontSize Pt

	cellWidth              Adjustment
	cellHeight             Adjustment
	fontBaseline           Adjustment
	underlinePosition      Adjustment
	underlineThickness     Px
	strikethroughPosition  Adjustment
	strikethroughThickness Px
	cursorThickness        Px
	cursorHeight           Adjustment
	boxThickness           Px
	iconHeightScale        float64
	iconHeightSingleScale  float64
	iconHeightSingleSet    bool

	fontFamily      fonts.FontFamily
	fontFamilySet   bool
	codepointMap    map[string]fonts.FontFamily
	codepointMapSet bool

	palette Palette

	state    EmulatorState
	hasState bool

	scrollbar              bool
	borderRadius           Px
	margin                 Px
	marginFill             color.Color
	padding                Px
	backgroundOpacity      float64
	backgroundOpacityCells bool
	faintFactor            float64
	focusDimming           float64
	cursorBlinkSpeed       time.Duration
	now                    func() time.Time

	cellBgDrawer     CellDrawer
	cellFgDrawer     CellDrawer
	cursorDrawer     CursorDrawer
	scrollbarDrawer  ScrollbarDrawer
	backgroundDrawer BackgroundDrawer
}

// dirtyCategories labels rebuild buckets for font loading and metric derivation.
// Fonts and cell metric derivation are split because font reload implies metric
// recomputation, while purely metric overrides skip font IO when faces are unchanged.
type dirtyCategories uint8

const dirtyNone dirtyCategories = 0

const (
	// dirtyMetrics means cell dimensions, underline/strikethrough positions,
	// cursor geometry in cells, icon scaling, etc. must be recomputed from faces.
	dirtyMetrics dirtyCategories = 1 << iota
	// dirtyFonts means font families or [rendererOptions.codepointMap] changed.
	dirtyFonts
)

// defaultRendererOptions returns the default renderer options.
func defaultRendererOptions() rendererOptions {
	return rendererOptions{
		dpi:                   DefaultDPI,
		fontSize:              DefaultFontSize,
		cursorThickness:       1,
		backgroundOpacity:     DefaultBgOpacity,
		faintFactor:           DefaultFaintFactor,
		focusDimming:          DefaultFocusDimming,
		cursorBlinkSpeed:      DefaultCursorBlinkSpeed,
		iconHeightScale:       1,
		iconHeightSingleScale: 1,
		now:                   time.Now,
		cellBgDrawer:          DrawCellBg,
		cellFgDrawer:          DrawCellFg,
		cursorDrawer:          DrawCursor,
		scrollbarDrawer:       DrawScrollbar,
		backgroundDrawer:      DrawBackground,
	}
}

func positivePx(px int, name string) Px {
	if px < 0 {
		panic(name + " must be >= 0")
	}
	return Px(px)
}

func requiredPx(px int, name string) Px {
	if px <= 0 {
		panic(name + " must be > 0")
	}
	return Px(px)
}

func clonePalette(p Palette) Palette {
	if len(p.Indexed) == 0 {
		p.Indexed = nil
		return p
	}
	indexed := make(map[int]color.Color, len(p.Indexed))
	maps.Copy(indexed, p.Indexed)
	p.Indexed = indexed
	return p
}

// WithCellWidth adjusts the base value of the cell width (clamped -1,1), as a
// percentage of the default value. Some values are clamped to minimum or maximum
// values, which can make it appear that certain values are ignored. For example,
// *Thickness adjustments cannot go below 1px.
func WithCellWidth(adjust float64) Option {
	return func(d *Renderer) {
		d.opts.cellWidth = NewAdjustment(adjust)
		d.markDirty(dirtyMetrics)
	}
}

// WithCellHeight adjusts the base value of the cell height (clamped -1,1), as a
// percentage of the default value. Some values are clamped to minimum or maximum
// values, which can make it appear that certain values are ignored. For example,
// *Thickness adjustments cannot go below 1px.
//
// This option has some additional behaviors to account for:
//   - The font will be centered vertically in the cell.
//   - The cursor will remain the same size as the font, but may be adjusted
//     separately.
//   - Powerline glyphs will be adjusted along with the cell height, so that things
//     like status lines continue to look aligned.
func WithCellHeight(adjust float64) Option {
	return func(d *Renderer) {
		d.opts.cellHeight = NewAdjustment(adjust)
		d.markDirty(dirtyMetrics)
	}
}

// WithFontBaseline adjusts the distance as a percentage from the bottom of the
// cell to the text baseline. Increase to move baseline UP, decrease to move
// baseline DOWN. See the notes about adjustments in [WithCellWidth].
func WithFontBaseline(adjust float64) Option {
	return func(d *Renderer) {
		d.opts.fontBaseline = NewAdjustment(adjust)
		d.markDirty(dirtyMetrics)
	}
}

// WithUnderlinePosition adjusts the distance as a percentage from the top of the
// cell to the top of the underline. Increase to move underline DOWN, decrease to
// move underline UP. See the notes about adjustments in [WithCellWidth].
func WithUnderlinePosition(adjust float64) Option {
	return func(d *Renderer) {
		d.opts.underlinePosition = NewAdjustment(adjust)
		d.markDirty(dirtyMetrics)
	}
}

// WithUnderlineThickness adjusts the thickness of the underline in pixels. See
// the notes about adjustments in [WithCellWidth]. Minimum is 1px.
func WithUnderlineThickness(px int) Option {
	return func(d *Renderer) {
		d.opts.underlineThickness = requiredPx(px, "underline thickness")
		d.markDirty(dirtyMetrics)
	}
}

// WithStrikethroughPosition adjusts the distance as a percentage from the top of
// the cell to the top of the strikethrough. Increase to move strikethrough DOWN,
// decrease to move strikethrough UP. See the notes about adjustments in
// [WithCellWidth].
func WithStrikethroughPosition(adjust float64) Option {
	return func(d *Renderer) {
		d.opts.strikethroughPosition = NewAdjustment(adjust)
		d.markDirty(dirtyMetrics)
	}
}

// WithStrikethroughThickness adjusts the thickness of the strikethrough in pixels.
// See the notes about adjustments in [WithCellWidth]. Minimum is 1px.
func WithStrikethroughThickness(px int) Option {
	return func(d *Renderer) {
		d.opts.strikethroughThickness = requiredPx(px, "strikethrough thickness")
		d.markDirty(dirtyMetrics)
	}
}

// WithCursorThickness adjusts the thickness of the cursor in pixels. See the
// notes about adjustments in [WithCellWidth]. Minimum is 1px.
func WithCursorThickness(px int) Option {
	return func(d *Renderer) {
		d.opts.cursorThickness = requiredPx(px, "cursor thickness")
		d.markDirty(dirtyMetrics)
	}
}

// WithCursorHeight adjusts the height as a percentage of the cursor. Currently
// applies to all cursor types ([CursorShape]). See the notes about adjustments in
// [WithCellWidth].
func WithCursorHeight(adjust float64) Option {
	return func(d *Renderer) {
		d.opts.cursorHeight = NewAdjustment(adjust)
		d.markDirty(dirtyMetrics)
	}
}

// WithBoxThickness sets the stroke thickness of box drawing characters in pixels.
// See the notes about adjustments in [WithCellWidth]. Minimum is 1px.
func WithBoxThickness(px int) Option {
	return func(d *Renderer) {
		d.opts.boxThickness = requiredPx(px, "box thickness")
		d.markDirty(dirtyMetrics)
	}
}

// WithIconHeight scales the vertical target band for [glyphNerdIcon] glyphs
// (codepoint-mapped symbol faces). [glyphGrid] monospace text and
// [glyphPowerline] separators use independent layout and ignore this option.
//
// Scale is a direct multiplier: 1 preserves the default target, 0.5 halves it,
// and 1.5 increases it by half. Scale must be >= 0; negative values panic.
func WithIconHeight(scale float64) Option {
	return func(d *Renderer) {
		d.opts.iconHeightScale = normalizeIconHeightScale(scale)
		if !d.opts.iconHeightSingleSet {
			d.opts.iconHeightSingleScale = d.opts.iconHeightScale
		}
		d.markDirty(dirtyMetrics)
	}
}

// WithIconHeightSingle scales the vertical target band for single-cell
// [glyphNerdIcon] glyphs. This overrides the single-cell target set by
// [WithIconHeight]. Scale uses the same direct-multiplier semantics as
// [WithIconHeight]; negative values panic.
func WithIconHeightSingle(scale float64) Option {
	return func(d *Renderer) {
		d.opts.iconHeightSingleScale = normalizeIconHeightScale(scale)
		d.opts.iconHeightSingleSet = true
		d.markDirty(dirtyMetrics)
	}
}

func normalizeIconHeightScale(scale float64) float64 {
	if scale < 0 {
		panic("icon height scale must be >= 0")
	}
	return scale
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
		d.opts.fontSize = size
		d.markDirty(dirtyFonts | dirtyMetrics)
	}
}

// WithFontFamily sets the primary font family, using JetBrains Mono as the default.
// Regular must be non-nil and must always be a monospace font. Missing variants
// synthesize from Regular unless FontFamily.DisableSynthetic is true.
func WithFontFamily(family fonts.FontFamily) Option {
	return func(d *Renderer) {
		d.opts.fontFamily = family
		d.opts.fontFamilySet = true
		d.markDirty(dirtyFonts | dirtyMetrics)
	}
}

// WithCodepointMap forces one or a range of Unicode codepoints to map to a specific
// font family. This is useful if you want to support special symbols or if you
// want to use specific glyphs that render better for your primary font, e.g.
// Symbols Nerd Font Mono (builtin, and used by default for all private-space
// symbols).
//
// Map keys are codepoint specs, where each spec is either a single codepoint or a
// range. Codepoints must be specified as full Unicode hex values, such as
// `U+ABCD`. Codepoints ranges are specified as `U+ABCD-U+DEFG`. You can specify
// multiple ranges for the same family separated by commas, such as
// `U+ABCD-U+DEFG,U+1234-U+5678`.
//
// Set to nil to disable codepoint mapping (including the defaults).
func WithCodepointMap(cp map[string]fonts.FontFamily) Option {
	return func(d *Renderer) {
		d.opts.codepointMapSet = true
		if cp == nil {
			d.opts.codepointMap = nil
		} else {
			d.opts.codepointMap = make(map[string]fonts.FontFamily, len(cp))
			maps.Copy(d.opts.codepointMap, cp)
		}
		d.markDirty(dirtyFonts | dirtyMetrics)
	}
}

// WithPalette replaces the renderer palette.
func WithPalette(palette Palette) Option {
	return func(d *Renderer) {
		d.opts.palette = clonePalette(palette)
	}
}

// WithEmulatorState replaces the renderer-owned emulator state.
func WithEmulatorState(state EmulatorState) Option {
	return func(d *Renderer) {
		d.opts.state = state
		d.opts.hasState = true
	}
}

// WithScrollbar sets whether to draw a scrollbar. Even if enabled, the scrollbar
// will only be drawn if the emulator state has a scrollback count greater than 0,
// and not in alt screen mode.
func WithScrollbar(enabled bool) Option {
	return func(d *Renderer) {
		d.opts.scrollbar = enabled
	}
}

// WithBorderRadius sets the border radius in pixels.
func WithBorderRadius(px int) Option {
	return func(d *Renderer) {
		d.opts.borderRadius = positivePx(px, "border radius")
	}
}

// WithMargin sets the margin in pixels, using the given fill color for the background.
func WithMargin(px int, fill color.Color) Option {
	return func(d *Renderer) {
		d.opts.margin = positivePx(px, "margin")
		d.opts.marginFill = fill
	}
}

// WithPadding sets the terminal window padding in pixels. Padding sits inside
// the rounded window between the window edge and the cell grid.
func WithPadding(px int) Option {
	return func(d *Renderer) {
		d.opts.padding = positivePx(px, "padding")
	}
}

// WithBackgroundOpacity sets terminal background opacity in the range [0,1].
// Explicit cell backgrounds remain opaque unless [WithBackgroundOpacityCells]
// is enabled.
func WithBackgroundOpacity(opacity float64) Option {
	if opacity < 0 || opacity > 1 {
		panic("background opacity must be between 0 and 1")
	}
	return func(d *Renderer) {
		d.opts.backgroundOpacity = opacity
	}
}

// WithBackgroundOpacityCells applies background opacity to explicit cell
// backgrounds too.
func WithBackgroundOpacityCells(enabled bool) Option {
	return func(d *Renderer) {
		d.opts.backgroundOpacityCells = enabled
	}
}

// WithFaintFactor sets how far faint foreground colors blend toward the
// resolved background in the range [0,1].
func WithFaintFactor(factor float64) Option {
	if factor < 0 || factor > 1 {
		panic("faint factor must be between 0 and 1")
	}
	return func(d *Renderer) {
		d.opts.faintFactor = factor
	}
}

// WithFocusDimming sets the unfocused dimming overlay opacity in the range
// [0,1].
func WithFocusDimming(factor float64) Option {
	if factor < 0 || factor > 1 {
		panic("focus dimming must be between 0 and 1")
	}
	return func(d *Renderer) {
		d.opts.focusDimming = factor
	}
}

// WithCursorBlinkSpeed sets the blink speed for the cursor, when cursor blinking
// is enabled. Defaults to 600ms (i.e. 1200ms for a full blink cycle).
func WithCursorBlinkSpeed(speed time.Duration) Option {
	if speed <= 0 {
		panic("cursor blink speed must be > 0")
	}
	return func(d *Renderer) {
		d.opts.cursorBlinkSpeed = speed
	}
}

// WithNow sets the clock used for blink phase calculation in [blinkVisible],
// [textVisible], and [cursorVisible]. Phase boundaries fall on multiples of each
// blink half-cycle since the Unix epoch; use fixed timestamps in tests to pin
// on vs off frames (see TestRendererTextBlinkUsesDeterministicClock).
func WithNow(now func() time.Time) Option {
	if now == nil {
		panic("now function must not be nil")
	}
	return func(d *Renderer) {
		d.opts.now = now
	}
}

// WithCellBgDrawer sets the cell background drawer for the first cell pass.
// Defaults to [DrawCellBg]. Drawing of foreground and backgrounds is done
// in two separate passes, to ensure that some glyphs (e.g. synthetic italic) can
// extend into the next cell without being covered by that cell's background.
func WithCellBgDrawer(drawer CellDrawer) Option {
	return func(d *Renderer) {
		if drawer == nil {
			d.opts.cellBgDrawer = DrawCellBg
		} else {
			d.opts.cellBgDrawer = drawer
		}
	}
}

// WithCellFgDrawer sets the cell foreground drawer for the second cell pass.
// Defaults to [DrawCellFg]. Drawing of foreground and backgrounds is done
// in two separate passes, to ensure that some glyphs (e.g. synthetic italic) can
// extend into the next cell without being covered by that cell's background.
func WithCellFgDrawer(drawer CellDrawer) Option {
	return func(d *Renderer) {
		if drawer == nil {
			d.opts.cellFgDrawer = DrawCellFg
		} else {
			d.opts.cellFgDrawer = drawer
		}
	}
}

// WithCursorDrawer sets the cursor drawer to be used by the renderer. Defaults to
// [DrawCursor].
func WithCursorDrawer(drawer CursorDrawer) Option {
	return func(d *Renderer) {
		if drawer == nil {
			d.opts.cursorDrawer = DrawCursor
		} else {
			d.opts.cursorDrawer = drawer
		}
	}
}

// WithScrollbarDrawer sets the scrollbar drawer to be used by the renderer. Defaults to
// [DrawScrollbar].
func WithScrollbarDrawer(drawer ScrollbarDrawer) Option {
	return func(d *Renderer) {
		if drawer == nil {
			d.opts.scrollbarDrawer = DrawScrollbar
		} else {
			d.opts.scrollbarDrawer = drawer
		}
	}
}

// WithBackgroundDrawer sets the background drawer to be used by the renderer. Defaults to
// [DrawBackground].
func WithBackgroundDrawer(drawer BackgroundDrawer) Option {
	return func(d *Renderer) {
		if drawer == nil {
			d.opts.backgroundDrawer = DrawBackground
		} else {
			d.opts.backgroundDrawer = drawer
		}
	}
}
