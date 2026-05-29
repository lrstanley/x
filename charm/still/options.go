// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"errors"
	"fmt"
	"image/color"
	"maps"
	"time"

	"github.com/lrstanley/x/charm/still/fonts"
	"github.com/lrstanley/x/charm/still/internal/config"
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

// WithFontSize sets the font size in points. Defaults to 11 (min: 1).
func WithFontSize(size int) Option {
	return WithFontSizePt(Pt(size))
}

// WithFontSizePt sets the font size in points. Defaults to 11pt (min: 1pt).
func WithFontSizePt(size Pt) Option {
	return func(d *Renderer) error {
		if size < 1 {
			return fmt.Errorf("still: font size must be >= 1pt, got %v", size)
		}
		d.opts.FontSize = size
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

// WithPalette replaces the renderer palette.
func WithPalette(palette Palette) Option {
	return func(d *Renderer) error {
		d.opts.Palette = config.ClonePalette(palette)
		return nil
	}
}

// WithScrollbar sets whether to draw a scrollbar.
func WithScrollbar(enabled bool) Option {
	return func(d *Renderer) error {
		d.opts.Scrollbar = enabled
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
func WithBackgroundOpacity(opacity float64) Option {
	return func(d *Renderer) error {
		if opacity < 0 || opacity > 1 {
			return fmt.Errorf("still: background opacity must be between 0 and 1, got %v", opacity)
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

// WithFaintFactor sets how far faint foreground colors blend toward the background.
func WithFaintFactor(factor float64) Option {
	return func(d *Renderer) error {
		if factor < 0 || factor > 1 {
			return fmt.Errorf("still: faint factor must be between 0 and 1, got %v", factor)
		}
		d.opts.FaintFactor = factor
		return nil
	}
}

// WithFocusDimming sets the unfocused dimming overlay opacity in the range [0,1].
func WithFocusDimming(factor float64) Option {
	return func(d *Renderer) error {
		if factor < 0 || factor > 1 {
			return fmt.Errorf("still: focus dimming must be between 0 and 1, got %v", factor)
		}
		d.opts.FocusDimming = factor
		return nil
	}
}

// WithCursorBlinkSpeed sets the blink speed for the cursor when blinking is enabled.
func WithCursorBlinkSpeed(speed time.Duration) Option {
	return func(d *Renderer) error {
		if speed <= 0 {
			return fmt.Errorf("still: cursor blink speed must be > 0, got %v", speed)
		}
		d.opts.CursorBlinkSpeed = speed
		return nil
	}
}

// WithNow sets the clock used for blink phase calculation.
func WithNow(now func() time.Time) Option {
	return func(d *Renderer) error {
		if now == nil {
			return errors.New("still: now function must not be nil")
		}
		d.opts.Now = now
		return nil
	}
}

// WithCellBgDrawer sets the cell background drawer for the first cell pass.
func WithCellBgDrawer(drawer CellDrawer) Option {
	return func(d *Renderer) error {
		if drawer == nil {
			d.hooks.cellBgDrawer = DrawCellBg
		} else {
			d.hooks.cellBgDrawer = drawer
		}
		return nil
	}
}

// WithCellFgDrawer sets the cell foreground drawer for the second cell pass.
func WithCellFgDrawer(drawer CellDrawer) Option {
	return func(d *Renderer) error {
		if drawer == nil {
			d.hooks.cellFgDrawer = DrawCellFg
		} else {
			d.hooks.cellFgDrawer = drawer
		}
		return nil
	}
}

// WithCursorDrawer sets the cursor drawer to be used by the renderer.
func WithCursorDrawer(drawer CursorDrawer) Option {
	return func(d *Renderer) error {
		if drawer == nil {
			d.hooks.cursorDrawer = DrawCursor
		} else {
			d.hooks.cursorDrawer = drawer
		}
		return nil
	}
}

// WithScrollbarDrawer sets the scrollbar drawer to be used by the renderer.
func WithScrollbarDrawer(drawer ScrollbarDrawer) Option {
	return func(d *Renderer) error {
		if drawer == nil {
			d.hooks.scrollbarDrawer = DrawScrollbar
		} else {
			d.hooks.scrollbarDrawer = drawer
		}
		return nil
	}
}

// WithBackgroundDrawer sets the background drawer to be used by the renderer.
func WithBackgroundDrawer(drawer BackgroundDrawer) Option {
	return func(d *Renderer) error {
		if drawer == nil {
			d.hooks.backgroundDrawer = DrawBackground
		} else {
			d.hooks.backgroundDrawer = drawer
		}
		return nil
	}
}
