// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package wasmvt

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// This file applies the libghostty-vt C ABI. Layout numbers live in
// abi.gen.go, produced by cmd/genabi from ghostty_type_json. The host
// does not parse that document at runtime.
//
// GhosttyString and GhosttyResult are documented in
// include/ghostty/vt/types.h. Terminal get and set are documented in
// include/ghostty/vt/terminal.h. Opaque constructor slots are documented
// in include/ghostty/vt/wasm.h. Pinned URLs are in the package comment.

// ErrNoValue is GhosttyResult GHOSTTY_NO_VALUE ([ResultNoValue]).
// A read had nothing to return, such as a selection on an empty cell or
// a search with no matches. Package vt re-exports this sentinel.
var ErrNoValue = errors.New("no value")

// String copies the GhosttyString at ptr+off out of linear memory.
//
// GhosttyString is a borrowed pointer plus a length (types.h). The copy
// is required because the next allocating call can grow memory and
// invalidate [Module.Slice]. A null pointer or a non-positive length,
// including an out-of-range pointer, returns an empty string.
//
// Empty strings produced by the library have a non-NULL pointer. A
// Go empty string here does not preserve that distinction.
func (m *Module) String(ptr int32, off int) string {
	sp := AsI32(m.U32(ptr, off+StringPtrOff))
	ln := int(m.U32(ptr, off+StringLenOff))
	if sp == 0 || ln <= 0 {
		return ""
	}
	return string(append([]byte(nil), m.Slice(sp, ln)...))
}

// StoreString writes a GhosttyString header at dst+off.
//
// Only the pointer and length fields are written. src must already hold
// the bytes, and those bytes must stay allocated for as long as the
// library reads the header. StoreString does not copy them.
func (m *Module) StoreString(dst int32, off int, src int32, n int) {
	m.SetU32(dst, off+StringPtrOff, asU32(src))
	m.SetU32(dst, off+StringLenOff, uint32(n)) //nolint:gosec // G115: GhosttyString len is a wasm size_t
}

// Result turns a GhosttyResult into a Go error.
//
// Zero is GHOSTTY_SUCCESS. GHOSTTY_NO_VALUE becomes an error wrapping
// [ErrNoValue]. Every other code is returned as text, because callers
// distinguish some of them (OUT_OF_SPACE, INVALID_VALUE) by comparing
// the raw int32 with the generated GhosttyResult constants before they
// call Result.
//
// The error text keeps the "vt:" prefix. Package vt returns this error
// unchanged.
//
// Pinned header values, for reading logs. The generated constants are
// the source of truth:
//
//	SUCCESS 0
//	OUT_OF_MEMORY -1
//	INVALID_VALUE -2
//	OUT_OF_SPACE -3
//	NO_VALUE -4
//	IO_ERROR -5
//	LIMIT_EXCEEDED -6
//	REJECTED -7
//
// Upstream: the GhosttyResult enum in include/ghostty/vt/types.h.
func Result(op string, rc int32) error {
	if rc == 0 {
		return nil
	}
	if rc == ResultNoValue {
		return fmt.Errorf("vt: %s: %w", op, ErrNoValue)
	}
	return fmt.Errorf("vt: %s result %d", op, rc)
}

// Ctor runs an opaque-handle constructor and returns the handle.
//
// The sequence is the one in wasm.h:
//
//  1. ghostty_wasm_alloc_opaque allocates a slot initialized to NULL.
//  2. newFn writes the handle into that slot and returns a GhosttyResult.
//     Constructors take the slot as their out-parameter. Pass 0 as the
//     allocator argument to use the default allocator.
//  3. On success, ghostty_wasm_take_opaque copies the handle out and
//     resets the slot. The slot storage is then freed. The handle is not.
//
// The caller owns the handle and releases it with the type-specific
// destructor (ghostty_terminal_free, ghostty_search_free, and so on),
// not with [Module.Free]. A failed constructor frees only the slot.
func (m *Module) Ctor(newFn func(slot int32) int32) (int32, error) {
	slot := m.Xghostty_wasm_alloc_opaque()
	if slot == 0 {
		return 0, errors.New("vt: alloc opaque slot")
	}
	defer m.Xghostty_wasm_free_opaque(slot)
	if err := Result("constructor", newFn(slot)); err != nil {
		return 0, err
	}
	handle := m.Xghostty_wasm_take_opaque(slot)
	if handle == 0 {
		return 0, errors.New("vt: empty opaque handle")
	}
	return handle, nil
}

// SetOption calls ghostty_terminal_set.
//
// opt is a GhosttyTerminalOption value, such as
// [TerminalOptionWritePty] or
// [TerminalOptionScrollbackMaxLines]. value is the raw second
// argument:
//
//   - Pointer-typed options (callbacks, userdata) take the pointer
//     itself. A callback pointer in this module is a table index from
//     [Module.Bind], passed directly, not a pointer to the index.
//   - Every other option takes a pointer to the value. Allocate that
//     storage with [Module.Alloc]. The library reads it during the call.
//   - 0 clears the option. The header documents the NULL behavior on
//     each GhosttyTerminalOption.
//
// Callbacks run synchronously inside VT writes and must not re-enter
// ghostty_terminal_vt_write on the same terminal. terminal.h ("Effects")
// is the list of callback typedefs.
func (m *Module) SetOption(term, opt, value int32) error {
	return Result("terminal set", m.Xghostty_terminal_set(term, opt, value))
}

// setBytes points opt at a temporary copy of buf for the duration of
// ghostty_terminal_set. The copy is freed before setBytes returns, so
// the option must consume the value during the call.
func (m *Module) setBytes(term, opt int32, buf []byte) error {
	ptr, err := m.Alloc(len(buf))
	if err != nil {
		return err
	}
	defer m.Free(ptr, len(buf))
	m.Put(ptr, buf)
	return m.SetOption(term, opt, ptr)
}

// SetU32Option sets a 4-byte integer option.
//
// The option's C type must be a 4-byte integer. Wider fields, including
// size_t on a future 64-bit target, need their own writer.
func (m *Module) SetU32Option(term, opt int32, v uint32) error {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], v)
	return m.setBytes(term, opt, buf[:])
}

// SetU8Option sets a 1-byte integer option.
func (m *Module) SetU8Option(term, opt int32, v byte) error {
	return m.setBytes(term, opt, []byte{v})
}

// SetBoolOption sets a bool option as a single byte, 0 or 1.
//
// That is how this wasm32 ABI passes a C bool by pointer. It is not a
// GhosttyResult and it is not an enum.
func (m *Module) SetBoolOption(term, opt int32, on bool) error {
	v := byte(0)
	if on {
		v = 1
	}
	return m.SetU8Option(term, opt, v)
}

// Get calls ghostty_terminal_get.
//
// data is a GhosttyTerminalData value, such as [TerminalDataMode]
// or [TerminalDataCursorX]. out must point at the output type
// documented for that value. On success the library writes through out.
// GHOSTTY_NO_VALUE is [ErrNoValue].
func (m *Module) Get(term, data, out int32) error {
	return Result("terminal get", m.Xghostty_terminal_get(term, data, out))
}

// Call2 invokes table index idx as a function of two i32 arguments.
//
// Index 0 is the Wasm null function pointer and is ignored. Ghostty
// stores reply callbacks (clipboard write and read) as function pointers
// inside the request struct. On this module those pointers are table
// indexes of generated Wasm functions, which the host calls to complete
// the request. A Go func from [Module.Bind] is invoked when it has this
// signature. Any other dynamic type is ignored.
//
// idx must be inside the table. An out-of-range index panics.
// Generated callees restore the Wasm stack before returning, so the
// host does not save it.
func (m *Module) Call2(idx, a, b int32) {
	if idx == 0 {
		return
	}
	fn, ok := m.t0[idx].(func(int32, int32))
	if !ok {
		return
	}
	fn(a, b)
}
