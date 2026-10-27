// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package screen is a cell grid and the terminal state drawn around it.
//
// It has no terminal parser and no Wasm runtime. A [Screen] is a rectangle
// of [Cell] values: grapheme, width, colors, attribute bits, underline, and
// an optional link. Attribute bit positions are stable. A [State] is the
// rest of what a renderer needs to draw a screen: the title, focus, cursor,
// default colors, scrollback, OSC 9;4 progress, and OSC 7501 program status.
// The emulator fills both, and a renderer reads them without importing it.
package screen
