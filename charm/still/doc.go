// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package still rasterizes [ultraviolet.Screen] buffers into images for
// tests, docs, and GIF generation. Rendering follows Ghostty-aligned font
// metrics, palette resolution, and terminal chrome (cursor, scrollbar, focus).
//
// Use [New] for standalone rendering. When exercising Bubble Tea models
// through steep, prefer [github.com/lrstanley/x/charm/steep.Harness.Image].
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
// Default rendering composes, in order: window background, cell backgrounds,
// cell foregrounds (glyphs and decorations), cursor, scrollbar gutter,
// unfocused dimming overlay, then an antialiased rounding mask on the terminal
// window. See the Ghostty configuration reference for the closest user-facing
// knobs: https://ghostty.org/docs/config/reference
package still
