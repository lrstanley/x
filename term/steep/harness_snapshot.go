// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"time"

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
	scr := h.imageViewport()
	h.imageMu.Lock()
	defer h.imageMu.Unlock()
	h.imageRenderer.DrawInto(dst, image.Rectangle{}, scr)
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
	scr := h.imageViewport()
	h.imageMu.Lock()
	defer h.imageMu.Unlock()
	return h.imageRenderer.Draw(scr)
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
	next := viewportState(h, prev, st)
	if h.imageStateSet && prev == next {
		return
	}
	h.imageRenderer.UpdateEmulatorState(&next)
	h.imageState = next
	h.imageStateSet = true
}

// copyCommitted renders scr as it looked at st, as of time at, and returns an
// owned image, with a func that repaints its progress bar in a copy of the
// image as of a later time when the bar moves with the clock (nil otherwise).
// The harness image cache
// is invalidated so the next live draw does not reuse metadata from this
// historical frame. Call it from the test goroutine.
func copyCommitted(h *Harness, scr *screen.Buffer, st vt.Snapshot, at time.Time) (img image.Image, redraw func(*image.NRGBA, time.Time)) {
	if scr == nil {
		return nil, nil
	}
	h.imageMu.Lock()
	defer h.imageMu.Unlock()
	img = copyDrawnLocked(h, scr, viewportState(h, h.imageState, st), at)
	return img, redrawLocked(h, scr)
}

// redrawLocked returns a func that repaints the progress bar of dst, a copy
// of the image just drawn for scr, with the state the renderer drew it with, as
// of a later time. It is nil unless that bar moves with the clock. h.imageMu
// must be held.
func redrawLocked(h *Harness, scr *screen.Buffer) func(*image.NRGBA, time.Time) {
	if scr == nil || !h.imageRenderer.ProgressAnimates() {
		return nil
	}
	state := *h.imageRenderer.GetEmulatorState()
	return func(dst *image.NRGBA, at time.Time) {
		h.imageMu.Lock()
		defer h.imageMu.Unlock()
		drawStateLocked(h, state, at, func() { h.imageRenderer.RepaintProgress(dst, scr) })
	}
}

// copyDrawnLocked renders scr with state, as of time at (zero for now), and
// returns an owned image. It invalidates the harness image cache like
// [copyCommitted]. h.imageMu must be held.
func copyDrawnLocked(h *Harness, scr screen.Screen, state still.EmulatorState, at time.Time) *image.NRGBA {
	var dst *image.NRGBA
	drawStateLocked(h, state, at, func() {
		dst = newNRGBA(h.imageRenderer.Bounds(scr))
		h.imageRenderer.DrawInto(dst, image.Rectangle{}, scr)
	})
	return dst
}

// drawStateLocked loads state into the renderer, pins its clock to at (zero
// for now) while paint runs, and invalidates the harness image cache, since the
// next live draw would otherwise reuse metadata from this historical state.
// h.imageMu must be held.
func drawStateLocked(h *Harness, state still.EmulatorState, at time.Time, paint func()) {
	h.imageRenderer.UpdateEmulatorState(&state)
	h.imageStateSet = false
	h.imageAt = at
	defer func() { h.imageAt = time.Time{} }()
	paint()
}

// frameBufs holds a few finished frame buffers for [newNRGBA]. It is a small
// bounded list, not a sync.Pool, which keeps up to two generations of
// full-size frames alive.
var frameBufs = make(chan []byte, 3)

// newNRGBA returns an image covering r, reusing a released buffer when one
// fits. Its pixels are stale: the caller must paint every one.
func newNRGBA(r image.Rectangle) *image.NRGBA {
	n := r.Dx() * r.Dy() * 4
	select {
	case pix := <-frameBufs:
		if cap(pix) >= n {
			return &image.NRGBA{Pix: pix[:n], Stride: r.Dx() * 4, Rect: r}
		}
	default:
	}
	return image.NewNRGBA(r)
}

// releaseNRGBA hands img's buffer back to [newNRGBA]. img must not be used
// again, nor shared with anything that still reads it.
func releaseNRGBA(img image.Image) {
	if n, ok := img.(*image.NRGBA); ok {
		select {
		case frameBufs <- n.Pix:
		default:
		}
	}
}

func viewportState(h *Harness, prev still.EmulatorState, st vt.Snapshot) still.EmulatorState {
	cur := st.Cursor
	return still.EmulatorState{
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
		Progress:        stillProgress(st.Progress),
	}
}

// stillProgress maps an OSC 9;4 report to the renderer's progress state.
func stillProgress(p vt.Progress) still.Progress {
	var state still.ProgressState
	switch p.State {
	case vt.ProgressSet:
		state = still.ProgressSet
	case vt.ProgressError:
		state = still.ProgressError
	case vt.ProgressIndeterminate:
		state = still.ProgressIndeterminate
	case vt.ProgressPause:
		state = still.ProgressPause
	case vt.ProgressRemove:
		return still.Progress{}
	default:
		return still.Progress{}
	}
	return still.Progress{State: state, Percent: p.Percent}
}

// cloneNRGBA returns an owned copy of src, which must not be nil, to draw on.
// Frames stay NRGBA, the renderer's own format, from the renderer to the GIF
// encoder: copying one is a plain byte copy, the encoder maps it to the GIF
// palette with a lookup table, and translucent pixels keep their exact color.
func cloneNRGBA(src image.Image) *image.NRGBA {
	if n, ok := src.(*image.NRGBA); ok {
		return &image.NRGBA{Pix: bytes.Clone(n.Pix), Stride: n.Stride, Rect: n.Rect}
	}
	b := src.Bounds()
	dst := image.NewNRGBA(b)
	draw.Draw(dst, b, src, b.Min, draw.Src)
	return dst
}

// reuseNRGBA returns prev when it already boxes next. Assigning an
// NRGBA to a color.Color allocates, so an unchanged color must not.
func reuseNRGBA(prev color.Color, next color.NRGBA) color.Color {
	if c, ok := prev.(color.NRGBA); ok && c == next {
		return prev
	}
	return next
}
