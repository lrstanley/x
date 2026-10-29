// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"image"
	"slices"

	"github.com/lrstanley/x/term/steep/snapshot"
)

// snapshotOpts prepends the STEEP_SNAPSHOT_DIR default to opts, so a
// per-call directory still wins.
func (h *Harness) snapshotOpts(opts []snapshot.Option) []snapshot.Option {
	if h.cfg.snapshotDir != "" {
		opts = append([]snapshot.Option{snapshot.WithDir(h.cfg.snapshotDir)}, opts...)
	}
	return opts
}

// textSnapshotOpts is [Harness.snapshotOpts], plus the harness's [WithANSI].
func (h *Harness) textSnapshotOpts(opts []snapshot.Option) []snapshot.Option {
	opts = h.snapshotOpts(opts)
	if h.cfg.stripANSI {
		opts = append([]snapshot.Option{snapshot.WithANSI(false)}, opts...)
	}
	return opts
}

// AssertSnapshot compares [Harness.View] against a snapshot file. It allows
// the test to continue.
func (h *Harness) AssertSnapshot(opts ...snapshot.Option) *Harness {
	h.tb.Helper()
	meta := CaptureMeta{Kind: CaptureKindText}
	defer h.hook(HookCapture, meta)()
	var res snapshot.Result
	pass := snapshot.AssertEqual(h.tb, h.View(), withResult(&res, h.textSnapshotOpts(opts))...)
	h.noteSnapshot(snapshotNote{CaptureMeta: meta, Pass: pass}, res)
	return h
}

// RequireSnapshot compares [Harness.View] against a snapshot file. It fails
// the test immediately if the snapshot does not match.
func (h *Harness) RequireSnapshot(opts ...snapshot.Option) *Harness {
	h.tb.Helper()
	meta := CaptureMeta{Kind: CaptureKindText, Required: true}
	defer h.hook(HookCapture, meta)()
	var res snapshot.Result
	pass := snapshot.AssertEqual(h.tb, h.View(), withResult(&res, h.textSnapshotOpts(opts))...)
	h.noteSnapshot(snapshotNote{CaptureMeta: meta, Pass: pass}, res)
	if !pass {
		h.tb.FailNow()
	}
	return h
}

// AssertJSON compares the screen's cells and the terminal's state, as JSON,
// against a snapshot file.
func (h *Harness) AssertJSON(opts ...snapshot.Option) *Harness {
	h.tb.Helper()
	meta := CaptureMeta{Kind: CaptureKindJSON}
	defer h.hook(HookCapture, meta)()
	var res snapshot.Result
	pass := snapshot.AssertScreenEqual(h.tb, h.session.Screen(), withResult(&res, h.snapshotOpts(opts))...)
	h.noteSnapshot(snapshotNote{CaptureMeta: meta, Pass: pass}, res)
	return h
}

// RequireJSON is [Harness.AssertJSON], but fails the test immediately if the
// snapshot does not match.
func (h *Harness) RequireJSON(opts ...snapshot.Option) *Harness {
	h.tb.Helper()
	meta := CaptureMeta{Kind: CaptureKindJSON, Required: true}
	defer h.hook(HookCapture, meta)()
	var res snapshot.Result
	pass := snapshot.AssertScreenEqual(h.tb, h.session.Screen(), withResult(&res, h.snapshotOpts(opts))...)
	h.noteSnapshot(snapshotNote{CaptureMeta: meta, Pass: pass}, res)
	if !pass {
		h.tb.FailNow()
	}
	return h
}

// withResult returns opts plus [snapshot.WithResult] of res, in a new slice.
func withResult(res *snapshot.Result, opts []snapshot.Option) []snapshot.Option {
	return append(slices.Clip(opts), snapshot.WithResult(res))
}

// ImageInto renders the screen into dst, at dst.Rect.Min (see
// [reel.Imager.ImageInto]). dst must be at least as large as the image
// [Harness.Image] returns. Unlike that image, dst is the caller's, safe to
// keep after the call.
func (h *Harness) ImageInto(dst *image.NRGBA) {
	h.tb.Helper()
	defer h.hook(HookCapture, CaptureMeta{Kind: CaptureKindImage})()
	h.check(h.imager.ImageInto(dst))
}

// Image renders the screen as an image, with the Render options of the
// recording profile (see [WithRecording] and [reel.Imager.Image]).
//
// The image is reused by the next call, so do not mutate or retain it; copy
// it, or use [Harness.ImageInto].
func (h *Harness) Image() image.Image {
	h.tb.Helper()
	defer h.hook(HookCapture, CaptureMeta{Kind: CaptureKindImage})()
	img, err := h.imager.Image()
	h.check(err)
	return img
}
