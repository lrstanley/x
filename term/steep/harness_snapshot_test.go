// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"image/color"
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
