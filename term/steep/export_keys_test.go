// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"image"
	"testing"
	"time"
)

func TestKeyLabel(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		meta any
		want string
	}{
		{InputMeta{Kind: "key", Key: "down"}, "↓"},
		{InputMeta{Kind: "key", Key: "enter"}, "⏎"},
		{InputMeta{Kind: "key", Key: "ctrl+c"}, "ctrl+c"},
		{InputMeta{Kind: "key", Key: "shift+tab"}, "shift+⇥"},
		{InputMeta{Kind: "key", Key: "+"}, "+"},
		{InputMeta{Kind: "type", Text: "hi\n"}, "hi⏎"},
		{InputMeta{Kind: "type", Text: "abcdefghijklmnopqrstuvwxyz"}, "abcdefghijklmno…"},
		{InputMeta{Kind: "paste", Text: "x"}, ""},
		{InputMeta{Kind: "mouse"}, ""},
		{nil, ""},
	} {
		if got := keyLabel(tc.meta); got != tc.want {
			t.Errorf("keyLabel(%v) = %q, want %q", tc.meta, got, tc.want)
		}
	}
}

func TestKeyTimelineState(t *testing.T) {
	t.Parallel()
	ms := time.Millisecond
	// Keys stay 1500ms after a burst: opaque for 1000ms, then three fade
	// levels on the 50ms grid at 1000, 1150 (1166 snapped), and 1350 (1333).
	tl := keyTimeline{
		at:    []time.Duration{0, 100 * ms, 3000 * ms},
		shown: 1500 * ms, hold: 1000 * ms, step: 50 * ms,
	}
	for _, tc := range []struct {
		p    time.Duration
		want keyState
	}{
		{-50 * ms, noKeys},
		{0, keyState{0, 0, 0}},
		{100 * ms, keyState{0, 1, 0}},
		{1099 * ms, keyState{0, 1, 0}},
		{1100 * ms, keyState{0, 1, 1}},
		{1250 * ms, keyState{0, 1, 2}},
		{1450 * ms, keyState{0, 1, 3}},
		{1600 * ms, noKeys},
		{3000 * ms, keyState{2, 2, 0}},
	} {
		if got := tl.state(tc.p); got != tc.want {
			t.Errorf("state(%v) = %+v, want %+v", tc.p, got, tc.want)
		}
	}
}

func TestExportKeysOverlay(t *testing.T) {
	t.Parallel()
	const shown = 300 * time.Millisecond
	var in, out []ExportFrame
	capture := func(frames *[]ExportFrame) ExportMutator {
		return func(_ ExportContext, f ExportFrame) []ExportFrame {
			*frames = append(*frames, f)
			return []ExportFrame{f}
		}
	}
	h := NewHarness(t, rootTestModel{},
		WithExport(ExportOn, ExportTypeGIF),
		WithExportDir(t.TempDir()),
		WithExportMutators(capture(&in), keysOverlay(shown), capture(&out)),
		WithIgnoreEnv(true),
	)
	h.Type("abc")
	h.Key("enter")
	time.Sleep(2 * shown)
	finishGIF(t, h, false)

	var inTotal, outTotal time.Duration
	for _, f := range in {
		inTotal += f.Delay
	}
	shownFrames := 0
	for _, f := range out {
		outTotal += f.Delay
		if !isInput(in, f.Image) {
			shownFrames++
			if f.Image.Bounds() != in[0].Image.Bounds() {
				t.Errorf("overlay bounds = %v, want %v", f.Image.Bounds(), in[0].Image.Bounds())
			}
		}
	}
	if outTotal != inTotal {
		t.Errorf("overlay changed the recording length from %v to %v", inTotal, outTotal)
	}
	if shownFrames == 0 {
		t.Error("no frame shows the key overlay")
	}
	if last := out[len(out)-1].Image; last != in[len(in)-1].Image {
		t.Error("last frame still shows keys after they faded; want the terminal image unchanged")
	}
}

// isInput reports whether img is one of the frames' own images, not a copy.
func isInput(frames []ExportFrame, img image.Image) bool {
	for _, f := range frames {
		if f.Image == img {
			return true
		}
	}
	return false
}
