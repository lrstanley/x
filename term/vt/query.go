// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"image/color"
	"slices"

	"github.com/lrstanley/x/term/vt/internal/wasmvt"
	"github.com/lrstanley/x/term/vt/screen"
)

func (t *Terminal) modeLocked(mode Mode) (bool, error) {
	on, ok, err := t.modeKnownLocked(mode)
	if err != nil || !ok {
		return false, err
	}
	return on, nil
}

// modeKnownLocked queries TerminalDataMode. The key is a
// GhosttyTerminalModeConfig whose first u16 is packMode. INVALID_VALUE
// and NO_VALUE mean the mode number is not recognized, which is ok
// false rather than an error. The value byte is 0 or 1.
func (t *Terminal) modeKnownLocked(mode Mode) (on, ok bool, err error) {
	n := wasmvt.SizeTerminalModeConfig
	t.mod.Zero(t.scratch, n)
	t.mod.SetU16(t.scratch, 0, packMode(mode))
	rc := t.mod.Xghostty_terminal_get(t.term, wasmvt.TerminalDataMode, t.scratch)
	if rc == wasmvt.ResultInvalidValue || rc == wasmvt.ResultNoValue {
		return false, false, nil
	}
	if err = wasmvt.Result("mode", rc); err != nil {
		return false, false, err
	}
	return t.mod.U8(t.scratch, wasmvt.TerminalModeConfigValueOff) != 0, true, nil
}

// setModeLocked sets TerminalOptionMode. Ghostty reads the
// GhosttyTerminalModeConfig during the call, so the allocation can
// be freed immediately after SetOption returns.
func (t *Terminal) setModeLocked(mode Mode, on bool) error {
	n := wasmvt.SizeTerminalModeConfig
	ptr, err := t.mod.Alloc(n)
	if err != nil {
		return err
	}
	defer t.mod.Free(ptr, n)
	t.mod.Zero(ptr, n)
	t.mod.SetU16(ptr, 0, packMode(mode))
	if on {
		t.mod.SetU8(ptr, wasmvt.TerminalModeConfigValueOff, 1)
	}
	return t.mod.SetOption(t.term, wasmvt.TerminalOptionMode, ptr)
}

// packMode encodes a mode number the way GhosttyTerminalModeConfig
// stores it. The high bit is the ANSI/DEC private marker. The low 15
// bits are the mode number.
func packMode(mode Mode) uint16 {
	v := uint16(mode.Number) & 0x7fff
	if mode.ANSI {
		v |= 0x8000
	}
	return v
}

// readRGB reads a TerminalData color option into three bytes. data is
// a generated TerminalData constant such as color foreground. A failed
// get returns fallback instead of an error.
func (t *Terminal) readRGB(data int32, fallback color.NRGBA) color.NRGBA {
	t.mod.Zero(t.scratch, 4)
	if t.mod.Get(t.term, data, t.scratch) != nil {
		return fallback
	}
	return color.NRGBA{R: t.mod.U8(t.scratch, 0), G: t.mod.U8(t.scratch, 1), B: t.mod.U8(t.scratch, 2), A: 0xff}
}

// applyColorLocked sets a TerminalOption color. nil clears it by
// passing a null value pointer, which is what ghostty_terminal_set
// documents for a cleared option. Otherwise three RGB bytes are
// passed by pointer and freed after the call.
func (t *Terminal) applyColorLocked(opt int32, c color.Color) error {
	if t.idle {
		return errClosed
	}
	if c == nil {
		return t.mod.SetOption(t.term, opt, 0)
	}
	r, g, b, _ := c.RGBA()
	ptr, err := t.mod.Alloc(3)
	if err != nil {
		return err
	}
	defer t.mod.Free(ptr, 3)
	t.mod.SetU8(ptr, 0, uint8(r>>8))
	t.mod.SetU8(ptr, 1, uint8(g>>8))
	t.mod.SetU8(ptr, 2, uint8(b>>8))
	if err := t.mod.SetOption(t.term, opt, ptr); err != nil {
		return err
	}
	t.gridFresh = false
	return nil
}

// scrollbackLenLocked reads TerminalDataScrollbackRows. The alternate
// screen reports zero because its history is not addressable this way.
func (t *Terminal) scrollbackLenLocked() int {
	if t.alt {
		return 0
	}
	t.mod.Zero(t.scratch, 4)
	if t.mod.Get(t.term, wasmvt.TerminalDataScrollbackRows, t.scratch) != nil {
		return 0
	}
	return int(t.mod.U32(t.scratch, 0))
}

// Scrollback returns history rows, oldest first. The alternate screen has
// no scrollback. The returned cells are copies.
func (t *Terminal) Scrollback() [][]screen.Cell {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := t.scrollbackLenLocked()
	if n <= 0 || t.rows <= 0 {
		return nil
	}
	offset, length := t.scrollbar()
	lines := make([][]screen.Cell, 0, n)
	for row := 0; row < n; row += t.rows {
		t.scrollRow(row)
		t.gridFresh = false
		grid := t.refreshLocked()
		take := min(t.rows, n-row)
		for y := range take {
			line := make([]screen.Cell, t.cols)
			for x := range t.cols {
				if cell := grid.CellAt(x, y); cell != nil {
					line[x] = *cell
				}
			}
			lines = append(lines, line)
		}
	}
	t.scrollRow(int(offset))
	_ = length
	t.gridFresh = false
	return lines
}

// scrollbar reads GhosttyTerminalScrollbar. offset is the viewport
// row measured from the top of the scrollable area. length is the
// total scrollable rows. Both are u64 in the manifest.
func (t *Terminal) scrollbar() (offset, length uint64) {
	n := wasmvt.SizeTerminalScrollbar
	ptr, err := t.mod.Alloc(n)
	if err != nil {
		return 0, 0
	}
	defer t.mod.Free(ptr, n)
	t.mod.Zero(ptr, n)
	if t.mod.Get(t.term, wasmvt.TerminalDataScrollbar, ptr) != nil {
		return 0, 0
	}
	off := wasmvt.TerminalScrollbarOffsetOff
	ln := wasmvt.TerminalScrollbarLenOff
	return t.mod.U64(ptr, off), t.mod.U64(ptr, ln)
}

// scrollRow moves the viewport to an absolute row. The argument is a
// GhosttyTerminalScrollViewport tagged union. The row arm's payload
// sits at the value offset. Ghostty ignores the call when the
// terminal handle is null.
func (t *Terminal) scrollRow(row int) {
	n := wasmvt.SizeTerminalScrollViewport
	t.mod.Zero(t.scratch, n)
	t.mod.SetI32(t.scratch, wasmvt.TerminalScrollViewportTagOff, wasmvt.TerminalScrollViewportTagRow)
	value := wasmvt.TerminalScrollViewportValueOff
	t.mod.SetU32(t.scratch, value, uint32(row))
	t.mod.Xghostty_terminal_scroll_viewport(t.term, t.scratch)
}

// ProgramStatuses returns OSC 7501 records sorted by id. App is copied from
// the nearest ancestor that set one. The slice is a copy.
func (t *Terminal) ProgramStatuses() []ProgramStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.statusView)
}
