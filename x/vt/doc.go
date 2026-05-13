// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

// Package vt provides a virtual terminal backed by libghostty-vt (Ghostty's
// VT engine) using [github.com/go-webgpu/goffi] instead of cgo. The API is
// shaped to mirror the ergonomics of [github.com/charmbracelet/x/vt] for the
// subset that is implemented today.
//
// # Native library
//
// This package dlopens libghostty-vt at runtime. Set LIBGHOSTTY_VT to an
// absolute path to the shared library if it is not discoverable via the
// default search order. Typical names are libghostty-vt.so.0 on Linux,
// libghostty-vt.*.dylib on macOS, and ghostty-vt.dll on Windows.
//
// # Stability
//
// Both the upstream C API and the struct layouts mirrored here are unstable.
// Pin the native library version you test against.
//
// # Race detector
//
// [github.com/go-webgpu/goffi] documents limitations around the Go race
// detector when cgo is disabled; keep that in mind when running -race on
// packages that import goffi.
package vt
