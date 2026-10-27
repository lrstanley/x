// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"fmt"

	"github.com/lrstanley/x/term/reel/vt/internal/wasmvt"
)

// SendKey encodes a key press and writes it to the pty reply stream.
//
// The encoder is synced from the terminal first, so keyboard modes
// follow the emulator. key.Code is a GhosttyKey manifest name
// ("ENTER", "A"). An unknown name panics in [wasmvt.KeyByName].
// An empty code leaves the key at 0 and the encoder uses Text.
func (t *Terminal) SendKey(key Key) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return errClosed
	}
	t.mod.Xghostty_key_encoder_setopt_from_terminal(t.keys, t.term)
	action := wasmvt.KeyActionPress
	t.mod.Xghostty_key_event_set_action(t.keyEv, action)
	t.mod.Xghostty_key_event_set_mods(t.keyEv, int32(key.Mods))
	code := int32(0)
	if key.Code != "" {
		code = wasmvt.KeyByName(key.Code)
	}
	t.mod.Xghostty_key_event_set_key(t.keyEv, code)
	var textPtr int32
	if key.Text != "" {
		raw := []byte(key.Text)
		var err error
		textPtr, err = t.mod.Alloc(len(raw))
		if err != nil {
			return err
		}
		defer t.mod.Free(textPtr, len(raw))
		t.mod.Put(textPtr, raw)
		t.mod.Xghostty_key_event_set_utf8(t.keyEv, textPtr, int32(len(raw)))
	} else {
		t.mod.Xghostty_key_event_set_utf8(t.keyEv, 0, 0)
	}
	return t.encodeKey()
}

// encodeKey runs ghostty_key_encoder_encode into a scratch buffer.
// The length out-parameter sits immediately after the 128-byte cap.
// Encoded bytes are copied onto [Terminal.Read].
func (t *Terminal) encodeKey() error {
	const capN = 128
	buf, err := t.mod.Alloc(capN + 4)
	if err != nil {
		return err
	}
	defer t.mod.Free(buf, capN+4)
	lenPtr := buf + capN
	t.mod.Zero(lenPtr, 4)
	rc := t.mod.Xghostty_key_encoder_encode(t.keys, t.keyEv, buf, capN, lenPtr)
	if err := wasmvt.Result("key encode", rc); err != nil {
		return err
	}
	n := int(t.mod.U32(lenPtr, 0))
	if n > 0 {
		t.replies.Write(append([]byte(nil), t.mod.Slice(buf, n)...))
	}
	return nil
}

// SendMouse encodes a mouse event when a tracking mode is enabled.
//
// x and y are cell coordinates. Ghostty's position is surface pixels,
// so they are scaled by the cell size stored on the terminal. The
// position pointer is two little-endian binary32 values. The C type
// GhosttySurfacePosition is two doubles. This build's mouse event
// setter consumes the 8-byte form written here.
func (t *Terminal) SendMouse(action MouseAction, button MouseButton, x, y int) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return errClosed
	}
	if err := t.configureMouse(); err != nil {
		return err
	}
	t.mod.Xghostty_mouse_event_set_action(t.mouseEv, int32(action))
	t.mod.Xghostty_mouse_event_set_button(t.mouseEv, int32(button))
	pos, err := t.mod.Alloc(8)
	if err != nil {
		return err
	}
	defer t.mod.Free(pos, 8)
	t.mod.SetF32(pos, 0, float32(x)*float32(t.cellW))
	t.mod.SetF32(pos, 4, float32(y)*float32(t.cellH))
	t.mod.Xghostty_mouse_event_set_position(t.mouseEv, pos)
	const capN = 64
	buf, err := t.mod.Alloc(capN + 4)
	if err != nil {
		return err
	}
	defer t.mod.Free(buf, capN+4)
	lenPtr := buf + capN
	t.mod.Zero(lenPtr, 4)
	rc := t.mod.Xghostty_mouse_encoder_encode(t.mice, t.mouseEv, buf, capN, lenPtr)
	if err := wasmvt.Result("mouse encode", rc); err != nil {
		return err
	}
	n := int(t.mod.U32(lenPtr, 0))
	if n > 0 {
		t.replies.Write(append([]byte(nil), t.mod.Slice(buf, n)...))
	}
	return nil
}

// Paste encodes text for the program, bracketing it when mode 2004 is set.
func (t *Terminal) Paste(text string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return errClosed
	}
	bracketed, err := t.modeLocked(Mode{Number: 2004, ANSI: false})
	if err != nil {
		return err
	}
	raw := []byte(text)
	data, err := t.mod.Alloc(len(raw) + 1)
	if err != nil {
		return err
	}
	defer t.mod.Free(data, len(raw)+1)
	t.mod.Put(data, raw)
	need := len(raw) + 16
	buf, err := t.mod.Alloc(need + 4)
	if err != nil {
		return err
	}
	defer t.mod.Free(buf, need+4)
	lenPtr := buf + int32(need)
	t.mod.Zero(lenPtr, 4)
	flag := int32(0)
	if bracketed {
		flag = 1
	}
	rc := t.mod.Xghostty_paste_encode(data, int32(len(raw)), flag, buf, int32(need), lenPtr)
	if rc == wasmvt.ResultOutOfSpace {
		return fmt.Errorf("vt: paste encode needs %d bytes", t.mod.U32(lenPtr, 0))
	}
	if err := wasmvt.Result("paste encode", rc); err != nil {
		return err
	}
	n := int(t.mod.U32(lenPtr, 0))
	if n > 0 {
		t.replies.Write(append([]byte(nil), t.mod.Slice(buf, n)...))
	}
	return nil
}

// Focus encodes a focus-in or focus-out report when mode 1004 is set.
func (t *Terminal) Focus(focused bool) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return errClosed
	}
	on, err := t.modeLocked(Mode{Number: 1004, ANSI: false})
	if err != nil || !on {
		return err
	}
	event := wasmvt.FocusEventLost
	if focused {
		event = wasmvt.FocusEventGained
	}
	const capN = 16
	buf, err := t.mod.Alloc(capN + 4)
	if err != nil {
		return err
	}
	defer t.mod.Free(buf, capN+4)
	lenPtr := buf + capN
	t.mod.Zero(lenPtr, 4)
	rc := t.mod.Xghostty_focus_encode(event, buf, capN, lenPtr)
	if err := wasmvt.Result("focus encode", rc); err != nil {
		return err
	}
	n := int(t.mod.U32(lenPtr, 0))
	if n > 0 {
		t.replies.Write(append([]byte(nil), t.mod.Slice(buf, n)...))
	}
	return nil
}

// configureMouse maps DEC private modes onto GhosttyMouseEncoder
// options. Tracking comes from modes 9 and 1000-1003. Format comes
// from 1005, 1006, 1015, and 1016. The size option is a sized
// GhosttyMouseEncoderSize in pixels. Enum values are passed by
// pointer and freed before return. Ghostty copies them during setopt.
func (t *Terminal) configureMouse() error {
	tracking := wasmvt.MouseTrackingModeNone
	switch {
	case t.modeOn(1003):
		tracking = wasmvt.MouseTrackingModeAny
	case t.modeOn(1002):
		tracking = wasmvt.MouseTrackingModeButton
	case t.modeOn(1000):
		tracking = wasmvt.MouseTrackingModeNormal
	case t.modeOn(9):
		tracking = wasmvt.MouseTrackingModeX10
	}
	format := wasmvt.MouseFormatX10
	switch {
	case t.modeOn(1016):
		format = wasmvt.MouseFormatSgrPixels
	case t.modeOn(1006):
		format = wasmvt.MouseFormatSgr
	case t.modeOn(1015):
		format = wasmvt.MouseFormatUrxvt
	case t.modeOn(1005):
		format = wasmvt.MouseFormatUtf8
	}
	opt, err := t.mod.Alloc(4)
	if err != nil {
		return err
	}
	defer t.mod.Free(opt, 4)
	t.mod.SetI32(opt, 0, tracking)
	t.mod.Xghostty_mouse_encoder_setopt(t.mice, wasmvt.MouseEncoderOptionEvent, opt)
	t.mod.SetI32(opt, 0, format)
	t.mod.Xghostty_mouse_encoder_setopt(t.mice, wasmvt.MouseEncoderOptionFormat, opt)

	n := wasmvt.SizeMouseEncoderSize
	size, err := t.mod.Alloc(n)
	if err != nil {
		return err
	}
	defer t.mod.Free(size, n)
	t.mod.Zero(size, n)
	t.mod.SetU32(size, 0, uint32(n))
	t.mod.SetU32(size, wasmvt.MouseEncoderSizeScreenWidthOff, uint32(t.cols)*t.cellW)
	t.mod.SetU32(size, wasmvt.MouseEncoderSizeScreenHeightOff, uint32(t.rows)*t.cellH)
	t.mod.SetU32(size, wasmvt.MouseEncoderSizeCellWidthOff, t.cellW)
	t.mod.SetU32(size, wasmvt.MouseEncoderSizeCellHeightOff, t.cellH)
	t.mod.Xghostty_mouse_encoder_setopt(t.mice, wasmvt.MouseEncoderOptionSize, size)
	return nil
}

func (t *Terminal) modeOn(number int) bool {
	on, _ := t.modeLocked(Mode{Number: number, ANSI: false})
	return on
}
