// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package vt is a terminal emulator backed by a checked-in libghostty-vt
// build. One WASM module is one concurrency domain. The caller owns a
// [Runtime] and lends instances from it. [NewRuntime] with a size less
// than 1 uses GOMAXPROCS. The generated runtime builds with the standard
// Go toolchain.
//
// Linear memory, the C type manifest, and GhosttyResult live in
// [github.com/lrstanley/x/term/reel/vt/internal/wasmvt]. This package turns
// those calls into terminal operations.
//
// The cell grid lives in [github.com/lrstanley/x/term/reel/vt/screen] and does
// not link the WASM runtime. Drawers import that package alone.
package vt
