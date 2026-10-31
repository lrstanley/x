// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInspector(t *testing.T) {
	files, _ := filepath.Glob("../../testdata/*.cast")
	if extra := os.Getenv("REEL_CAST"); extra != "" {
		files = append(files, extra)
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			x, err := load(t.Context(), data, 1)
			if err != nil {
				t.Skipf("load: %v", err)
			}
			t.Cleanup(func() { _ = x.player.Close() })
			s := x.summary()
			if _, err = json.Marshal(s); err != nil {
				t.Fatal(err)
			}
			for _, i := range []int{len(s.Events) - 1, len(s.Events) / 2, -1} {
				if err = x.seek(i); err != nil {
					t.Fatal(err)
				}
				if _, err = json.Marshal(x.frame(true)); err != nil { //nolint:musttag // fields are tagged; vt types marshal by name
					t.Fatal(err)
				}
				if _, err = x.player.Image(); err != nil {
					t.Fatal(err)
				}
			}
			for i := range s.Events {
				if _, err = x.detail(i); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// TestInspectorSources checks that marks and annotations keep the sources a
// steep test recorded, and marks their category.
func TestInspectorSources(t *testing.T) {
	data, err := os.ReadFile("../public/samples/failing-wait.cast")
	if errors.Is(err, fs.ErrNotExist) {
		t.Skip("no samples; bun run samples records them")
	}
	if err != nil {
		t.Fatal(err)
	}
	x, err := load(t.Context(), data, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = x.player.Close() })
	var mark, failure bool
	for _, e := range x.summary().Events {
		switch {
		case e.Type == "mark" && e.Label == "failure:1":
			mark = e.Category == "failure" && e.Source != nil
		case e.Kind == "steep.failure":
			failure = e.Source != nil && e.Source.Line > 0
		}
	}
	if !mark || !failure {
		t.Errorf("failure mark has category and source: %v, failure annotation has source: %v", mark, failure)
	}
}

// TestDetectDisplay checks the parts a steep test's table turns on: window
// chrome for its title, and no scrollbar, as it draws on the alternate
// screen.
func TestDetectDisplay(t *testing.T) {
	data, err := os.ReadFile("../public/samples/failing-wait.cast")
	if errors.Is(err, fs.ErrNotExist) {
		t.Skip("no samples; bun run samples records them")
	}
	if err != nil {
		t.Fatal(err)
	}
	x, err := load(t.Context(), data, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = x.player.Close() })
	if !x.displayFor.Chrome || x.displayReason["chrome"] == "" {
		t.Errorf("chrome = %v (%q), want on for the title", x.displayFor.Chrome, x.displayReason["chrome"])
	}
	if x.displayFor.Scrollbar {
		t.Errorf("scrollbar on (%q), want off on the alternate screen", x.displayReason["scrollbar"])
	}
	layout, err := x.player.Layout()
	if err != nil {
		t.Fatal(err)
	}
	if err = x.setDisplay(display{}); err != nil {
		t.Fatal(err)
	}
	bare, err := x.player.Layout()
	if err != nil {
		t.Fatal(err)
	}
	if bare.Grid.Min.Y >= layout.Grid.Min.Y {
		t.Errorf("grid at %v without chrome, want above %v", bare.Grid.Min, layout.Grid.Min)
	}
	// Chrome brings padding, which moves the grid in from the left too.
	if bare.Grid.Min.X != 0 || layout.Grid.Min.X != chromePadding {
		t.Errorf("grid x = %d with chrome, %d without, want %d and 0", layout.Grid.Min.X, bare.Grid.Min.X, chromePadding)
	}
}

func TestTokenize(t *testing.T) {
	var got []string
	tokenize([]byte("hi\x1b[1;31mred\x1b]8;;http://x\x1b\\link\x07\x1b[?2026h"), func(_ int, tok token) {
		got = append(got, tok.Tag+"|"+tok.Name+"|"+tok.Raw)
	})
	exp := []string{
		"||hi", "SGR|SGR|\\e[1;31m", "||red", "OSC 8|OSC 8|\\e]8;;http://x\\e\\\\",
		"||link", "BEL|BEL|\\x07", "MODE|DECSET|\\e[?2026h",
	}
	if len(got) != len(exp) {
		t.Fatalf("got %q, want %q", got, exp)
	}
	for i := range exp {
		if got[i] != exp[i] {
			t.Errorf("token %d = %q, want %q", i, got[i], exp[i])
		}
	}
}

// TestScaleAndPrepare checks that frames grow with their scale, and that a
// cast quick to replay gets no checkpoints to prepare.
func TestScaleAndPrepare(t *testing.T) {
	data, err := os.ReadFile("../public/samples/failing-wait.cast")
	if errors.Is(err, fs.ErrNotExist) {
		t.Skip("no samples; bun run samples records them")
	}
	if err != nil {
		t.Fatal(err)
	}
	x, err := load(t.Context(), data, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = x.player.Close() })
	small, err := x.player.Image()
	if err != nil {
		t.Fatal(err)
	}
	width := small.Rect.Dx()
	if err = x.setScale(2); err != nil {
		t.Fatal(err)
	}
	large, err := x.player.Image()
	if err != nil {
		t.Fatal(err)
	}
	if large.Rect.Dx() < 2*width-2 {
		t.Errorf("width %d at scale 2, want about twice %d", large.Rect.Dx(), width)
	}
	more, err := x.prepare(time.Second)
	if err != nil || more || x.checkpoints != 0 {
		t.Errorf("prepare = %v, %v with %d checkpoints, want nothing to do", more, err, x.checkpoints)
	}

	// A slow pace plans checkpoints, about one per checkpointSpan of replay,
	// and a faster one later keeps them.
	x.replayed, x.replayTime = minTimed, minTimed*checkpointSpan/2
	want := min(maxCheckpoints, x.player.Len()/2)
	if got := x.planCheckpoints(); got != want {
		t.Errorf("planCheckpoints at 25ms an event = %d, want %d", got, want)
	}
	x.checkpoints = want
	x.replayTime = time.Millisecond
	if got := x.planCheckpoints(); got != want {
		t.Errorf("planCheckpoints after a faster pace = %d, want it kept at %d", got, want)
	}
}
