// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/steep/snapshot"
)

// note is a steep annotation, decoded.
type note struct {
	kind   string
	fields map[string]any
}

func (n note) String() string { return n.kind }

// notes returns the steep annotations of tl, oldest first, with the kinds
// in skip left out.
func notes(t *testing.T, tl *reel.Timeline, skip ...string) []note {
	t.Helper()
	var out []note
	for a := range tl.Annotations() {
		if !strings.HasPrefix(a.Kind, "steep.") || slices.Contains(skip, a.Kind) {
			continue
		}
		n := note{kind: a.Kind}
		if err := a.Decode(&n.fields); err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	return out
}

// findNote returns the first of notes of kind whose fields hold every one
// of want, failing the test when there is none.
func findNote(t *testing.T, notes []note, kind string, want map[string]any) note {
	t.Helper()
	for _, n := range notes {
		if n.kind != kind {
			continue
		}
		ok := true
		for k, v := range want {
			if !equalJSON(n.fields[k], v) {
				ok = false
			}
		}
		if ok {
			return n
		}
	}
	t.Fatalf("no %s annotation with %v in %+v", kind, want, notes)
	return note{}
}

// equalJSON reports whether a, a decoded JSON value, is b, with Go numbers
// compared as JSON numbers.
func equalJSON(a, b any) bool {
	switch b := b.(type) {
	case int:
		f, ok := a.(float64)
		return ok && f == float64(b)
	case []any:
		as, ok := a.([]any)
		if !ok || len(as) != len(b) {
			return false
		}
		for i := range b {
			if !equalJSON(as[i], b[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		am, ok := a.(map[string]any)
		if !ok || len(am) != len(b) {
			return false
		}
		for k, v := range b {
			if !equalJSON(am[k], v) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}

func kinds(notes []note) []string {
	out := make([]string, len(notes))
	for i, n := range notes {
		out[i] = n.kind
	}
	return out
}

// runFailing runs fn on a goroutine of its own, which a fatal failure of a
// [fakeTB] ends, and waits for it.
func runFailing(fn func()) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	<-done
}

// TestAnnotations runs a test through the harness's actions and checks the
// annotations it records, in order, and that the exported ".cast" holds
// them, the result last, just before the mark Close places.
func TestAnnotations(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ftb := &fakeTB{TB: t}
	h := NewHarness(ftb, rootTestModel{}, exportOptions(dir,
		WithRecording(reel.Profile{Outputs: reel.Files("rec.cast")}),
		WithExportAlways(),
		WithSettle(20*time.Millisecond),
	)...)
	h.Wait("size=20x4").
		Type("ab").Key("enter").Paste("p").
		LeftClick(1, 2).LeftDrag(0, 0, 2, 1).
		Assert([]string{"size", "text=ab"}).
		Assert("missing").
		RequireNot("missing").
		AssertMatch(`text=\w+`).
		AssertDimensions(20, 4).
		SendProgram(setTextMsg("x")).WaitMessage[setTextMsg]()
	h.AssertHasMessage[setTextMsg]()
	h.SetClipboard("clip").SetScrollbackSize(10).ClearScrollback().
		Mutate(func(m rootTestModel) rootTestModel { return m }).
		Sleep(time.Millisecond).
		QuitProgram().
		WaitFinished()
	h.Close()

	got := notes(t, h.Timeline(), kindExit)
	want := []string{
		kindStart, kindReady,
		kindWaitStart, kindWaitEnd,
		kindInput, kindInput, kindInput, kindInput, kindInput,
		kindAssert,
		kindFailure, kindAssert,
		kindAssert, kindAssert, kindAssert,
		kindProgram, kindWaitStart, kindWaitEnd,
		kindAssert,
		kindTerminal, kindTerminal, kindTerminal,
		kindProgram,
		kindWaitStart, kindWaitEnd,
		kindProgram,
		kindWaitStart, kindWaitEnd,
		kindResult,
	}
	if !slices.Equal(kinds(got), want) {
		t.Fatalf("annotations:\n%q\nwant\n%q", kinds(got), want)
	}

	findNote(t, got, kindStart, map[string]any{
		"test": t.Name(), "cols": 20, "rows": 4, "timeout_ms": 2000, "check_ms": 10,
		"settle_ms": 20, "ansi": true, "export": "always",
	})
	findNote(t, got, kindReady, map[string]any{"outcome": "ready"})
	findNote(t, got, kindWaitStart, map[string]any{"id": 1, "wait": "text", "text": []any{"size=20x4"}, "timeout_ms": 2000})
	findNote(t, got, kindWaitEnd, map[string]any{
		"id": 1, "wait": "text", "outcome": "satisfied",
		"matches": []any{map[string]any{"text": "size=20x4", "row": 0, "col": 0}},
	})
	findNote(t, got, kindInput, map[string]any{"action": "type", "text": "ab"})
	findNote(t, got, kindInput, map[string]any{"action": "key", "key": "enter"})
	findNote(t, got, kindInput, map[string]any{"action": "paste", "text": "p"})
	findNote(t, got, kindInput, map[string]any{"action": "click", "button": "left", "at": map[string]any{"row": 2, "col": 1}})
	findNote(t, got, kindInput, map[string]any{
		"action": "drag", "button": "left",
		"at": map[string]any{"row": 0, "col": 0}, "to": map[string]any{"row": 1, "col": 2},
	})
	findNote(t, got, kindAssert, map[string]any{
		"assert": "contains", "required": false, "pass": true, "text": []any{"size", "text=ab"},
		"matches": []any{
			map[string]any{"text": "size", "row": 0, "col": 0},
			map[string]any{"text": "text=ab", "row": 1, "col": 0},
		},
	})
	findNote(t, got, kindFailure, map[string]any{})
	findNote(t, got, kindAssert, map[string]any{"assert": "contains", "pass": false, "text": []any{"missing"}})
	findNote(t, got, kindAssert, map[string]any{"assert": "not_contains", "required": true, "pass": true})
	findNote(t, got, kindAssert, map[string]any{
		"assert": "match", "pass": true, "pattern": `text=\w+`,
		"matches": []any{map[string]any{"text": "text=ab", "row": 1, "col": 0}},
	})
	findNote(t, got, kindAssert, map[string]any{
		"assert": "dimensions", "pass": true,
		"expected": map[string]any{"width": 20, "height": 4},
		"actual":   map[string]any{"width": 20, "height": 4},
	})
	findNote(t, got, kindProgram, map[string]any{"action": "send", "type": "steep.setTextMsg", "value": "x"})
	findNote(t, got, kindWaitEnd, map[string]any{"wait": "message", "outcome": "satisfied", "type": "steep.setTextMsg"})
	findNote(t, got, kindAssert, map[string]any{"assert": "has_message", "pass": true, "type": "steep.setTextMsg"})
	findNote(t, got, kindTerminal, map[string]any{"action": "clipboard", "text": "clip"})
	findNote(t, got, kindTerminal, map[string]any{"action": "scrollback_size", "lines": 10})
	findNote(t, got, kindTerminal, map[string]any{"action": "clear_scrollback"})
	findNote(t, got, kindProgram, map[string]any{"action": "mutate", "type": "steep.rootTestModel"})
	findNote(t, got, kindWaitStart, map[string]any{"wait": "sleep", "duration_ms": 1})
	findNote(t, got, kindProgram, map[string]any{"action": "quit"})
	findNote(t, got, kindWaitEnd, map[string]any{"wait": "finished", "outcome": "satisfied"})
	findNote(t, got, kindResult, map[string]any{"test": t.Name(), "failed": true, "failures": 1})
	findNote(t, notes(t, h.Timeline()), kindExit, map[string]any{})

	// The result comes just before the mark Close places, the third close
	// after QuitProgram's and WaitFinished's, which the recording ends on.
	events := slices.Collect(h.Timeline().Events())
	result := slices.IndexFunc(events, func(ev reel.Event) bool {
		a, ok := ev.(reel.Annotation)
		return ok && a.Kind == kindResult
	})
	if m, ok := events[result+1].(reel.Mark); !ok || m.Label != "close-3" {
		t.Fatalf("event after the result = %+v, want the close-3 mark", events[result+1])
	}

	// The exported recording holds every annotation up to the mark.
	f, err := os.Open(filepath.Join(dir, "rec.cast"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	tl, err := reel.ReadTimeline(f)
	if err != nil {
		t.Fatal(err)
	}
	exported := notes(t, tl, kindExit)
	if !slices.Equal(kinds(exported), want) {
		t.Fatalf("exported annotations:\n%q\nwant\n%q", kinds(exported), want)
	}
	var queries int
	for a := range tl.Annotations() {
		if a.Kind == reel.QueryKind {
			queries++
		}
	}
	if queries == 0 {
		t.Fatal("exported recording has none of the program's queries")
	}
}

// TestAnnotationsWaitOutcomes checks the end of a wait that fails: a
// timeout, a cancelation, and another failure, each after the failure it
// reported.
func TestAnnotationsWaitOutcomes(t *testing.T) {
	t.Parallel()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		name    string
		wait    func(h *Harness)
		outcome string
	}{
		{"timeout", func(h *Harness) { h.Wait("never", WithTimeout(30*time.Millisecond)) }, outcomeTimeout},
		{"canceled", func(h *Harness) { h.WaitSettle(WithContext(canceled)) }, outcomeCanceled},
		{"failed", func(h *Harness) { h.WaitMatch("(") }, outcomeFailed},
		{"message timeout", func(h *Harness) { h.WaitLiveMessage[setTextMsg](WithTimeout(30 * time.Millisecond)) }, outcomeTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ftb := &fakeTB{TB: t}
			h := NewHarness(ftb, rootTestModel{}, WithWindowSize(20, 4), WithIgnoreEnv(true))
			runFailing(func() { tc.wait(h) })
			got := notes(t, h.Timeline())
			n := len(got)
			if n < 3 || got[n-3].kind != kindWaitStart || got[n-2].kind != kindFailure || got[n-1].kind != kindWaitEnd {
				t.Fatalf("annotations = %q, want a wait's start, a failure, and its end", kinds(got))
			}
			findNote(t, got[n-2:], kindFailure, map[string]any{})
			findNote(t, got[n-1:], kindWaitEnd, map[string]any{"outcome": tc.outcome})
		})
	}
}

// TestAnnotationsRequire checks that a requirement that fails is recorded
// before it stops the test.
func TestAnnotationsRequire(t *testing.T) {
	t.Parallel()
	ftb := &fakeTB{TB: t}
	h := NewHarness(ftb, rootTestModel{}, WithWindowSize(20, 4), WithIgnoreEnv(true))
	h.Wait("size=20x4")
	stopped := true
	runFailing(func() {
		h.RequireHeight(9)
		stopped = false
	})
	if !stopped {
		t.Fatal("RequireHeight did not stop the test")
	}
	got := notes(t, h.Timeline())
	findNote(t, got, kindAssert, map[string]any{
		"assert": "height", "required": true, "pass": false,
		"expected": map[string]any{"height": 9},
		"actual":   map[string]any{"width": 20, "height": 4},
	})
}

func TestAnnotationsSnapshot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stale := filepath.Join(dir, "TestAnnotationsSnapshot.02-b.snap")
	if err := os.WriteFile(stale, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ftb := &fakeTB{TB: t}
	h := NewHarness(ftb, rootTestModel{}, WithWindowSize(20, 2), WithIgnoreEnv(true))
	h.Wait("size=20x2").
		AssertSnapshot(snapshot.WithDir(dir), snapshot.WithSuffix("a"), snapshot.WithUpdate(true)).
		AssertJSON(snapshot.WithDir(dir), snapshot.WithSuffix("missing")).
		AssertSnapshot(snapshot.WithDir(dir), snapshot.WithSuffix("b"))

	got := notes(t, h.Timeline())
	findNote(t, got, kindSnapshot, map[string]any{
		"format": "text", "path": filepath.Join(dir, "TestAnnotationsSnapshot-a.snap"),
		"required": false, "pass": true, "updated": true,
	})
	findNote(t, got, kindSnapshot, map[string]any{
		"format": "json", "path": filepath.Join(dir, "TestAnnotationsSnapshot-missing.json"),
		"required": false, "pass": false, "missing": true,
	})
	n := findNote(t, got, kindSnapshot, map[string]any{"format": "text", "path": stale, "pass": false})
	if diff, _ := n.fields["diff"].(string); !strings.Contains(diff, "-old") || !strings.Contains(diff, "+size=20x2") {
		t.Fatalf("diff = %q", diff)
	}
}

func TestAnnotationsMessages(t *testing.T) {
	t.Parallel()
	h := NewHarness(t, rootTestModel{}, WithWindowSize(20, 2), WithIgnoreEnv(true))
	h.SendProgram(setTextMsg("x")).Wait("text=x")
	if got := notes(t, h.Timeline()); slices.Contains(kinds(got), kindMessage) {
		t.Fatal("recorded messages without WithRecordMessages")
	}

	h = NewHarness(t, rootTestModel{}, WithWindowSize(20, 2), WithIgnoreEnv(true), WithRecordMessages(true))
	h.SendProgram(setTextMsg("x")).Wait("text=x")
	got := notes(t, h.Timeline())
	findNote(t, got, kindStart, map[string]any{"messages": true})
	findNote(t, got, kindMessage, map[string]any{"type": "tea.WindowSizeMsg", "value": "{Width:20 Height:2}"})
	findNote(t, got, kindMessage, map[string]any{"type": "steep.setTextMsg", "value": "x"})
}

func TestAnnotationsExit(t *testing.T) {
	t.Parallel()
	ftb := &fakeTB{TB: t}
	h := NewHarness(ftb, rootTestModel{}, WithWindowSize(20, 2), WithIgnoreEnv(true))
	h.Wait("size=20x2")
	h.program.Kill()
	runFailing(func() { h.WaitFinished() })
	findNote(t, notes(t, h.Timeline()), kindExit, map[string]any{"killed": true})
}

func TestMatchAt(t *testing.T) {
	t.Parallel()
	view := "ab\n日本x"
	if got := matchAt(view, strings.Index(view, "x"), len(view)); got != (match{Text: "x", cell: cell{Row: 1, Col: 4}}) {
		t.Fatalf("matchAt = %+v", got)
	}
	if got := patternMatches(view, `b\n日`); len(got) != 1 || got[0].cell != (cell{Row: 0, Col: 1}) {
		t.Fatalf("patternMatches = %+v", got)
	}
}

func TestClip(t *testing.T) {
	t.Parallel()
	var truncated bool
	if got := clip("ok", &truncated); got != "ok" || truncated {
		t.Fatalf("clip = %q, %v", got, truncated)
	}
	long := strings.Repeat("é", noteTextMax)
	got := clip(long, &truncated)
	if !truncated || len(got) > noteTextMax || !strings.HasPrefix(long, got) || len(got)%2 != 0 {
		t.Fatalf("clip of %d bytes = %d bytes, truncated %v", len(long), len(got), truncated)
	}
}

// TestAnnotationSchema checks an example of each kind the harness records
// against reel's schema, and that the schema rejects what it must.
func TestAnnotationSchema(t *testing.T) {
	t.Parallel()
	one, two := 1, 2
	valid := map[string][]any{
		kindStart: {newStartNote("TestX", defaultOptions().validate())},
		kindReady: {readyNote{Outcome: "ready", ElapsedMS: 3}},
		kindInput: {
			inputNote{Action: "type", Text: "hi"},
			inputNote{Action: "key", Key: "ctrl+c"},
			inputNote{Action: "mouse", Mouse: "press", Button: "left", At: &cell{Row: 1, Col: 2}},
			inputNote{Action: "drag", Button: "right", At: &cell{}, To: &cell{Row: 3, Col: 4}},
		},
		kindTerminal: {terminalNote{Action: "scrollback_size", Lines: new(0)}, terminalNote{Action: "clipboard", Text: "x", Truncated: true}},
		kindProgram:  {programNote{Action: "send", Type: "tea.KeyPressMsg", Value: "a"}, programNote{Action: "quit"}},
		kindWaitStart: {
			waitNote{ID: 1, Wait: "text", Text: []string{"a"}, TimeoutMS: 100},
			waitNote{ID: 2, Wait: "message", Type: "tea.Msg", Live: true, TimeoutMS: 100},
			waitNote{ID: 3, Wait: "sleep", DurationMS: 5},
		},
		kindWaitEnd: {waitEndNote{ID: 1, Wait: "match", Outcome: "satisfied", Matches: []match{{Text: "a", cell: cell{Row: 0, Col: 1}}}}},
		kindAssert: {
			assertNote{Assert: "contains", Pass: true, Text: []string{"a"}},
			assertNote{Assert: "width", Required: true, Expected: &dims{Width: &one}, Actual: &dims{Width: &two, Height: &one}},
			assertNote{Assert: "has_message", Type: "tea.QuitMsg"},
		},
		kindSnapshot: {snapshotNote{Format: "text", Path: "testdata/x.snap", Diff: "-a\n+b"}},
		kindFailure:  {failureNote{Message: "boom"}},
		kindMessage:  {messageNote{Type: "tea.QuitMsg", Value: "{}"}},
		kindExit:     {exitNote{}, exitNote{Error: "bad", Killed: true}},
		kindResult:   {resultNote{Test: "TestX", Failed: true, Panicked: true, Failures: 0, DurationMS: 9, Stack: "panic(...)"}},
	}
	// Values and fields a newer steep may write.
	valid[kindReady] = append(valid[kindReady], readyNote{Outcome: "late"})
	valid[kindInput] = append(valid[kindInput], map[string]any{"action": "key", "key": "a", "extra": true})
	valid[kindWaitEnd] = append(valid[kindWaitEnd], waitEndNote{ID: 1, Wait: "func", Outcome: "maybe"})
	valid[kindSnapshot] = append(valid[kindSnapshot], snapshotNote{Format: "png", Path: "x"})
	for kind, examples := range valid {
		for _, v := range examples {
			if _, err := reel.NewAnnotation(kind, v); err != nil {
				t.Errorf("%s %+v: %v", kind, v, err)
			}
		}
	}

	for name, tc := range map[string]struct {
		kind string
		v    any
	}{
		"start without test":      {kindStart, map[string]any{"cols": 1, "rows": 1, "timeout_ms": 1, "check_ms": 1, "settle_ms": 1, "ansi": true, "export": "off"}},
		"input click without at":  {kindInput, inputNote{Action: "click", Button: "left"}},
		"terminal size no lines":  {kindTerminal, terminalNote{Action: "scrollback_size"}},
		"program send no type":    {kindProgram, programNote{Action: "send"}},
		"wait start bad id":       {kindWaitStart, waitNote{Wait: "func"}},
		"wait text without text":  {kindWaitStart, waitNote{ID: 1, Wait: "text"}},
		"assert dims no actual":   {kindAssert, assertNote{Assert: "height", Expected: &dims{Height: &one}}},
		"failure without message": {kindFailure, map[string]any{"text": "x"}},
		"message without value":   {kindMessage, map[string]any{"type": "x"}},
		"exit killed false":       {kindExit, map[string]any{"killed": false}},
		"result negative failure": {kindResult, resultNote{Test: "x", Failures: -1}},
	} {
		if _, err := reel.NewAnnotation(tc.kind, tc.v); !errors.Is(err, reel.ErrInvalidAnnotation) {
			t.Errorf("%s: NewAnnotation = %v, want ErrInvalidAnnotation", name, err)
		}
	}
}
