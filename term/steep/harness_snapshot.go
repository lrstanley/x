// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"image"
	"slices"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/vt"
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

// AssertSnapshot compares [Harness.View] against a snapshot file. It allows
// the test to continue.
func (h *Harness) AssertSnapshot(opts ...snapshot.Option) *Harness {
	h.tb.Helper()
	meta := CaptureMeta{Kind: CaptureKindText}
	defer h.hook(HookCapture, meta)()
	var res snapshot.Result
	pass := snapshot.AssertEqual(h.tb, h.View(), withResult(&res, h.snapshotOpts(opts))...)
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
	pass := snapshot.AssertEqual(h.tb, h.View(), withResult(&res, h.snapshotOpts(opts))...)
	h.noteSnapshot(snapshotNote{CaptureMeta: meta, Pass: pass}, res)
	if !pass {
		h.tb.FailNow()
	}
	return h
}

// AssertJSON compares the screen's cells and the terminal's state, as JSON,
// against a snapshot file. It allows the test to continue.
func (h *Harness) AssertJSON(opts ...snapshot.Option) *Harness {
	h.tb.Helper()
	meta := CaptureMeta{Kind: CaptureKindJSON}
	defer h.hook(HookCapture, meta)()
	var res snapshot.Result
	pass := snapshot.AssertScreenEqual(h.tb, screenSnapshot(h.tb, h.session), withResult(&res, opts)...)
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
	pass := snapshot.AssertScreenEqual(h.tb, screenSnapshot(h.tb, h.session), withResult(&res, opts)...)
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

// ImageInto renders the screen into dst, at dst.Rect.Min. dst must be at
// least as large as the image [Harness.Image] returns. Unlike that image,
// dst is the caller's, safe to keep after the call.
func (h *Harness) ImageInto(dst *image.NRGBA) {
	h.tb.Helper()
	defer h.hook(HookCapture, CaptureMeta{Kind: CaptureKindImage})()
	h.drawImageInto(dst)
}

func (h *Harness) drawImageInto(dst *image.NRGBA) {
	h.tb.Helper()
	st := h.session.State()
	scr := h.imageViewport(st)
	h.imageMu.Lock()
	defer h.imageMu.Unlock()
	h.imageRenderer.Draw(dst, scr, st, timelineAt(h, time.Now()))
}

// Image renders the screen as an image, with the Render options of the
// recording profile (see [WithRecording]).
//
// The image is reused by the next call, so do not mutate or retain it; copy
// it, or use [Harness.ImageInto].
func (h *Harness) Image() image.Image {
	h.tb.Helper()
	defer h.hook(HookCapture, CaptureMeta{Kind: CaptureKindImage})()
	return h.drawImage()
}

func (h *Harness) drawImage() image.Image {
	st := h.session.State()
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

// imageViewport returns the retained screen clone while its generation
// matches st, or a new clone, so a generation change is never drawn from the
// previous grid.
func (h *Harness) imageViewport(st vt.State) *vt.Buffer {
	h.imageMu.Lock()
	if scr := h.imageScreen; st.Generation != 0 && scr != nil && scr.Generation() == st.Generation {
		h.imageMu.Unlock()
		return scr
	}
	h.imageMu.Unlock()

	scr := h.session.Terminal().Screen()
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

// timelineAt is the offset of t from the harness start.
func timelineAt(h *Harness, t time.Time) time.Duration {
	return t.Sub(h.start)
}

func screenSnapshot(tb testing.TB, s *reel.Session) *snapshot.ScreenSnapshot {
	tb.Helper()
	term := s.Terminal()
	grid := term.Screen()
	cur := term.Cursor()
	fg := term.Foreground()
	bg := term.Background()
	cursorColor, hasCursorColor := term.CursorColor()
	cols, rows := term.Size()
	cursorSnap := snapshot.Cursor{
		Position: snapshot.Position{X: cur.X, Y: cur.Y},
		Visible:  cur.Visible,
		Blink:    cur.Blink,
		Style:    cur.Shape,
	}
	if hasCursorColor {
		cursorSnap.Color = snapshot.Color{Color: cursorColor}
	}
	snap := &snapshot.ScreenSnapshot{
		Title:         term.Title(),
		Rows:          rows,
		Cols:          cols,
		AltScreen:     term.AltScreen(),
		Focused:       s.Focused(),
		ProgramStatus: snapshotProgramStatuses(programStatuses(term)),
		Cursor:        cursorSnap,
		BgColor:       snapshot.Color{Color: bg},
		FgColor:       snapshot.Color{Color: fg},
	}
	snap.WithScreenBuffer(tb, grid)
	return snap
}

func snapshotProgramStatuses(in []ansi.ProgramStatus) []snapshot.ProgramStatus {
	if len(in) == 0 {
		return nil
	}
	out := make([]snapshot.ProgramStatus, len(in))
	for i, s := range in {
		out[i] = snapshot.ProgramStatus{
			ID:      s.ID,
			State:   string(s.State),
			Kind:    string(s.Kind),
			App:     s.App,
			Title:   s.Title,
			Message: s.Message,
		}
		if s.HasProgress {
			progress := s.Progress
			out[i].Progress = &progress
		}
	}
	return out
}
