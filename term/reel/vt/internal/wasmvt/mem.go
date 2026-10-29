// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package wasmvt

import (
	"encoding/binary"
	"fmt"
	"math"
)

// This file is the host view of Wasm linear memory.
//
// ghostty_wasm_alloc and ghostty_wasm_free own the bytes. The typed
// loads below only interpret them. Addresses are Wasm i32 pointers,
// read as unsigned byte offsets (a negative i32 is a high address).
// Multi-byte values are little-endian, which is abi.endian for the
// pinned wasm32 build. An out-of-range read returns the zero value.
// An out-of-range write is ignored.
//
// Upstream: ghostty_wasm_alloc and ghostty_wasm_free in
// include/ghostty/vt/wasm.h, implemented by src/lib/allocator/wasm.zig.
// The pinned URLs are in the package comment.

// Alloc reserves n bytes of caller-owned Wasm linear memory for a
// scratch buffer or an ABI value.
//
// The address is aligned to abi.max_alignment from ghostty_type_json.
// The bytes are uninitialized. n <= 0 returns a zero pointer and a nil
// error, matching ghostty_wasm_alloc's NULL for a zero length, and does
// not enter the module. A failed allocation returns an error.
//
// Release the pointer with [Module.Free] and the same n. Alloc can grow
// linear memory, so any slice taken from [Module.Memory] or [Module.Slice]
// before this call is stale.
func (m *Module) Alloc(n int) (int32, error) {
	if n <= 0 {
		return 0, nil
	}
	ptr := m.Xghostty_wasm_alloc(Int32(n))
	if ptr == 0 {
		return 0, fmt.Errorf("vt: alloc %d bytes", n)
	}
	return ptr, nil
}

// Free releases storage from [Module.Alloc].
//
// ptr 0 and n <= 0 are ignored, matching ghostty_wasm_free. n must be
// the length passed to Alloc. This is a different family from buffers
// libghostty-vt itself allocates: those use ghostty_free, and opaque
// handles use their type-specific destructor. wasm.h forbids mixing them.
func (m *Module) Free(ptr int32, n int) {
	if ptr == 0 || n <= 0 {
		return
	}
	m.Xghostty_wasm_free(ptr, Int32(n))
}

// Slice returns n bytes of linear memory at ptr.
//
// The slice aliases the module heap. It is invalid after any call that
// can grow memory, including [Module.Alloc] and most libghostty-vt
// exports. Copy bytes out (append([]byte(nil), s...)) when they must
// outlive the next call. A zero pointer, a non-positive length, or a
// range outside the current memory returns nil.
func (m *Module) Slice(ptr int32, n int) []byte {
	if ptr == 0 || n <= 0 {
		return nil
	}
	mem := m.Memory()
	start := int(uint32(ptr)) //nolint:gosec // G115: wasm byte offset, not a numeric cast
	end := start + n
	if start < 0 || end > len(mem) {
		return nil
	}
	return mem[start:end]
}

// Put copies b into linear memory at ptr.
//
// A destination shorter than b, including a nil slice from [Module.Slice],
// receives only the bytes that fit. Put does not allocate and does not grow
// memory.
func (m *Module) Put(ptr int32, b []byte) {
	dst := m.Slice(ptr, len(b))
	copy(dst, b)
}

// Zero clears n bytes at ptr.
//
// A range [Module.Slice] rejects is left untouched.
func (m *Module) Zero(ptr int32, n int) {
	clear(m.Slice(ptr, n))
}

// view returns n bytes at ptr+off, or nil when that range is outside
// the current linear memory.
func (m *Module) view(ptr int32, off, n int) []byte {
	b := m.Slice(ptr, off+n)
	if len(b) < off+n {
		return nil
	}
	return b[off:]
}

// U8 reads the byte at ptr+off. An out-of-range address reads as zero.
func (m *Module) U8(ptr int32, off int) byte {
	b := m.view(ptr, off, 1)
	if len(b) == 0 {
		return 0
	}
	return b[0]
}

// I8 reads the byte at ptr+off as a signed int8, as a C int8_t field
// holds it. An out-of-range address reads as zero.
func (m *Module) I8(ptr int32, off int) int8 {
	return int8(m.U8(ptr, off)) //nolint:gosec // G115: bitcast of an int8_t
}

// SetU8 writes v at ptr+off. An out-of-range address is ignored.
func (m *Module) SetU8(ptr int32, off int, v byte) {
	b := m.view(ptr, off, 1)
	if len(b) > 0 {
		b[0] = v
	}
}

// U16 reads a little-endian uint16 at ptr+off.
// An out-of-range address reads as zero.
func (m *Module) U16(ptr int32, off int) uint16 {
	b := m.view(ptr, off, 2)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint16(b)
}

// SetU16 writes a little-endian uint16 at ptr+off.
// An out-of-range address is ignored.
func (m *Module) SetU16(ptr int32, off int, v uint16) {
	b := m.view(ptr, off, 2)
	if b != nil {
		binary.LittleEndian.PutUint16(b, v)
	}
}

// U32 reads a little-endian uint32 at ptr+off.
//
// Wasm pointers and size_t are 4 bytes on the pinned build, so pointer
// fields and lengths use this width. An out-of-range address reads as zero.
func (m *Module) U32(ptr int32, off int) uint32 {
	b := m.view(ptr, off, 4)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint32(b)
}

// SetU32 writes a little-endian uint32 at ptr+off.
// An out-of-range address is ignored.
func (m *Module) SetU32(ptr int32, off int, v uint32) {
	b := m.view(ptr, off, 4)
	if b != nil {
		binary.LittleEndian.PutUint32(b, v)
	}
}

// I32 reads a little-endian int32 at ptr+off.
//
// Ghostty enums and GhosttyResult use this width (c_int on the Zig
// side, a 4-byte integer in the manifest). An out-of-range address
// reads as zero.
func (m *Module) I32(ptr int32, off int) int32 {
	return AsI32(m.U32(ptr, off))
}

// SetI32 writes a little-endian int32 at ptr+off.
// An out-of-range address is ignored.
func (m *Module) SetI32(ptr int32, off int, v int32) {
	m.SetU32(ptr, off, asU32(v))
}

// U64 reads a little-endian uint64 at ptr+off.
// An out-of-range address reads as zero.
func (m *Module) U64(ptr int32, off int) uint64 {
	b := m.view(ptr, off, 8)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint64(b)
}

// Int32 converts a host int to a wasm i32: a length (the pinned build's
// size_t is 4 bytes), an offset, or a terminal size. Callers must pass a
// value that fits.
func Int32(n int) int32 {
	return int32(n) //nolint:gosec // G115: the caller's value fits a wasm i32
}

// Uint32 converts a host int to an unsigned 32-bit field, such as a count
// or a grid row. Callers must pass a value that fits.
func Uint32(n int) uint32 {
	return uint32(n) //nolint:gosec // G115: the caller's value fits a u32 field
}

// Uint16 converts a host int to an unsigned 16-bit field, such as a grid
// column or a terminal size. Callers must pass a value that fits.
func Uint16(n int) uint16 {
	return uint16(n) //nolint:gosec // G115: the caller's value fits a u16 field
}

// AsI32 bitcasts a wasm i32 that was read as a uint32.
func AsI32(v uint32) int32 {
	return int32(v) //nolint:gosec // G115: bitcast of a wasm i32
}

// asU32 bitcasts a wasm i32 into the uint32 load/store helpers.
func asU32(v int32) uint32 {
	return uint32(v) //nolint:gosec // G115: bitcast of a wasm i32
}

// SetF32 writes v as an IEEE-754 binary32 at ptr+off, little-endian.
//
// Use this only for fields the manifest sizes as 4 bytes. Ghostty's
// GhosttySurfacePosition members are C doubles (8 bytes each). See
// include/ghostty/vt/types.h. An out-of-range address is ignored.
func (m *Module) SetF32(ptr int32, off int, v float32) {
	m.SetU32(ptr, off, math.Float32bits(v))
}
