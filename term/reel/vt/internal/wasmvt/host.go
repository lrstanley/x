// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package wasmvt

// Memory returns the module's linear memory.
//
// The slice aliases the heap. It is invalid after any call that can grow
// memory, including [Module.Alloc] and most libghostty-vt exports.
// Pointers and handles remain valid. Re-read Memory before the next
// access. wasm.h ("Memory growth") documents the same rule for JS hosts.
func (m *Module) Memory() []byte { return m.memory }

// Table returns the indirect function table.
//
// Index 0 is nil. wasm2go fills the following indexes with generated
// functions. [Module.Bind] appends host functions. The slice aliases the
// table. Do not retain it across Bind.
func (m *Module) Table() []any { return m.t0 }

// Bind appends fn to the indirect function table and returns its index.
//
// libghostty-vt callback options take a C function pointer. In this
// wasm32 module that pointer is a table index. Pass the index straight
// to [Module.SetOption]. It is not a pointer to the index.
//
// fn's Go type must match the call_indirect signature Ghostty will use.
// The C typedefs are the GhosttyTerminal*Fn types in
// include/ghostty/vt/terminal.h ("Effects"). A mismatch panics inside
// the generated call, not in Bind. Bind while the module is otherwise
// idle: the table is not safe for concurrent use.
func (m *Module) Bind(fn any) int32 {
	m.t0 = append(m.t0, fn)
	return Int32(len(m.t0) - 1)
}
