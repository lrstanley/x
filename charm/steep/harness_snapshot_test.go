// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"image"
	"image/color"
	"image/draw"
	"path/filepath"
	"sync"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/lrstanley/x/charm/steep/snapshot"
	"github.com/lrstanley/x/charm/still"
)

func TestHarnessAssertViewSnapshot(t *testing.T) {
	t.Parallel()

	snapDir := filepath.Join(t.TempDir(), "testdata")

	h := NewHarness(t, rootTestModel{text: "assert-snap"}, WithWindowSize(80, 3))

	h.WaitStrings([]string{"size=80x3", "text=assert-snap"})
	h.AssertSnapshot(snapshot.WithDir(snapDir), snapshot.WithUpdate(true))

	got := readSteepSnapshot(t, filepath.Join(snapDir, "TestHarnessAssertViewSnapshot.snap"))
	if got != "size=80x3\ntext=assert-snap\n" {
		t.Fatalf("snapshot = %q, want plain view text", got)
	}
}

func TestHarnessImageRendersLiveEmulatorState(t *testing.T) {
	t.Parallel()

	cursorColor := color.NRGBA{R: 0xff, A: 0xff}
	h := NewHarness(t, rootTestModel{text: "image"}, WithWindowSize(24, 3))
	h.WaitString("text=image")
	h.Blur()
	h.SetCursorColor(cursorColor)
	h.emulator.mu.Lock()
	if _, err := h.emulator.vt.WriteString(ansi.SetCursorStyle(5)); err != nil {
		t.Fatalf("set cursor style: %v", err)
	}
	h.emulator.mu.Unlock()

	var capturedState still.EmulatorState
	var capturedBounds image.Rectangle
	img := h.Image(
		still.WithPadding(2),
		still.WithBackgroundDrawer(func(ctx still.Context, dst draw.Image, area image.Rectangle) {
			capturedState = ctx.EmulatorState()
			capturedBounds = ctx.ScreenBounds()
			still.DrawBackground(ctx, dst, area)
		}),
	)
	if img.Bounds().Empty() {
		t.Fatal("Image() returned empty bounds")
	}
	if capturedBounds.Dx() != h.Width() || capturedBounds.Dy() != h.Height() {
		t.Fatalf("screen bounds = %v, want harness dimensions %dx%d", capturedBounds, h.Width(), h.Height())
	}
	if capturedState.Focused {
		t.Fatal("captured state focused = true, want false")
	}
	if capturedState.CursorStyle != uv.CursorBar {
		t.Fatalf("captured cursor style = %v, want CursorBar", capturedState.CursorStyle)
	}
	if capturedState.CursorColor != cursorColor {
		t.Fatalf("captured cursor color = %#v, want %#v", capturedState.CursorColor, cursorColor)
	}

	if got := h.Image().Bounds(); got != img.Bounds() {
		t.Fatalf("persistent image options bounds = %v, want %v", got, img.Bounds())
	}
}

func TestHarnessImageConcurrentWithTerminalMutation(t *testing.T) {
	t.Parallel()

	h := NewHarness(t, rootTestModel{}, WithWindowSize(24, 3))
	h.WaitString("size=24x3")

	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			_ = h.Image(
				still.WithPadding(i%3),
				still.WithNow(func() time.Time { return time.Unix(int64(i), 0) }),
			)
		}(i)
		go func() {
			defer wg.Done()
			h.Type("x")
		}()
	}
	wg.Wait()
}

func TestCursorShapeFromVT(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		style vt.CursorStyle
		want  uv.CursorShape
	}{
		"block":     {style: vt.CursorBlock, want: uv.CursorBlock},
		"underline": {style: vt.CursorUnderline, want: uv.CursorUnderline},
		"bar":       {style: vt.CursorBar, want: uv.CursorBar},
		"unknown":   {style: vt.CursorStyle(99), want: uv.CursorBlock},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := cursorShapeFromVT(tt.style); got != tt.want {
				t.Fatalf("cursorShapeFromVT(%v) = %v, want %v", tt.style, got, tt.want)
			}
		})
	}
}

func BenchmarkHarness_Image(b *testing.B) {
	h := NewHarness(b, rootTestModel{text: "benchmark"}, WithWindowSize(80, 24))
	h.WaitString("text=benchmark")

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		img := h.Image()
		if img.Bounds().Empty() {
			b.Fatal("empty image bounds")
		}
	}
}
