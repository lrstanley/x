// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package screen is a cell grid.
//
// It has no terminal parser and no Wasm runtime. A [Screen] is a rectangle
// of [Cell] values: grapheme, width, colors, attribute bits, underline, and
// an optional link. Attribute bit positions are stable.
package screen
