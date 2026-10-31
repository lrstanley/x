// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/steep/snapshot"
)

// typeAndWait is a test helper that wraps the harness, whose actions the
// recording places at the line of the test that called it.
func typeAndWait(t *testing.T, h *Harness, text string) {
	t.Helper()
	h.Type(text).Wait("text=" + text)
}

// TestSource records sources and compares, with a snapshot, the source of
// each annotation and mark, which point at the lines of this test,
// including through the helper, in the timeline and in the exported
// ".cast". Lines here are in the snapshot, so edits to this file before the
// end of the test need it updated, with UPDATE_SNAPSHOTS=true.
func TestSource(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := NewHarness(&fakeTB{TB: t}, rootTestModel{}, exportOptions(dir,
		WithSource(true), WithRecordMessages(true), WithSettle(20*time.Millisecond),
		WithRecording(reel.Profile{Outputs: reel.MustFiles("rec.cast")}), WithExportAlways(),
	)...)
	typeAndWait(t, h, "ab")
	h.Mark("mine").Assert("missing")
	h.SendProgram(setTextMsg("x")).WaitMessage[setTextMsg]()
	h.Close()

	got := sourceListing(t, h.Timeline())
	snapshot.RequireEqual(t, got)

	// The export ends at the mark Close places, before the program exits.
	f, err := os.Open(filepath.Join(dir, "rec.cast"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	tl, err := reel.ReadTimeline(f)
	if err != nil {
		t.Fatal(err)
	}
	want, _, _ := strings.Cut(got, "steep.exit")
	if exported := sourceListing(t, tl); exported != want {
		t.Fatalf("exported sources:\n%s\nwant\n%s", exported, want)
	}

	findNote(t, notes(t, h.Timeline()), kindStart, map[string]any{
		"package": "github.com/lrstanley/x/term/steep",
		"module":  "github.com/lrstanley/x/term/steep",
		"dir":     ".",
	})
}

// sourceListing returns a line for each annotation and mark of tl, of its
// kind or label and its source, leaving out queries and messages, which
// have no source, as it checks.
func sourceListing(t *testing.T, tl *reel.Timeline) string {
	t.Helper()
	var b strings.Builder
	for ev := range tl.Events() {
		var src *reel.Source
		switch ev := ev.(type) {
		case reel.Mark:
			fmt.Fprintf(&b, "mark %-15s", ev.Label)
			src = ev.Source
		case reel.Annotation:
			if ev.Kind == reel.QueryKind || ev.Kind == kindMessage {
				// Neither comes from the test's goroutine.
				if ev.Source != nil {
					t.Errorf("%s annotation has a source: %+v", ev.Kind, ev.Source)
				}
				continue
			}
			fmt.Fprintf(&b, "%-20s", ev.Kind)
			src = ev.Source
		default:
			continue
		}
		if src != nil {
			fmt.Fprintf(&b, " %s:%d %s", src.File, src.Line, src.Func)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// TestSourceOff checks that sources are off by default.
func TestSourceOff(t *testing.T) {
	t.Parallel()
	h := NewHarness(t, rootTestModel{}, WithIgnoreEnv(true), WithWindowSize(20, 4))
	h.Mark("mine").Type("a").Wait("text=a")
	h.Close()
	for m := range h.Timeline().Marks() {
		if m.Source != nil {
			t.Fatalf("mark %s has a source without WithSource", m.Label)
		}
	}
	for a := range h.Timeline().Annotations() {
		if a.Source != nil {
			t.Fatalf("annotation %s has a source without WithSource", a.Kind)
		}
	}
}
