// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"bytes"
	"image"
	"image/draw"
	"time"

	"github.com/lrstanley/x/term/reel/vt/screen"
	"github.com/lrstanley/x/term/steep/snapshot"
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

// ImageInto renders the current terminal screen buffer into dst, at
// dst.Rect.Min. dst must be at least as large as the image [Harness.Image]
// returns for the same options. Pixels written to dst are caller-owned and
// safe to retain after the call returns.
//
// See also [Harness.Image].
func (h *Harness) ImageInto(dst *image.NRGBA) {
	h.tb.Helper()
	defer h.hook(HookCapture, CaptureMeta{Kind: "image_into"})()
	h.drawImageInto(dst)
}

func (h *Harness) drawImageInto(dst *image.NRGBA) {
	h.tb.Helper()
	st := liveState(h)
	scr := h.imageViewport(st)
	h.imageMu.Lock()
	defer h.imageMu.Unlock()
	h.imageRenderer.Draw(dst, scr, st, timelineAt(h, time.Now()))
}

// Image renders the current terminal screen buffer as an image. Configure the
// harness renderer with [WithRenderer] when creating the harness.
//
// The returned [image.Image] is owned by the harness and invalidated by the
// next [Harness.Image] call. Do not mutate it and do not retain it across
// [Harness.Image] calls; encode or copy immediately, or use
// [Harness.ImageInto] when pixels must outlive the draw.
//
// See also [Harness.ImageInto].
func (h *Harness) Image() image.Image {
	h.tb.Helper()
	defer h.hook(HookCapture, CaptureMeta{Kind: "image"})()
	return h.drawImage()
}

func (h *Harness) drawImage() image.Image {
	st := liveState(h)
	scr := h.imageViewport(st)
	h.imageMu.Lock()
	defer h.imageMu.Unlock()
	r := h.imageRenderer.Layout(scr.Width(), scr.Height()).Image
	if h.image == nil || h.image.Rect != r {
		h.image = image.NewNRGBA(r)
	}
	h.imageRenderer.Draw(h.image, scr, st, timelineAt(h, time.Now()))
	return h.image
}

// imageViewport returns the viewport to draw with st: the retained clone when
// its generation still matches st, or a new clone via Screen otherwise, so a
// generation change is never drawn from the previous grid.
func (h *Harness) imageViewport(st screen.State) *screen.Buffer {
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

// liveState is the terminal's state now, with the harness's focus, which the
// terminal does not track. Call it from the test goroutine, which is the one
// that changes focus.
func liveState(h *Harness) screen.State {
	st := h.emulator.term.State()
	st.Focused = h.emulator.focused
	return st
}

// timelineAt is the offset of t from the harness start, which frames are
// drawn as of.
func timelineAt(h *Harness, t time.Time) time.Duration {
	return t.Sub(h.start)
}

// copyLive renders the terminal now and returns an owned image, with the
// screen and state it was drawn from. Call it from the test goroutine.
func copyLive(h *Harness) (img image.Image, scr *screen.Buffer, st screen.State) {
	st = liveState(h)
	scr = h.imageViewport(st)
	h.imageMu.Lock()
	defer h.imageMu.Unlock()
	return copyDrawnLocked(h, scr, st, timelineAt(h, time.Now())), scr, st
}

// copyCommitted renders scr as it looked at st, as of time at, and returns an
// owned image, with what keeps its indicators (the progress bar and program
// status) moving in copies of the image as of later times when they move with
// the clock (nil otherwise). st takes the harness's focus now. Call it from
// the test goroutine.
func copyCommitted(h *Harness, scr *screen.Buffer, st screen.State, at time.Time) (img image.Image, held *heldIndicators) {
	if scr == nil {
		return nil, nil
	}
	st.Focused = h.emulator.focused
	h.imageMu.Lock()
	img = copyDrawnLocked(h, scr, st, timelineAt(h, at))
	h.imageMu.Unlock()
	return img, newHeldIndicators(h, scr, st)
}

// heldIndicators keeps the indicators of a held frame moving: the screen and
// state the image was drawn from.
type heldIndicators struct {
	h     *Harness
	scr   *screen.Buffer
	state screen.State
}

// newHeldIndicators returns the [heldIndicators] of an image of scr drawn
// with st. It is nil unless they move with the clock.
func newHeldIndicators(h *Harness, scr *screen.Buffer, st screen.State) *heldIndicators {
	if scr == nil || !h.imageRenderer.Animates(st) {
		return nil
	}
	return &heldIndicators{h: h, scr: scr, state: st}
}

// change reports whether the indicators may look different at to than at
// from, see [render.Renderer.Changes].
func (r *heldIndicators) change(from, to time.Time) bool {
	r.h.imageMu.Lock()
	defer r.h.imageMu.Unlock()
	return r.h.imageRenderer.Changes(r.state, timelineAt(r.h, from), timelineAt(r.h, to))
}

// repaint repaints the indicators of dst, a copy of the image, as of at.
func (r *heldIndicators) repaint(dst *image.NRGBA, at time.Time) {
	r.h.imageMu.Lock()
	defer r.h.imageMu.Unlock()
	r.h.imageRenderer.Repaint(dst, r.scr, r.state, timelineAt(r.h, at))
}

// copyDrawnLocked renders scr with st, as of offset at, and returns an owned
// image. h.imageMu must be held.
func copyDrawnLocked(h *Harness, scr screen.Screen, st screen.State, at time.Duration) *image.NRGBA {
	b := scr.Bounds()
	dst := newNRGBA(h.imageRenderer.Layout(b.Dx(), b.Dy()).Image)
	h.imageRenderer.Draw(dst, scr, st, at)
	return dst
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
