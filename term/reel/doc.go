// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package reel emulates terminal programs and turns what they draw into
// images and video. The module is split into packages that can be used on
// their own:
//
//   - vt is a terminal emulator backed by a checked-in libghostty-vt Wasm
//     build.
//   - vt/screen is the cell buffer and terminal state an emulator fills and a
//     renderer reads. It does not link the Wasm runtime.
//   - render rasterizes a screen into an image: fonts, colors, window chrome,
//     cursor, and progress and status indicators. Its fonts, types, and units
//     subpackages hold the bundled fonts and shared option types.
//   - sink encodes frames to PNG and GIF, and through ffmpeg to MP4, WebM,
//     and animated WebP.
//
// Apart from internal test helpers, no package in the module imports testing,
// Bubble Tea, or steep, so any program can link them. This package has no API
// yet; a recording API that ties the emulator, renderer, and sinks together is
// planned here.
package reel
