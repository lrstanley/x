// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/internal/testutil"
)

const ms = time.Millisecond

// event is one event of a timeline built with [buildTimeline]: an
// asciicast event code and its data, at an offset.
type event struct {
	at   time.Duration
	code string
	data string
}

func output(atMS int, s string) event { return event{time.Duration(atMS) * ms, "o", s} }
func input(atMS int, s string) event  { return event{time.Duration(atMS) * ms, "i", s} }
func mark(atMS int, s string) event   { return event{time.Duration(atMS) * ms, "m", s} }

// buildTimeline returns a cols by rows timeline titled title, with events,
// read from the asciicast v2 file they make.
func buildTimeline(t *testing.T, cols, rows int, title string, events ...event) *reel.Timeline {
	t.Helper()
	var b strings.Builder
	header, _ := json.Marshal(map[string]any{"version": 2, "width": cols, "height": rows, "title": title})
	b.Write(header)
	for _, ev := range events {
		data, _ := json.Marshal(ev.data)
		fmt.Fprintf(&b, "\n[%.3f, %q, %s]", ev.at.Seconds(), ev.code, data)
	}
	tl, err := reel.ReadTimeline(strings.NewReader(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	return tl
}

// recordFrames is an [reel.Effect] that appends every frame it sees to
// frames and passes it on.
type recordFrames struct{ frames *[]reel.Frame }

func (e recordFrames) Start(*reel.Context) (reel.Stage, error) {
	return func(f reel.Frame, emit func(reel.Frame)) {
		*e.frames = append(*e.frames, f)
		emit(f)
	}, nil
}

// exportFrames exports tl to a GIF with the fake rasterizer, and returns
// the frames the GIF was given.
func exportFrames(t *testing.T, tl *reel.Timeline, p reel.Profile) []reel.Frame {
	t.Helper()
	var frames []reel.Frame
	p.Outputs = reel.Files(filepath.Join(t.TempDir(), "out.gif"))
	p.Rasterizer = testutil.Rasterizer{}
	p.Effects = append(slices.Clip(p.Effects), recordFrames{&frames})
	if err := reel.Export(t.Context(), tl, p); err != nil {
		t.Fatal(err)
	}
	return frames
}

// totalDelay is how long frames play.
func totalDelay(frames []reel.Frame) time.Duration {
	var total time.Duration
	for _, f := range frames {
		total += f.Delay
	}
	return total
}
