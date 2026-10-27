// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

// Package render rasterizes [screen.Screen] buffers into images for
// tests, docs, and GIF generation. Rendering follows Ghostty-aligned font
// metrics, palette resolution, and terminal chrome (cursor, scrollbar, focus).
//
// Use [New] for standalone rendering. When exercising Bubble Tea models
// through steep, prefer [github.com/lrstanley/x/term/steep.Harness.Image].
//
//	d, err := render.New(
//		render.WithScale(1.25),
//		render.WithPadding(8),
//	)
//	cols, rows := scr.Bounds().Dx(), scr.Bounds().Dy()
//	img := image.NewNRGBA(d.Layout(cols, rows).Image)
//	d.Draw(img, scr, screen.State{Focused: true}, 0)
//
// Each draw takes the [screen.State] around the grid, which the emulator
// fills, and an offset into the timeline that blinking and animated parts of
// the image take their phase from. The zero state draws an unfocused window
// in the renderer's default colors. [Renderer.Draw] copies into an image the
// caller owns, so its pixels outlive the call; the renderer keeps its own
// copy of the last image, and repaints only what changed since.
//
// [Renderer] owns the full draw pipeline: window background, optional title bar
// ([WithWindowChrome], showing [screen.State.Title]), cell backgrounds, cell
// foregrounds (glyphs and decorations), cursor, OSC 9;4 progress bar
// ([WithProgressBar], from [screen.State.ShownProgress]), scrollbar gutter,
// unfocused dimming overlay, an antialiased rounding mask on the terminal
// window, then the OSC 7501 program status outline around it.
//
// OSC 7501 program status ([WithProgramStatus], from
// [screen.State.ProgramStatuses]) is drawn only with window chrome. The most
// urgent record (see [screen.ProgramState]) replaces the title with its icon,
// spinner, or percent, title, and message, and colors a thin outline just
// outside the window edge. The other records show as dots after the title.
// Title bar text, the window title included, is drawn as plain text with
// controls, bidi overrides, and invisible characters removed.
// [Renderer.Animates], [Renderer.Changes], and [Renderer.Repaint] let a
// recorder keep an animated progress bar or program status moving while the
// screen holds still.
//
// Each glyph is drawn from the first font that actually has it: a
// [WithCodepointMap] range (by default, Nerd Font, Powerline, and symbol ranges
// map to the bundled Symbols Nerd Font), then the primary family
// ([WithFontFamily]), then a bundled symbol subset of DejaVu Sans Mono, then an
// installed font ([WithSystemFontFallback]). Block elements, Braille, and the
// Legacy Computing block mosaics are drawn from the cell geometry instead, so
// neighboring cells tile exactly.
//
// Emoji in wide cells are drawn in full color, scaled into the cell. With
// [WithSystemFontFallback] enabled an installed color font (Noto Color Emoji,
// Apple Color Emoji, Segoe UI Emoji) is tried first; otherwise, or when it does
// not have the emoji, a bundled subset of Noto Color Emoji draws it. That subset
// has no skin tones, so those are drawn in the default tone.
//
// See the Ghostty configuration reference for the closest user-facing
// knobs: https://ghostty.org/docs/config/reference
package render
