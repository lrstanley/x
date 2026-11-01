// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package look is how a recording looks: its theme, font, frame, and
// playback, as both a tape's Set commands and the CLI's flags give it.
package look

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/cmd/reel/internal/themes"
	"github.com/lrstanley/x/term/reel/effects"
	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/reel/render/fonts"
)

// Look is how a recording looks. A nil field is unset, so that layers of
// it can be merged (see [Look.Merge]): a tape's settings, then the
// environment and flags over them. Unset fields take reel's defaults.
//
// The kong tags make it the CLI's flags; each tape setting of the same
// meaning is named in its help.
type Look struct {
	Theme         *string  `name:"theme" env:"REEL_THEME" placeholder:"NAME|FILE|JSON" help:"Theme: a name from \"reel themes\", a file of xterm.js theme JSON, or that JSON (tape: Theme)."`
	FontFamily    *string  `name:"font" env:"REEL_FONT" placeholder:"FAMILY[,FAMILY...]" help:"Font family; the first installed one of a comma-separated list (tape: FontFamily)."`
	FontSize      *float64 `name:"font-size" env:"REEL_FONT_SIZE" placeholder:"PX" help:"Font size in pixels (tape: FontSize)."`
	LetterSpacing *float64 `name:"letter-spacing" placeholder:"PX" help:"Extra pixels between letters (tape: LetterSpacing)."`
	LineHeight    *float64 `name:"line-height" placeholder:"X" help:"Line height, as a multiple of the font's (tape: LineHeight)."`
	Padding       *int     `name:"padding" env:"REEL_PADDING" placeholder:"PX" help:"Padding around the terminal, in pixels (tape: Padding)."`
	Margin        *int     `name:"margin" env:"REEL_MARGIN" placeholder:"PX" help:"Margin around the window, in pixels (tape: Margin)."`
	MarginFill    *string  `name:"margin-fill" env:"REEL_MARGIN_FILL" placeholder:"#RRGGBB" help:"Margin color; defaults to the theme's background (tape: MarginFill)."`
	BorderRadius  *int     `name:"radius" env:"REEL_RADIUS" placeholder:"PX" help:"Window corner radius, in pixels (tape: BorderRadius)."`
	WindowBar     *bool    `name:"chrome" env:"REEL_CHROME" negatable:"" help:"Draw a window bar (tape: WindowBar)."`
	Scrollbar     *bool    `name:"scrollbar" negatable:"" help:"Draw a scrollbar (tape: Scrollbar)."`
	PromptMarks   *bool    `name:"prompt-marks" negatable:"" help:"Draw OSC 133 prompt marks (tape: PromptMarks)."`
	LinkUnderline *bool    `name:"link-underline" negatable:"" help:"Underline OSC 8 hyperlinks (tape: LinkUnderline)."`
	Bell          *bool    `name:"bell" negatable:"" help:"Flash the bell (tape: Bell)."`
	FPS           *int     `name:"fps" env:"REEL_FPS" placeholder:"N" help:"Frame rate; reel's default is 20 (tape: Framerate)."`
	Speed         *float64 `name:"speed" env:"REEL_SPEED" placeholder:"X" help:"Playback speed: 2 plays twice as fast (tape: PlaybackSpeed)."`
	Title         *string  `name:"title" placeholder:"TEXT" help:"Recording title, for the window bar, intro card, and asciicast header (tape: Title)."`
	KeyOverlay    *bool    `name:"keys" env:"REEL_KEYS" negatable:"" help:"Overlay the keys pressed (tape: KeyOverlay)."`
	Toasts        *bool    `name:"toasts" env:"REEL_TOASTS" negatable:"" help:"Show notifications and clipboard writes as toasts (tape: Toasts)."`
}

// Merge returns l with the fields o sets replacing l's.
func (l Look) Merge(o Look) Look {
	l.Theme = cmp.Or(o.Theme, l.Theme)
	l.FontFamily = cmp.Or(o.FontFamily, l.FontFamily)
	l.FontSize = cmp.Or(o.FontSize, l.FontSize)
	l.LetterSpacing = cmp.Or(o.LetterSpacing, l.LetterSpacing)
	l.LineHeight = cmp.Or(o.LineHeight, l.LineHeight)
	l.Padding = cmp.Or(o.Padding, l.Padding)
	l.Margin = cmp.Or(o.Margin, l.Margin)
	l.MarginFill = cmp.Or(o.MarginFill, l.MarginFill)
	l.BorderRadius = cmp.Or(o.BorderRadius, l.BorderRadius)
	l.WindowBar = cmp.Or(o.WindowBar, l.WindowBar)
	l.Scrollbar = cmp.Or(o.Scrollbar, l.Scrollbar)
	l.PromptMarks = cmp.Or(o.PromptMarks, l.PromptMarks)
	l.LinkUnderline = cmp.Or(o.LinkUnderline, l.LinkUnderline)
	l.Bell = cmp.Or(o.Bell, l.Bell)
	l.FPS = cmp.Or(o.FPS, l.FPS)
	l.Speed = cmp.Or(o.Speed, l.Speed)
	l.Title = cmp.Or(o.Title, l.Title)
	l.KeyOverlay = cmp.Or(o.KeyOverlay, l.KeyOverlay)
	l.Toasts = cmp.Or(o.Toasts, l.Toasts)
	return l
}

// Validate reports a set field with a value no recording can have, and a
// theme or margin fill that does not resolve.
func (l Look) Validate() error {
	if l.Theme != nil {
		if _, err := themes.Resolve(*l.Theme); err != nil {
			return err
		}
	}
	if l.MarginFill != nil {
		if _, err := themes.ParseColor(*l.MarginFill); err != nil {
			return fmt.Errorf("margin fill: %w", err)
		}
	}
	switch {
	case l.FontSize != nil && *l.FontSize <= 0:
		return fmt.Errorf("font size must be above 0, got %v", *l.FontSize)
	case l.LineHeight != nil && *l.LineHeight <= 0:
		return fmt.Errorf("line height must be above 0, got %v", *l.LineHeight)
	case l.Speed != nil && *l.Speed <= 0:
		return fmt.Errorf("speed must be above 0, got %v", *l.Speed)
	case l.FPS != nil && *l.FPS < 1:
		return fmt.Errorf("fps must be at least 1, got %d", *l.FPS)
	}
	for name, v := range map[string]*int{"padding": l.Padding, "margin": l.Margin, "radius": l.BorderRadius} {
		if v != nil && *v < 0 {
			return fmt.Errorf("%s must not be negative, got %d", name, *v)
		}
	}
	return nil
}

// Palette returns the theme's palette, and whether a theme is set.
func (l Look) Palette() (pal render.Palette, themed bool, err error) {
	if l.Theme == nil {
		return pal, false, nil
	}
	pal, err = themes.Resolve(*l.Theme)
	return pal, err == nil, err
}

// Render returns the render options of l, drawn at fontSize, or at the
// renderer's default size when it is zero, and warnings about what it
// could only approximate.
func (l Look) Render(fontSize float64) (opts []render.Option, warnings []string, err error) {
	pal, themed, err := l.Palette()
	if err != nil {
		return nil, nil, err
	}
	if themed {
		opts = append(opts, render.WithPalette(pal))
	}
	if l.FontFamily != nil {
		if family, ok := firstFamily(*l.FontFamily); ok {
			opts = append(opts, render.WithFontFamily(family))
		} else {
			warnings = append(warnings, fmt.Sprintf("none of the fonts %q is installed; using the default", *l.FontFamily))
		}
	}
	if l.Padding != nil {
		opts = append(opts, render.WithPadding(*l.Padding))
	}
	if l.Margin != nil && *l.Margin > 0 {
		fill := pal.DefaultBackground
		if l.MarginFill != nil {
			if fill, err = themes.ParseColor(*l.MarginFill); err != nil {
				return nil, nil, fmt.Errorf("margin fill: %w", err)
			}
		}
		opts = append(opts, render.WithMargin(*l.Margin, fill))
	}
	if l.LineHeight != nil {
		opts = append(opts, render.WithCellHeight(*l.LineHeight-1))
	}
	if l.BorderRadius != nil {
		opts = append(opts, render.WithBorderRadius(*l.BorderRadius))
	}
	for _, b := range []struct {
		v   *bool
		opt func(bool) render.Option
	}{
		{l.WindowBar, render.WithWindowChrome},
		{l.Scrollbar, render.WithScrollbar},
		{l.PromptMarks, render.WithPromptMarks},
		{l.LinkUnderline, render.WithLinkUnderline},
		{l.Bell, render.WithBell},
	} {
		if b.v != nil {
			opts = append(opts, b.opt(*b.v))
		}
	}
	if fontSize > 0 {
		opts = append(opts, render.WithScale(render.ScaleForFontSize(fontSize)))
	}
	if l.LetterSpacing != nil && *l.LetterSpacing != 0 {
		// Letter spacing is in pixels, and the cell width adjustment a
		// fraction of the cell, so it needs the cell's width.
		r, renderErr := render.New(opts...)
		if renderErr != nil {
			return nil, nil, renderErr
		}
		cell := r.Metrics().CellSize()
		_ = r.Close()
		opts = append(opts, render.WithCellWidth(*l.LetterSpacing/float64(cell.X)))
	}
	return opts, warnings, nil
}

// Profile sets p's playback and title from l.
func (l Look) Profile(p *reel.Profile) {
	if l.FPS != nil {
		p.FPS = *l.FPS
	}
	if l.Speed != nil {
		p.Speed = *l.Speed
	}
	if l.Title != nil {
		p.Title = *l.Title
	}
}

// Effects returns the effects l turns on.
func (l Look) Effects() []reel.Effect {
	var fx []reel.Effect
	if l.KeyOverlay != nil && *l.KeyOverlay {
		fx = append(fx, effects.Keys(time.Second))
	}
	if l.Toasts != nil && *l.Toasts {
		fx = append(fx, effects.Toasts(effects.WithClipboardToasts()))
	}
	return fx
}

// Arrange orders effects gathered from several places as a profile needs
// them, keeping each group's order: [reel.Concealer] effects that conceal
// first, as reel requires, then [reel.Cutter] effects, so those after them,
// such as [effects.Keys], leave out what they cut, then the rest.
func Arrange(fx []reel.Effect) []reel.Effect {
	rank := func(e reel.Effect) int {
		for e != nil {
			if c, ok := e.(reel.Concealer); ok && c.Conceals() {
				return 0
			}
			if c, ok := e.(reel.Cutter); ok && c.Cuts() {
				return 1
			}
			u, ok := e.(interface{ Unwrap() reel.Effect })
			if !ok {
				break
			}
			e = u.Unwrap()
		}
		return 2
	}
	out := slices.Clone(fx)
	slices.SortStableFunc(out, func(a, b reel.Effect) int { return rank(a) - rank(b) })
	return out
}

// firstFamily loads the first installed family of a comma-separated list,
// as in CSS.
func firstFamily(list string) (fonts.FontFamily, bool) {
	for name := range strings.SplitSeq(list, ",") {
		name = strings.Trim(strings.TrimSpace(name), `"'`)
		if family, err := fonts.LoadFamily(name); err == nil {
			return family, true
		}
	}
	return fonts.FontFamily{}, false
}
