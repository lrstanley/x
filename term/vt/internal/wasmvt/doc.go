// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package wasmvt is the host side of the checked-in libghostty-vt module.
//
// vt.generated.go is that module translated by wasm2go
// (github.com/ncruces/wasm2go). The translator emits one Go method per
// Wasm export and does not know the C ABI those exports implement. This
// package is the hand-written half: linear memory, the type manifest,
// opaque constructors, and GhosttyResult. Terminal behavior stays in
// package vt.
//
// # Updating Ghostty
//
// The Wasm module is built from one Ghostty commit. [GhosttyRev] must
// match GHOSTTY_REV in term/vt/build/Dockerfile. Rebuild with
// `task -d term/vt generate` after changing the pin. That rebuilds the
// module and regenerates abi.gen.go from ghostty_type_json. Struct
// layouts are not stable across commits. Use the generated constants.
//
// WebAssembly utilities (alloc, free, opaque slots, memory growth):
//
//	https://github.com/ghostty-org/ghostty/blob/e3ebb1d1bf772cf5d1bc70b5ae19e6943e33a110/include/ghostty/vt/wasm.h
//
// Implementation of those exports:
//
//	https://github.com/ghostty-org/ghostty/blob/e3ebb1d1bf772cf5d1bc70b5ae19e6943e33a110/src/lib/allocator/wasm.zig
//
// Shared types, GhosttyResult, GhosttyString, and ghostty_type_json:
//
//	https://github.com/ghostty-org/ghostty/blob/e3ebb1d1bf772cf5d1bc70b5ae19e6943e33a110/include/ghostty/vt/types.h
//
// JSON schema for that manifest:
//
//	https://github.com/ghostty-org/ghostty/blob/e3ebb1d1bf772cf5d1bc70b5ae19e6943e33a110/src/terminal/c/types.schema.json
//
// Terminal options and queries (ghostty_terminal_set, ghostty_terminal_get):
//
//	https://github.com/ghostty-org/ghostty/blob/e3ebb1d1bf772cf5d1bc70b5ae19e6943e33a110/include/ghostty/vt/terminal.h
//
// # Memory
//
// One [Module] is one Wasm instance and one concurrency domain. Its
// methods are not safe for concurrent use. [Module.Alloc] and most
// exports can grow linear memory. Numeric pointers and handles stay
// valid, but a slice from [Module.Memory] or [Module.Slice] does not.
// Re-read memory immediately before the next host access. wasm.h
// ("Memory growth") is the upstream description of that rule.
//
// # ABI
//
// This host assumes the pinned wasm32-freestanding build: little-endian,
// 4-byte pointers, and 4-byte size_t. cmd/genabi refuses to emit constants
// for any other target. A rebuild for another target needs new load and
// store code as well as a generator change.
//
// Sized C structs (anything initialized with GHOSTTY_INIT_SIZED) store
// their byte size in the first field. Callers that pass one by pointer
// write that size before the call. The manifest field list is authoritative
// for the offset. On this target it is 0.
package wasmvt

// GhosttyRev is the Ghostty commit these helpers and the doc links were
// written against. It must match GHOSTTY_REV in term/vt/build/Dockerfile.
const GhosttyRev = "e3ebb1d1bf772cf5d1bc70b5ae19e6943e33a110"
