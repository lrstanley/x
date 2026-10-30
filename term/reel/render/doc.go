// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

// Package render rasterizes [vt.Screen] buffers into images for
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
//	d.Draw(img, scr, vt.State{Focused: true}, 0)
//
// To target a pixel size, [Renderer.Fit] returns the most columns and rows
// that fit within it, and [FitScale] the largest [WithScale] at which a given
// screen does; [ScaleForFontSize] converts a font size in pixels to a scale.
//
// Each draw takes the [vt.State] around the grid, which the emulator
// fills, and an offset into the timeline that blinking and animated parts of
// the image take their phase from. The zero state draws an unfocused window
// in the renderer's default colors. [Renderer.Draw] copies into an image the
// caller owns, so its pixels outlive the call; the renderer keeps its own
// copy of the last image, and repaints only what changed since.
//
// [Renderer] owns the full draw pipeline: window background, optional title bar
// ([WithWindowChrome], showing [vt.State.Title] and
// [vt.State.WorkingDirectory]), cell backgrounds, cell foregrounds (glyphs
// and decorations, with an underline under OSC 8 links when
// [WithLinkUnderline] is set), exit status badges on the rows of OSC 133
// prompts whose command failed ([WithPromptMarks], from
// [vt.State.PromptMarks]), cursor, OSC 9;4 progress bar ([WithProgressBar],
// from [vt.State.ShownProgress]), scrollbar gutter (with a tick per prompt),
// unfocused dimming overlay, the bell flash ([WithBell], from
// [vt.State.Bell]), an antialiased rounding mask on the terminal window, then
// the OSC 7501 program status outline around it.
//
// Kitty graphics ([vt.Buffer.Images]) are drawn in their layers: under the
// cell backgrounds, between the backgrounds and the text, or over the text.
// Each is scaled from the terminal's cells to the renderer's, so an image
// covers the same cells at any font size. Unicode placeholder cells draw
// their image instead of their text.
//
// OSC 7501 program status ([WithProgramStatus], from
// [vt.State.ProgramStatuses]) is drawn only with window chrome. The most
// urgent record (see [vt.ProgramState]) replaces the title with its icon,
// spinner, or percent, title, and message, and colors a thin outline just
// outside the window edge. The other records show as dots after the title.
// Title bar text, the window title and working directory included, is drawn
// as plain text with controls, bidi overrides, and invisible characters
// removed.
// [Renderer.Animates], [Renderer.Changes], and [Renderer.Repaint] let a
// recorder keep an animated progress bar, program status, or bell moving
// while the screen holds still.
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
