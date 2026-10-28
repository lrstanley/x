// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"errors"
	"fmt"
	"image/color"
	"maps"
	"time"

	"github.com/lrstanley/x/term/reel/render/fonts"
	"github.com/lrstanley/x/term/reel/render/internal/config"
)

// Option configures a [Renderer] at construction time via [New] or [MustNew] only.
type Option func(*Renderer) error

// WithCellWidth adjusts the base value of the cell width (clamped -1,1), as a
// percentage of the default value.
func WithCellWidth(adjust float64) Option {
	return func(d *Renderer) error {
		d.opts.CellWidth = NewAdjustment(adjust)
		return nil
	}
}

// WithCellHeight adjusts the base value of the cell height (clamped -1,1), as a
// percentage of the default value.
func WithCellHeight(adjust float64) Option {
	return func(d *Renderer) error {
		d.opts.CellHeight = NewAdjustment(adjust)
		return nil
	}
}

// WithFontBaseline adjusts the distance as a percentage from the bottom of the
// cell to the text baseline.
func WithFontBaseline(adjust float64) Option {
	return func(d *Renderer) error {
		d.opts.FontBaseline = NewAdjustment(adjust)
		return nil
	}
}

// WithUnderlinePosition adjusts the distance as a percentage from the top of the
// cell to the top of the underline.
func WithUnderlinePosition(adjust float64) Option {
	return func(d *Renderer) error {
		d.opts.UnderlinePosition = NewAdjustment(adjust)
		return nil
	}
}

// WithUnderlineThickness adjusts the thickness of the underline in pixels.
func WithUnderlineThickness(px int) Option {
	return func(d *Renderer) error {
		v, err := config.ParseRequiredPx(px, "underline thickness")
		if err != nil {
			return err
		}
		d.opts.UnderlineThickness = v
		return nil
	}
}

// WithStrikethroughPosition adjusts the distance as a percentage from the top of
// the cell to the top of the strikethrough.
func WithStrikethroughPosition(adjust float64) Option {
	return func(d *Renderer) error {
		d.opts.StrikethroughPosition = NewAdjustment(adjust)
		return nil
	}
}

// WithStrikethroughThickness adjusts the thickness of the strikethrough in pixels.
func WithStrikethroughThickness(px int) Option {
	return func(d *Renderer) error {
		v, err := config.ParseRequiredPx(px, "strikethrough thickness")
		if err != nil {
			return err
		}
		d.opts.StrikethroughThickness = v
		return nil
	}
}

// WithCursorThickness adjusts the thickness of the cursor in pixels.
func WithCursorThickness(px int) Option {
	return func(d *Renderer) error {
		v, err := config.ParseRequiredPx(px, "cursor thickness")
		if err != nil {
			return err
		}
		d.opts.CursorThickness = v
		return nil
	}
}

// WithCursorHeight adjusts the height as a percentage of the cursor.
func WithCursorHeight(adjust float64) Option {
	return func(d *Renderer) error {
		d.opts.CursorHeight = NewAdjustment(adjust)
		return nil
	}
}

// WithBoxThickness sets the stroke thickness of box drawing characters in pixels.
func WithBoxThickness(px int) Option {
	return func(d *Renderer) error {
		v, err := config.ParseRequiredPx(px, "box thickness")
		if err != nil {
			return err
		}
		d.opts.BoxThickness = v
		return nil
	}
}

// WithIconHeight scales the vertical target band for nerd-icon glyphs.
func WithIconHeight(scale float64) Option {
	return func(d *Renderer) error {
		v, err := config.ParseIconHeightScale(scale)
		if err != nil {
			return err
		}
		d.opts.IconHeightScale = v
		if !d.opts.IconHeightSingleSet {
			d.opts.IconHeightSingleScale = d.opts.IconHeightScale
		}
		return nil
	}
}

// WithIconHeightSingle scales the vertical target band for single-cell nerd icons.
func WithIconHeightSingle(scale float64) Option {
	return func(d *Renderer) error {
		v, err := config.ParseIconHeightScale(scale)
		if err != nil {
			return err
		}
		d.opts.IconHeightSingleScale = v
		d.opts.IconHeightSingleSet = true
		return nil
	}
}

// WithScale sets the size of everything drawn from the font: glyphs, and so
// the cell size and the image size with them. Defaults to 1, which is 11pt at
// 96 DPI (about 14.7px). Use 2 for twice the pixels in each direction.
//
// Fixed pixel lengths ([WithPadding], [WithMargin], [WithBorderRadius],
// [WithProgressBarHeight], and so on) do not scale with it. Must be > 0.
func WithScale(scale float64) Option {
	return func(d *Renderer) error {
		v, err := config.ParseScale(scale)
		if err != nil {
			return err
		}
		d.opts.Scale = v
		return nil
	}
}

// WithFontFamily sets the primary font family.
func WithFontFamily(family fonts.FontFamily) Option {
	return func(d *Renderer) error {
		d.opts.FontFamily = family
		d.opts.FontFamilySet = true
		return nil
	}
}

// WithFontFamilyName sets the primary font family by the family name recorded in
// the font files (for example "JetBrains Mono" or "FiraMono Nerd Font Mono"),
// resolving the regular, bold, italic, and bold italic faces from embedded and
// installed fonts. See [fonts.LoadFamily]. Construction fails with an
// error naming the family and the searched directories if it cannot be found.
func WithFontFamilyName(name string) Option {
	return func(d *Renderer) error {
		family, err := fonts.LoadFamily(name)
		if err != nil {
			return err
		}
		d.opts.FontFamily = family
		d.opts.FontFamilySet = true
		return nil
	}
}

// WithCodepointMap forces Unicode codepoint ranges to map to a specific font family.
func WithCodepointMap(cp map[string]fonts.FontFamily) Option {
	return func(d *Renderer) error {
		d.opts.CodepointMapSet = true
		if cp == nil {
			d.opts.CodepointMap = nil
		} else {
			d.opts.CodepointMap = make(map[string]fonts.FontFamily, len(cp))
			maps.Copy(d.opts.CodepointMap, cp)
		}
		return nil
	}
}

// WithSystemFontFallback sets whether runes that no configured or bundled
// font covers (CJK, emoji, uncommon scripts) may be drawn with an installed
// font. It is enabled by default. The first such rune makes the renderer read
// the cmap of every installed font. Disable it for output that must not depend
// on the fonts installed on the machine.
//
// It also lets emoji in wide cells be drawn in full color from an installed
// color font (CBDT, sbix, or COLR version 0), which takes precedence over the
// bundled emoji font; the first emoji reads the tables of every installed font
// again. Without it emoji are still drawn in color, from the bundled font only.
func WithSystemFontFallback(enabled bool) Option {
	return func(d *Renderer) error {
		d.opts.SystemFontFallback = enabled
		return nil
	}
}

// WithPalette sets the terminal color theme: the ANSI indexed colors, default
// foreground and background, cursor, margin, and progress/status colors used
// whenever a cell or control sequence doesn't specify one. Fields left unset
// fall back to built-in defaults. It applies to every frame drawn, whatever the
// output format.
func WithPalette(palette Palette) Option {
	return func(d *Renderer) error {
		d.opts.Palette = config.ClonePalette(palette)
		return nil
	}
}

// PaletteOf returns the palette opts set with [WithPalette], the last one
// winning, or the zero palette, which draws with the built-in defaults,
// without one. It only applies opts, without loading fonts, and returns
// their errors.
func PaletteOf(opts ...Option) (Palette, error) {
	d := &Renderer{opts: config.DefaultOptions()}
	var errs []error
	for _, opt := range opts {
		if opt != nil {
			errs = append(errs, opt(d))
		}
	}
	return config.ClonePalette(d.opts.Palette), errors.Join(errs...)
}

// WithScrollbar sets whether to draw a scrollbar, a track with a thumb for the
// viewport's place in the scrollback, to the right of the cell grid. It is
// only drawn when there is scrollback, and not on the alternate screen. See
// [WithScrollbarWidth] for its width.
func WithScrollbar(enabled bool) Option {
	return func(d *Renderer) error {
		d.opts.Scrollbar = enabled
		return nil
	}
}

// WithScrollbarWidth sets the scrollbar's width in pixels, which is part of
// the image whether or not there is scrollback to show. Zero, the default,
// scales it with the cell width, so it stays in proportion with the text at
// any [WithScale]: a third of a cell, and at least 2 pixels.
func WithScrollbarWidth(px int) Option {
	return func(d *Renderer) error {
		v, err := config.ParsePositivePx(px, "scrollbar width")
		if err != nil {
			return err
		}
		d.opts.ScrollbarWidth = v
		return nil
	}
}

// WithWindowChrome sets whether to draw a title bar above the terminal grid,
// with window buttons and [vt.State.Title]. The bar is part of the window,
// so padding, margin, border radius, and focus dimming apply to it, and it
// grows the image by [Renderer.Metrics] cell height plus half a cell.
// Disabled by default, so cell-accurate screenshots do not change.
//
// Title bar text is drawn a little smaller than the grid, in the same family,
// and is optically centered on the buttons. It is drawn as plain text: the
// title is untrusted, so controls become spaces, and bidi overrides and
// invisible characters are dropped. Program status ([WithProgramStatus]) is
// only drawn with window chrome.
func WithWindowChrome(enabled bool) Option {
	return func(d *Renderer) error {
		d.opts.WindowChrome = enabled
		return nil
	}
}

// WithProgressBar sets whether to draw the OSC 9;4 progress report that
// [vt.State.ShownProgress] returns as a thin bar. With [WithWindowChrome]
// it covers the bottom edge of the title bar, right above the grid; otherwise
// it covers the top edge of the window. It never moves the grid. Enabled by
// default; a state with no progress draws nothing.
func WithProgressBar(enabled bool) Option {
	return func(d *Renderer) error {
		d.opts.ProgressBar = enabled
		return nil
	}
}

// WithProgramStatus sets whether to draw the OSC 7501 program status records
// from [vt.State.ProgramStatuses]. They are drawn only with
// [WithWindowChrome]; without it nothing is drawn. The most urgent record
// (see [vt.ProgramState]), with the earlier record winning ties (the root
// sorts first), replaces the window title with its state icon (or a spinner,
// or the percent while working at a known progress), title, and message, and
// draws a thin state-colored outline just outside the window edge. Each other
// record adds a dot in its own state color after the title, most urgent
// first, up to six. With no records the normal title is drawn. Enabled by
// default.
//
// The outline sits in the margin ([WithMargin]) and follows
// [WithBorderRadius]. With less than 4px of margin it covers the window's
// outermost pixels instead. Working records move comets around it, or fill it
// clockwise from the top center to their progress; blocked records breathe;
// done, error, and idle records are steady. Movement follows the offset
// passed to [Renderer.Draw] and loops. Colors come from [Palette.Progress],
// [Palette.ProgressPause], [Palette.ProgressError], and [Palette.ProgramDone].
func WithProgramStatus(enabled bool) Option {
	return func(d *Renderer) error {
		d.opts.ProgramStatus = enabled
		return nil
	}
}

// WithProgressBarHeight sets the progress bar height in pixels. The default
// is 2. Colors come from [Palette.Progress], [Palette.ProgressError], and
// [Palette.ProgressPause].
func WithProgressBarHeight(px int) Option {
	return func(d *Renderer) error {
		v, err := config.ParseRequiredPx(px, "progress bar height")
		if err != nil {
			return err
		}
		d.opts.ProgressBarHeight = v
		return nil
	}
}

// WithBorderRadius sets the border radius in pixels.
func WithBorderRadius(px int) Option {
	return func(d *Renderer) error {
		v, err := config.ParsePositivePx(px, "border radius")
		if err != nil {
			return err
		}
		d.opts.BorderRadius = v
		return nil
	}
}

// WithMargin sets the margin in pixels, using the given fill color for the background.
func WithMargin(px int, fill color.Color) Option {
	return func(d *Renderer) error {
		v, err := config.ParsePositivePx(px, "margin")
		if err != nil {
			return err
		}
		d.opts.Margin = v
		d.opts.MarginFill = fill
		return nil
	}
}

// WithPadding sets the terminal window padding in pixels.
func WithPadding(px int) Option {
	return func(d *Renderer) error {
		v, err := config.ParsePositivePx(px, "padding")
		if err != nil {
			return err
		}
		d.opts.Padding = v
		return nil
	}
}

// WithBackgroundOpacity sets terminal background opacity in the range [0,1].
// Defaults to 1 (fully opaque).
func WithBackgroundOpacity(opacity float64) Option {
	return func(d *Renderer) error {
		if opacity < 0 || opacity > 1 {
			return fmt.Errorf("render: background opacity must be between 0 and 1, got %v", opacity)
		}
		d.opts.BackgroundOpacity = opacity
		return nil
	}
}

// WithBackgroundOpacityCells applies background opacity to explicit cell backgrounds too.
func WithBackgroundOpacityCells(enabled bool) Option {
	return func(d *Renderer) error {
		d.opts.BackgroundOpacityCells = enabled
		return nil
	}
}

// WithFaintFactor sets how far faint foreground colors blend toward the
// background, in the range [0,1]. Defaults to 0.5.
func WithFaintFactor(factor float64) Option {
	return func(d *Renderer) error {
		if factor < 0 || factor > 1 {
			return fmt.Errorf("render: faint factor must be between 0 and 1, got %v", factor)
		}
		d.opts.FaintFactor = factor
		return nil
	}
}

// WithFocusDimming sets the unfocused dimming overlay opacity in the range [0,1].
// Defaults to 0.16. A value of 0 disables dimming.
func WithFocusDimming(factor float64) Option {
	return func(d *Renderer) error {
		if factor < 0 || factor > 1 {
			return fmt.Errorf("render: focus dimming must be between 0 and 1, got %v", factor)
		}
		d.opts.FocusDimming = factor
		return nil
	}
}

// WithCursorBlinkSpeed sets the blink speed for the cursor when blinking is
// enabled. Defaults to 600ms.
func WithCursorBlinkSpeed(speed time.Duration) Option {
	return func(d *Renderer) error {
		if speed <= 0 {
			return fmt.Errorf("render: cursor blink speed must be > 0, got %v", speed)
		}
		d.opts.CursorBlinkSpeed = speed
		return nil
	}
}
