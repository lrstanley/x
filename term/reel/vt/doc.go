// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package vt is a terminal emulator backed by a checked-in libghostty-vt
// build. One WASM module is one concurrency domain. The caller owns a
// [Runtime] and lends instances from it. [NewRuntime] with a size of 0
// uses GOMAXPROCS, and a negative size sets no limit. The generated runtime
// builds with the standard Go toolchain.
//
// Linear memory, the C type manifest, and GhosttyResult live in
// [github.com/lrstanley/x/term/reel/vt/internal/wasmvt]. This package turns
// those calls into terminal operations.
//
// The emulator fills a cell grid and the terminal state around it, which a
// renderer reads. A [Screen] is a rectangle of [Cell] values: grapheme,
// width, colors, attribute bits, underline, and an optional link. Attribute
// bit positions are stable. A [Buffer] also carries the screen's Kitty
// graphics placements ([Buffer.Images]), measured in the terminal's cell
// pixels ([WithCellSize]). A [State] is the rest of what a renderer needs
// to draw a screen: the title, focus, cursor, default colors, reverse
// video, the palette colors the program set, the glyphs it registered with
// the Glyph Protocol, scrollback, OSC 9;4 progress, OSC 7501 program status,
// and the program's events, such as the bell, stamped on the terminal's clock
// ([WithClock]).
package vt
