// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/reel/vt"
)

// TestExportEventTimes checks that program events are stamped with the
// timeline offset of the output that carried them, and that an event alone,
// such as a bell, is a frame.
func TestExportEventTimes(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(Header{Cols: 6, Rows: 1},
		outAt(0, "hi"),
		outAt(250, "\a"),
		outAt(400, "\x1b]9;done\x1b\\"),
		Exit{event: at(600)},
	)
	frames := exportFrames(t, tl, Profile{FPS: 100})
	if len(frames) != 3 {
		t.Fatalf("got %d frames, want 3: text, bell, notification", len(frames))
	}
	if bell := frames[1].Cells.State.Bell; bell != (vt.Bell{Count: 1, At: 250 * ms}) {
		t.Errorf("Bell = %+v, want one at 250ms", bell)
	}
	if n := frames[2].Cells.State.Notifications; len(n) != 1 || n[0].At != 400*ms || n[0].Body != "done" {
		t.Errorf("Notifications = %+v, want done at 400ms", n)
	}
}

// TestExportBellFades checks that with [render.WithBell], a frame held after
// a bell is split while the flash fades and again when the title bell clears.
func TestExportBellFades(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(Header{Cols: 6, Rows: 1},
		outAt(0, "hi"),
		outAt(250, "\a"),
		Exit{event: at(3000)},
	)
	var frames []Frame
	err := Export(t.Context(), tl, Profile{
		FPS:     100,
		Render:  []render.Option{render.WithBell(true), render.WithWindowChrome(true)},
		Outputs: MustFiles(filepath.Join(t.TempDir(), "out.gif")),
		Effects: []Effect{recordFrames(&frames)},
	})
	if err != nil {
		t.Fatal(err)
	}
	var ats []time.Duration
	for _, f := range frames {
		ats = append(ats, f.At)
	}
	want := []time.Duration{0, 250 * ms, 300 * ms, 350 * ms, 400 * ms, 450 * ms, 500 * ms, 550 * ms, 2250 * ms}
	if !slices.Equal(ats, want) {
		t.Fatalf("frames at %v, want %v", ats, want)
	}
}
