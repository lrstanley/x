// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package export

import (
	"image"
	"image/color"
	"image/draw"
	"testing"
	"time"
)

const (
	terminalBenchmarkCols   = 80
	terminalBenchmarkRows   = 24
	terminalBenchmarkWidth  = 1280 // MustNew(20pt).Draw(80×24 screen)
	terminalBenchmarkHeight = 864
)

const (
	gifAddFrameBudget = 5500 * time.Microsecond // 5.5ms
)

// terminalBenchmarkFrame returns an NRGBA frame sized like still.MustNew(20pt)
// rendering an 80×24 terminal screen, with a gradient fill that exercises the LUT.
func terminalBenchmarkFrame(offset int) *image.NRGBA {
	w, h := terminalBenchmarkWidth, terminalBenchmarkHeight
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(((x+offset)*6/w)*40 + 16), //nolint:gosec // bounded.
				G: uint8(((y+offset)*6/h)*40 + 16), //nolint:gosec // bounded.
				B: 0x80,
				A: 0xff,
			})
		}
	}
	return img
}

// assertBenchmarkBudget fails b when average elapsed/op exceeds budget unless
// -short is set.
func assertBenchmarkBudget(b *testing.B, budget time.Duration) {
	b.Helper()
	if testing.Short() {
		return
	}
	perOp := b.Elapsed() / time.Duration(b.N)
	if perOp > budget {
		b.Fatalf("elapsed/op %v exceeds budget %v (total %v for %d ops)", perOp, budget, b.Elapsed(), b.N)
	}
}

func BenchmarkGIFAddFrame(b *testing.B) {
	frameA := terminalBenchmarkFrame(0)
	frameB := terminalBenchmarkFrame(1)
	rec := &gifRecording{loopCount: 0}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; b.Loop(); i++ {
		frame := frameA
		if i&1 == 1 {
			frame = frameB
		}
		if err := appendRecordingFrame(rec, defaultGIFPalette(), true, OptimizeFrames, nil, false, frame, time.Millisecond, 0); err != nil {
			b.Fatal(err)
		}
	}

	b.StopTimer()
	assertBenchmarkBudget(b, gifAddFrameBudget)
}

func BenchmarkGIFAddFrameSteadyState(b *testing.B) {
	frameA := terminalBenchmarkFrame(0)
	frameB := terminalBenchmarkFrame(1)
	rec := &gifRecording{loopCount: 0}

	for i := range 16 {
		frame := frameA
		if i&1 == 1 {
			frame = frameB
		}
		if err := appendRecordingFrame(rec, defaultGIFPalette(), true, OptimizeFrames, nil, false, frame, time.Millisecond, 0); err != nil {
			b.Fatal(err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; b.Loop(); i++ {
		frame := frameA
		if i&1 == 1 {
			frame = frameB
		}
		if err := appendRecordingFrame(rec, defaultGIFPalette(), true, OptimizeFrames, nil, false, frame, time.Millisecond, 0); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPalettizeDraw(b *testing.B) {
	src := terminalBenchmarkFrame(0)
	pal := defaultGIFPalette()
	dst := image.NewPaletted(src.Bounds(), pal)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		draw.Draw(dst, src.Bounds(), src, src.Bounds().Min, draw.Src)
	}
}

func BenchmarkPalettizeLUT(b *testing.B) {
	src := terminalBenchmarkFrame(0)
	pal := defaultGIFPalette()
	dst := image.NewPaletted(src.Bounds(), pal)
	initDefaultGIFLUT()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		palettizeNRGBA(dst, src)
	}
}
