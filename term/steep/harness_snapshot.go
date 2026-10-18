// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"image"
	"image/color"
	"image/draw"

	"github.com/lrstanley/x/term/steep/snapshot"
	"github.com/lrstanley/x/term/still"
	"github.com/lrstanley/x/term/vt"
	"github.com/lrstanley/x/term/vt/screen"
)

func (h *Harness) snapshotOpts(opts []snapshot.Option) []snapshot.Option {
	cfg := collectOptions(h.tb, h.mergedOpts()...)
	out := opts
	if dir := snapshotDirFromEnv(cfg); dir != nil {
		out = append([]snapshot.Option{dir}, out...)
	}
	if !cfg.stripANSI {
		return out
	}
	return append([]snapshot.Option{snapshot.WithANSI(false)}, out...)
}

// AssertSnapshot compares the current terminal screen buffer against a snapshot
// file. It allows the test to continue.
//
// See also [Harness.RequireSnapshot].
func (h *Harness) AssertSnapshot(opts ...snapshot.Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookCapture, CaptureMeta{Kind: "snapshot"})()
	snapshot.AssertEqual(h.tb, h.View(), h.snapshotOpts(opts)...)
	return h
}

// RequireSnapshot compares the current terminal screen buffer against a snapshot
// file. It fails the test immediately if the snapshot does not match.
//
// See also [Harness.AssertSnapshot].
func (h *Harness) RequireSnapshot(opts ...snapshot.Option) *Harness {
	h.tb.Helper()
	defer h.hook(HookCapture, CaptureMeta{Kind: "snapshot"})()
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
	defer h.hook(HookCapture, CaptureMeta{Kind: "json"})()
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
	defer h.hook(HookCapture, CaptureMeta{Kind: "json"})()
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
	defer h.hook(HookCapture, CaptureMeta{Kind: "image_into"})()
	h.drawImageInto(dst)
}

func (h *Harness) drawImageInto(dst draw.Image) {
	h.tb.Helper()
	h.imageRenderer.DrawInto(dst, image.Rectangle{}, h.imageViewport())
}

// Image renders the current terminal screen buffer as an image. Configure the
// harness renderer with [WithRenderer] when creating the harness.
//
// The returned [image.Image] is owned by the harness renderer and invalidated
// by the next [Harness.Image] call or [Harness.Close]. Do not mutate it and do
// not retain it across [Harness.Image] calls; encode or copy immediately, or use
// [Harness.ImageInto] when pixels must outlive the draw.
//
// See also [Harness.ImageInto].
func (h *Harness) Image() image.Image {
	h.tb.Helper()
	defer h.hook(HookCapture, CaptureMeta{Kind: "image"})()
	return h.drawImage()
}

func (h *Harness) drawImage() image.Image {
	return h.imageRenderer.Draw(h.imageViewport())
}

// imageViewport copies emulator state into the renderer and returns the
// retained viewport clone when its generation still matches the terminal.
// A mismatch clones via Screen, so a generation change is never drawn
// from the previous grid.
func (h *Harness) imageViewport() *screen.Buffer {
	st := h.emulator.term.Snapshot()
	syncViewport(h, st)

	h.imageMu.Lock()
	if scr := h.imageScreen; st.Generation != 0 && scr != nil && scr.Generation() == st.Generation {
		h.imageMu.Unlock()
		return scr
	}
	h.imageMu.Unlock()

	scr := h.emulator.term.Screen()
	h.imageMu.Lock()
	h.imageScreen = scr
	h.imageMu.Unlock()
	return scr
}

func (h *Harness) dropImageScreen() {
	h.imageMu.Lock()
	h.imageScreen = nil
	h.imageMu.Unlock()
}

// syncViewport copies st into the renderer when it differs from the
// last copy. Unchanged NRGBA colors keep the previous interface value.
// The terminal lock is not held here.
func syncViewport(h *Harness, st vt.Snapshot) {
	h.imageMu.Lock()
	defer h.imageMu.Unlock()

	prev := h.imageState
	cur := st.Cursor
	next := still.EmulatorState{
		Title:           st.Title,
		Focused:         h.emulator.focused,
		AltScreen:       st.AltScreen,
		CursorVisible:   cur.Visible,
		CursorX:         cur.X,
		CursorY:         cur.Y,
		CursorColor:     reuseNRGBA(prev.CursorColor, st.CursorColor),
		CursorStyle:     cur.Shape,
		CursorBlink:     cur.Blink,
		FgColor:         reuseNRGBA(prev.FgColor, st.Foreground),
		BgColor:         reuseNRGBA(prev.BgColor, st.Background),
		ScrollbackCount: st.ScrollbackLen,
	}
	if h.imageStateSet && prev == next {
		return
	}
	h.imageRenderer.UpdateEmulatorState(&next)
	h.imageState = next
	h.imageStateSet = true
}

// reuseNRGBA returns prev when it already boxes next. Assigning an
// NRGBA to a color.Color allocates, so an unchanged color must not.
func reuseNRGBA(prev color.Color, next color.NRGBA) color.Color {
	if c, ok := prev.(color.NRGBA); ok && c == next {
		return prev
	}
	return next
}
