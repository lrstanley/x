// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package vt

import (
	"encoding/binary"
	"unsafe"
)

// C layout mirrors (libghostty-vt). Verified in tests against
// ghostty_style_default / ghostty_style_is_default when the library is present.

type ghosttyTerminalOptions struct {
	Cols          uint16
	Rows          uint16
	MaxScrollback uintptr
}

type ghosttyPoint struct {
	Tag   int32
	_     uint32
	Union [16]byte
}

func ghosttyPointActive(x uint16, y uint32) ghosttyPoint {
	var p ghosttyPoint
	p.Tag = ghosttyPointTagActive
	binary.LittleEndian.PutUint16(p.Union[0:2], x)
	binary.LittleEndian.PutUint32(p.Union[4:8], y)
	return p
}

type ghosttyGridRef struct {
	Size uintptr
	Node unsafe.Pointer
	X    uint16
	Y    uint16
	_    uint32
}

type ghosttyColorRGB struct {
	R uint8
	G uint8
	B uint8
}

type ghosttyStyleColor struct {
	Tag   int32
	_     uint32
	Value uint64
}

type ghosttyStyle struct {
	Size           uintptr
	Fg             ghosttyStyleColor
	Bg             ghosttyStyleColor
	UnderlineColor ghosttyStyleColor
	Bold           bool
	Italic         bool
	Faint          bool
	Blink          bool
	Inverse        bool
	Invisible      bool
	Strikethrough  bool
	Overline       bool
	Underline      int32
}

const (
	sizeofGhosttyTerminalOptions = unsafe.Sizeof(ghosttyTerminalOptions{})
	sizeofGhosttyPoint           = unsafe.Sizeof(ghosttyPoint{})
	sizeofGhosttyGridRef         = unsafe.Sizeof(ghosttyGridRef{})
	sizeofGhosttyStyle           = unsafe.Sizeof(ghosttyStyle{})
)
