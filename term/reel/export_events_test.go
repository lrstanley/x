// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"testing"

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
