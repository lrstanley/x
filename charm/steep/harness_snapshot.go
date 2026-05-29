// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"image"
	"image/draw"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/lrstanley/x/charm/steep/snapshot"
	"github.com/lrstanley/x/charm/still"
)

func (h *Harness) snapshotOpts(opts []snapshot.Option) []snapshot.Option {
	if !collectOptions(h.mergedOpts()...).stripANSI {
		return opts
	}
	return append([]snapshot.Option{snapshot.WithANSI(false)}, opts...)
}

// AssertSnapshot compares the current terminal screen buffer against a snapshot
// file. It allows the test to continue.
//
// See also [Harness.RequireSnapshot].
func (h *Harness) AssertSnapshot(opts ...snapshot.Option) *Harness {
	h.tb.Helper()
	snapshot.AssertEqual(h.tb, h.View(), h.snapshotOpts(opts)...)
	return h
}

// RequireSnapshot compares the current terminal screen buffer against a snapshot
// file. It fails the test immediately if the snapshot does not match.
//
// See also [Harness.AssertSnapshot].
func (h *Harness) RequireSnapshot(opts ...snapshot.Option) *Harness {
	h.tb.Helper()
	if !snapshot.AssertEqual(h.tb, h.View(), h.snapshotOpts(opts)...) {
		h.tb.FailNow()
	}
	return h
}

// AssertJSON compares the current terminal screen buffer in JSON format against a
// previously captured snapshot. It allows the test to continue.
//
// See also [Harness.AssertSnapshot].
func (h *Harness) AssertJSON(opts ...snapshot.Option) *Harness {
	h.tb.Helper()
	snapshot.AssertScreenEqual(h.tb, h.emulator.snapshot(h.tb, opts...), opts...)
	return h
}

// RequireJSON compares the current terminal screen buffer in JSON format against a
// previously captured snapshot. It fails the test immediately if the snapshot does
// not match.
//
// See also [Harness.AssertJSON].
func (h *Harness) RequireJSON(opts ...snapshot.Option) *Harness {
	h.tb.Helper()
	snapshot.RequireScreenEqual(h.tb, h.emulator.snapshot(h.tb, opts...), opts...)
	return h
}

// ImageInto renders the current terminal screen buffer into dst. dst must be at
// least as large as [Harness.ImageBounds] reports for the same options. Pixels
// written to dst are caller-owned and safe to retain after the call returns.
//
// See also [Harness.Image].
func (h *Harness) ImageInto(dst draw.Image) {
	h.tb.Helper()

	h.emulator.mu.RLock()
	defer h.emulator.mu.RUnlock()

	h.syncRendererStateLocked()
	h.imageRenderer.DrawInto(dst, image.Rectangle{}, h.emulator.vt)
}

// Image renders the current terminal screen buffer as an image. Configure the
// harness renderer with [WithImageRenderer] when creating the harness.
//
// The returned [image.Image] is owned by the harness renderer and invalidated
// by the next [Harness.Image] call or [Harness.Close]. Do not mutate it and do
// not retain it across [Harness.Image] calls; encode or copy immediately, or use
// [Harness.ImageInto] when pixels must outlive the draw.
//
// See also [Harness.ImageInto].
func (h *Harness) Image() image.Image {
	h.tb.Helper()

	h.emulator.mu.RLock()
	defer h.emulator.mu.RUnlock()

	h.syncRendererStateLocked()
	return h.imageRenderer.Draw(h.emulator.vt)
}

// syncRendererStateLocked copies live emulator state into the harness renderer.
// The caller must hold [emulator.mu] for reading.
func (h *Harness) syncRendererStateLocked() {
	h.tb.Helper()

	state := h.rendererStateLocked()
	h.imageRenderer.UpdateEmulatorState(&state)
}

func (h *Harness) rendererStateLocked() still.EmulatorState {
	h.emulator.trackMu.RLock()
	defer h.emulator.trackMu.RUnlock()

	return still.EmulatorState{
		Title:           h.emulator.title,
		Focused:         h.emulator.focused,
		AltScreen:       h.emulator.altScreen,
		CursorVisible:   h.emulator.cursorVis,
		CursorX:         h.emulator.cursorPos.X,
		CursorY:         h.emulator.cursorPos.Y,
		CursorColor:     h.emulator.cursorColor,
		CursorStyle:     cursorShapeFromVT(h.emulator.cursorStyle),
		CursorBlink:     h.emulator.cursorBlink,
		FgColor:         h.emulator.fgColor,
		BgColor:         h.emulator.bgColor,
		ScrollbackCount: h.emulator.vt.ScrollbackLen(),
	}
}

func cursorShapeFromVT(style vt.CursorStyle) uv.CursorShape {
	switch style {
	case vt.CursorBlock:
		return uv.CursorBlock
	case vt.CursorUnderline:
		return uv.CursorUnderline
	case vt.CursorBar:
		return uv.CursorBar
	default:
		return uv.CursorBlock
	}
}
