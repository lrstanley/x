// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

// Package still rasterizes [screen.Screen] buffers into images for
// tests, docs, and GIF generation. Rendering follows Ghostty-aligned font
// metrics, palette resolution, and terminal chrome (cursor, scrollbar, focus).
//
// Use [New] for standalone rendering. When exercising Bubble Tea models
// through steep, prefer [github.com/lrstanley/x/term/steep.Harness.Image].
//
//	d, err := still.New(
//		still.WithFontSizePt(still.Pt(14)),
//		still.WithPadding(8),
//	)
//	d.UpdateEmulatorState(&still.EmulatorState{Focused: true})
//	img := d.Draw(screen)
//
// The image returned from [Renderer.Draw] is owned by the renderer; encode or copy
// it before the next draw. Use [Renderer.DrawInto] when pixels must outlive the
// call.
//
// [Renderer] owns the full draw pipeline: window background, optional title bar
// ([WithWindowChrome], showing [EmulatorState.Title]), cell backgrounds, cell
// foregrounds (glyphs and decorations), cursor, OSC 9;4 progress bar
// ([WithProgressBar], from [EmulatorState.Progress]), scrollbar gutter,
// unfocused dimming overlay, an antialiased rounding mask on the terminal
// window, then the OSC 7501 program status outline around it.
//
// OSC 7501 program status ([WithProgramStatus], from
// [EmulatorState.ProgramStatuses]) is drawn only with window chrome. The most
// urgent record (see [ProgramState]) replaces the title with its icon,
// spinner, or percent, title, and message, and colors a thin outline just
// outside the window edge. The other records show as dots after the title.
// Title bar text, the window title included, is drawn as plain text with
// controls, bidi overrides, and invisible characters removed.
// [Renderer.IndicatorsAnimate], [Renderer.IndicatorsChange], and
// [Renderer.RepaintIndicators] let a recorder keep an animated progress bar or
// program status moving while the screen holds still.
//
// Each glyph is drawn from the first font that actually has it: a
// [WithCodepointMap] range (by default, Nerd Font, Powerline, and symbol ranges
// map to the bundled Symbols Nerd Font), then the primary family
// ([WithFontFamily]), then a bundled symbol subset of DejaVu Sans Mono, then an
// installed font ([WithSystemFontFallback]). Block elements, Braille, and the
// Legacy Computing block mosaics are drawn from the cell geometry instead, so
// neighboring cells tile exactly.
//
// See the Ghostty configuration reference for the closest user-facing
// knobs: https://ghostty.org/docs/config/reference
package still
