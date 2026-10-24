// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/steep/snapshot"
	"github.com/lrstanley/x/term/still"
	"github.com/lrstanley/x/term/vt/screen"
)

func TestHarnessAssertViewSnapshot(t *testing.T) {
	t.Parallel()

	snapDir := filepath.Join(t.TempDir(), "testdata")

	h := NewHarness(t, rootTestModel{text: "assert-snap"}, WithWindowSize(80, 3))

	h.Wait([]string{"size=80x3", "text=assert-snap"})
	h.AssertSnapshot(snapshot.WithDir(snapDir), snapshot.WithUpdate(true))

	got := readSteepSnapshot(t, filepath.Join(snapDir, "TestHarnessAssertViewSnapshot.snap"))
	want := padScreen([]string{"size=80x3", "text=assert-snap", ""}, 80)
	if got != want {
		t.Fatalf("snapshot = %q, want full %dx3 screen", got, 80)
	}
}

// padScreen pads each line to width with spaces. An empty line is a blank row.
// Lines are joined without a trailing newline, matching [vt.Emulator.Render].
func padScreen(lines []string, width int) string {
	out := make([]string, len(lines))
	for i, line := range lines {
		if n := width - len(line); n > 0 {
			line += strings.Repeat(" ", n)
		}
		out[i] = line
	}
	return strings.Join(out, "\n")
}

func TestHarnessImageRendersLiveEmulatorState(t *testing.T) {
	t.Parallel()

	cursorColor := color.NRGBA{R: 0xff, A: 0xff}
	h := NewHarness(t, rootTestModel{text: "image"}, WithWindowSize(24, 3),
		WithRenderer(still.WithPadding(2)),
	)
	h.Wait("text=image")
	h.Blur()
	h.SetCursorColor(cursorColor)
	if err := h.emulator.writeString(ansi.SetCursorStyle(5)); err != nil {
		t.Fatalf("set cursor style: %v", err)
	}
	img := h.Image()
	if img.Bounds().Empty() {
		t.Fatal("Image() returned empty bounds")
	}
	screenBounds := h.Bounds()
	if screenBounds.Dx() != h.Width() || screenBounds.Dy() != h.Height() {
		t.Fatalf("screen bounds = %v, want harness dimensions %dx%d", screenBounds, h.Width(), h.Height())
	}
	capturedState := h.imageRenderer.GetEmulatorState()
	if capturedState == nil || capturedState.Focused {
		t.Fatal("emulator state focused = true, want false")
	}
	if capturedState.CursorStyle != screen.CursorBar {
		t.Fatalf("cursor style = %v, want CursorBar", capturedState.CursorStyle)
	}
	if capturedState.CursorColor != cursorColor {
		t.Fatalf("cursor color = %#v, want %#v", capturedState.CursorColor, cursorColor)
	}

	if got := h.Image().Bounds(); got != img.Bounds() {
		t.Fatalf("persistent image options bounds = %v, want %v", got, img.Bounds())
	}
}

func TestHarnessImageTracksEmulatorStateChanges(t *testing.T) {
	t.Parallel()

	h := NewHarness(t, rootTestModel{text: "state"}, WithWindowSize(24, 3))
	h.Wait("text=state")
	_ = h.Image()

	fg := color.NRGBA{R: 0x10, G: 0x20, B: 0x30, A: 0xff}
	bg := color.NRGBA{R: 0x01, G: 0x02, B: 0x03, A: 0xff}
	h.SetFgColor(fg).SetBgColor(bg).Blur()
	if err := h.emulator.writeString(ansi.SetWindowTitle("renamed")); err != nil {
		t.Fatalf("set title: %v", err)
	}
	_ = h.Image()
	st := h.imageRenderer.GetEmulatorState()
	if st == nil || st.Focused || st.Title != "renamed" || st.FgColor != fg || st.BgColor != bg {
		t.Fatalf("state = %#v", st)
	}

	fg = color.NRGBA{R: 0xaa, G: 0xbb, B: 0xcc, A: 0xff}
	h.SetFgColor(fg).Focus()
	_ = h.Image()
	st = h.imageRenderer.GetEmulatorState()
	if st == nil || !st.Focused || st.Title != "renamed" || st.FgColor != fg || st.BgColor != bg {
		t.Fatalf("updated state = %#v", st)
	}
}

// TestHarnessImageTracksProgress checks that an OSC 9;4 sequence the program
// writes reaches the renderer and the image, and that removing it clears the
// bar, without the grid changing.
func TestHarnessImageTracksProgress(t *testing.T) {
	t.Parallel()

	h := NewHarness(t, rootTestModel{text: "progress"}, WithWindowSize(24, 3), WithRenderer(still.WithWindowChrome(true)))
	h.Wait("text=progress")
	before := copyImage(t, h.Image())

	for _, tc := range []struct {
		seq  string
		want still.Progress
	}{
		{"\x1b]9;4;1;50\x07", still.Progress{State: still.ProgressSet, Percent: 50}},
		{"\x1b]9;4;2;70\x07", still.Progress{State: still.ProgressError, Percent: 70}},
		{"\x1b]9;4;3\x07", still.Progress{State: still.ProgressIndeterminate, Percent: -1}},
		{"\x1b]9;4;4\x07", still.Progress{State: still.ProgressPause, Percent: -1}},
	} {
		writeVT(t, h, tc.seq)
		img := copyImage(t, h.Image())
		if st := h.imageRenderer.GetEmulatorState(); st == nil || st.Progress != tc.want {
			t.Fatalf("%q: renderer progress = %+v, want %+v", tc.seq, st, tc.want)
		}
		if bytes.Equal(before.Pix, img.Pix) {
			t.Fatalf("%q: image did not change", tc.seq)
		}
	}

	writeVT(t, h, "\x1b]9;4;0\x07")
	if got := copyImage(t, h.Image()); !bytes.Equal(before.Pix, got.Pix) {
		t.Fatal("removing progress did not clear the bar")
	}
}

// statusHarness is a harness whose renderer draws program status: window
// chrome, and a margin for the outline.
func statusHarness(t *testing.T, text string) *Harness {
	t.Helper()
	h := NewHarness(t, rootTestModel{text: text}, WithWindowSize(30, 3), WithRenderer(
		still.WithWindowChrome(true), still.WithMargin(8, color.NRGBA{R: 21, G: 22, B: 28, A: 255}), still.WithBorderRadius(8),
	))
	h.Wait("text=" + text)
	return h
}

// TestHarnessImageTracksProgramStatus checks that OSC 7501 records the
// program writes reach the renderer and the image, and that clearing a record
// by id, clearing them all, a new prompt (OSC 133 A), and a full reset (RIS)
// each stop drawing what they drop.
func TestHarnessImageTracksProgramStatus(t *testing.T) {
	t.Parallel()

	h := statusHarness(t, "status")
	agentDone := ansi.ProgramStatus{State: ansi.ProgramStateDone, App: "agent", Title: "Refactor complete"}
	before := copyImage(t, h.Image())
	records := func() []still.ProgramStatus {
		t.Helper()
		_ = h.Image()
		return h.imageRenderer.GetEmulatorState().ProgramStatuses
	}

	writeVT(t, h, ansi.SetProgramStatus(agentDone))
	rootOnly := copyImage(t, h.Image())
	if bytes.Equal(before.Pix, rootOnly.Pix) {
		t.Fatal("a done record did not change the image")
	}
	want := still.ProgramStatus{State: still.ProgramDone, App: "agent", Title: "Refactor complete", Progress: -1}
	if got := records(); len(got) != 1 || got[0] != want {
		t.Fatalf("renderer records = %+v, want %+v", got, want)
	}

	writeVT(t, h, ansi.SetProgramStatus(ansi.ProgramStatus{State: ansi.ProgramStateWorking, ID: "job", Progress: 40, HasProgress: true, Title: "build"}))
	if got := records(); len(got) != 2 || got[1].ID != "job" || got[1].App != "agent" || got[1].Progress != 40 {
		t.Fatalf("renderer records = %+v, want the root and job, which inherits its app", got)
	}
	if bytes.Equal(rootOnly.Pix, copyImage(t, h.Image()).Pix) {
		t.Fatal("a child record did not change the image")
	}
	writeVT(t, h, ansi.ClearProgramStatusID("job"))
	if got := copyImage(t, h.Image()); !bytes.Equal(rootOnly.Pix, got.Pix) {
		t.Fatal("clearing the child by id did not restore the root-only image")
	}
	writeVT(t, h, ansi.ClearProgramStatus)
	if got := copyImage(t, h.Image()); !bytes.Equal(before.Pix, got.Pix) || records() != nil {
		t.Fatal("clearing every record did not restore the image")
	}

	writeVT(t, h, ansi.SetProgramStatus(ansi.ProgramStatus{State: ansi.ProgramStateWorking, ID: "w"})+
		ansi.SetProgramStatus(ansi.ProgramStatus{State: ansi.ProgramStateBlocked, ID: "b"})+
		ansi.SetProgramStatus(agentDone))
	writeVT(t, h, "\x1b]133;A\x1b\\")
	if got := records(); len(got) != 1 || got[0] != want {
		t.Fatalf("after OSC 133 A, renderer records = %+v, want only the done record", got)
	}
	if got := copyImage(t, h.Image()); !bytes.Equal(rootOnly.Pix, got.Pix) {
		t.Fatal("records dropped by OSC 133 A are still drawn")
	}

	writeVT(t, h, "\x1bc")
	if got := records(); got != nil {
		t.Fatalf("after RIS, renderer records = %+v", got)
	}
}

// TestHarnessImageProgramStatusHostileText checks that a title and message
// with bidi overrides and invisible characters draw like the visible text.
func TestHarnessImageProgramStatusHostileText(t *testing.T) {
	t.Parallel()

	h := statusHarness(t, "hostile")
	writeVT(t, h, ansi.SetProgramStatus(ansi.ProgramStatus{State: ansi.ProgramStateError, Title: "Run \u202egnp.exe\u202c now?\u200b", Message: "Open \u2066invoice\u2069\u200d.pdf\u00ad"}))
	hostile := copyImage(t, h.Image())
	writeVT(t, h, ansi.SetProgramStatus(ansi.ProgramStatus{State: ansi.ProgramStateError, Title: "Run gnp.exe now?", Message: "Open invoice.pdf"}))
	if clean := copyImage(t, h.Image()); !bytes.Equal(hostile.Pix, clean.Pix) {
		t.Fatal("hostile text draws differently from the visible text")
	}
}

// TestHarnessProgramStatusStopsProgress checks the OSC 7501 rule for OSC 9;4:
// once any program status report arrives, even a clear, progress is no longer
// drawn, until a full reset.
func TestHarnessProgramStatusStopsProgress(t *testing.T) {
	t.Parallel()

	h := statusHarness(t, "rule")
	progress := func() still.Progress {
		t.Helper()
		_ = h.Image()
		return h.imageRenderer.GetEmulatorState().Progress
	}
	set := func(pct int) still.Progress { return still.Progress{State: still.ProgressSet, Percent: pct} }

	writeVT(t, h, "\x1b]9;4;1;50\x07")
	if got := progress(); got != set(50) {
		t.Fatalf("before OSC 7501, progress = %+v", got)
	}
	writeVT(t, h, ansi.ClearProgramStatus)
	if got := progress(); got != (still.Progress{}) {
		t.Fatalf("after an OSC 7501 report, progress = %+v, want none", got)
	}
	writeVT(t, h, "\x1b]9;4;1;70\x07")
	if got := progress(); got != (still.Progress{}) {
		t.Fatalf("OSC 9;4 after OSC 7501 drew progress %+v", got)
	}
	writeVT(t, h, "\x1bc\x1b]9;4;1;30\x07")
	if got := progress(); got != set(30) {
		t.Fatalf("after a full reset, progress = %+v, want %+v", got, set(30))
	}
}

func TestHarnessImageFollowsGridGeneration(t *testing.T) {
	t.Parallel()

	h := NewHarness(t, rootTestModel{text: "one"}, WithWindowSize(20, 2))
	h.Wait("text=one")
	first := copyImage(t, h.Image())
	h.SendProgram(setTextMsg("two")).Wait("text=two")
	second := copyImage(t, h.Image())
	if bytes.Equal(first.Pix, second.Pix) {
		t.Fatal("image unchanged after screen update")
	}
	third := copyImage(t, h.Image())
	if !bytes.Equal(second.Pix, third.Pix) {
		t.Fatal("image changed without a screen update")
	}
}

func copyImage(t *testing.T, src image.Image) *image.NRGBA {
	t.Helper()
	if src == nil || src.Bounds().Empty() {
		t.Fatal("empty image")
	}
	b := src.Bounds()
	dst := image.NewNRGBA(b)
	draw.Draw(dst, b, src, b.Min, draw.Src)
	return dst
}

func TestHarnessImageConcurrentWithTerminalMutation(t *testing.T) {
	t.Parallel()

	h := NewHarness(t, rootTestModel{}, WithWindowSize(24, 3))
	h.Wait("size=24x3")
	// Steady cursor: blink phase is time-dependent and this test cannot use synctest
	// (VT I/O runs outside the bubble).
	if err := h.emulator.writeString(ansi.SetCursorStyle(2)); err != nil {
		t.Fatalf("set steady cursor: %v", err)
	}

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = h.Image()
		}()
		go func() {
			defer wg.Done()
			h.Type("x")
		}()
	}
	wg.Wait()
}

func BenchmarkHarness_Image(b *testing.B) {
	h := NewHarness(b, rootTestModel{text: "benchmark"}, WithWindowSize(80, 24))
	h.Wait("text=benchmark")

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		img := h.Image()
		if img.Bounds().Empty() {
			b.Fatal("empty image bounds")
		}
	}
}
