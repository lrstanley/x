// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package sink

import (
	"image"
	"image/color"
	"image/draw"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel/sink/internal/gifopt"
	"github.com/lrstanley/x/term/reel/sink/internal/palette"
	"github.com/lrstanley/x/term/reel/sink/internal/testutil"
)

const (
	gifAddFrameBudget = 5500 * time.Microsecond // 5.5ms
)

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
	frameA := testutil.TerminalFrame(0)
	frameB := testutil.TerminalFrame(1)
	rec := &gifRecording{loopCount: 0}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; b.Loop(); i++ {
		frame := frameA
		if i&1 == 1 {
			frame = frameB
		}
		if err := appendRecordingFrame(rec, OptimizeFrames, nil, false, frame, time.Millisecond); err != nil {
			b.Fatal(err)
		}
	}

	b.StopTimer()
	assertBenchmarkBudget(b, gifAddFrameBudget)
}

func BenchmarkGIFAddFrameSteadyState(b *testing.B) {
	frameA := testutil.TerminalFrame(0)
	frameB := testutil.TerminalFrame(1)
	rec := &gifRecording{loopCount: 0}

	for i := range 16 {
		frame := frameA
		if i&1 == 1 {
			frame = frameB
		}
		if err := appendRecordingFrame(rec, OptimizeFrames, nil, false, frame, time.Millisecond); err != nil {
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
		if err := appendRecordingFrame(rec, OptimizeFrames, nil, false, frame, time.Millisecond); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGIFAddFrameAdaptive is BenchmarkGIFAddFrame with the default
// adaptive palette, after its colors are cached.
func BenchmarkGIFAddFrameAdaptive(b *testing.B) {
	frameA := testutil.TerminalFrame(0)
	frameB := testutil.TerminalFrame(1)
	rec := &gifRecording{adaptive: palette.NewAdaptive(true, false)}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; b.Loop(); i++ {
		frame := frameA
		if i&1 == 1 {
			frame = frameB
		}
		if err := appendRecordingFrame(rec, OptimizeFrames, nil, false, frame, time.Millisecond); err != nil {
			b.Fatal(err)
		}
	}

	b.StopTimer()
	assertBenchmarkBudget(b, gifAddFrameBudget)
}

func BenchmarkOptimizeDisposals(b *testing.B) {
	const w, h = testutil.TerminalWidth, testutil.TerminalHeight
	pal := testutil.GrayPalette()
	base := image.NewPaletted(image.Rect(0, 0, w, h), pal)
	for i := range base.Pix {
		base.Pix[i] = 16
	}
	blink := gifopt.Clone(base)
	for y := 400; y < 416; y++ {
		for x := 600; x < 616; x++ {
			blink.SetColorIndex(x, y, 196)
		}
	}
	frames := [8]*image.Paletted{}
	for i := range frames {
		if i%2 == 0 {
			frames[i] = base
		} else {
			frames[i] = blink
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		rec := &gifRecording{
			size:   image.Pt(w, h),
			images: make([]*image.Paletted, len(frames)),
			delays: make([]time.Duration, len(frames)),
		}
		for i, frame := range frames {
			rec.images[i] = gifopt.Clone(frame)
			rec.delays[i] = 50 * time.Millisecond
		}
		optimizeDisposals(rec)
	}
}

// benchmarkSources returns the terminal benchmark frame as each image type a
// caller may send, opaque and with a translucent background.
func benchmarkSources() []struct {
	name string
	img  image.Image
} {
	opaque := testutil.TerminalFrame(0)
	translucent := testutil.TerminalFrame(0)
	for i := 3; i < len(translucent.Pix); i += 4 {
		translucent.Pix[i] = 0x99
	}
	return []struct {
		name string
		img  image.Image
	}{
		{"NRGBA", opaque},
		{"RGBA", testutil.ToRGBA(opaque)},
		{"NRGBA-translucent", translucent},
		{"RGBA-translucent", testutil.ToRGBA(translucent)},
	}
}

// BenchmarkGIFAddFrameSource is the per-frame encoder cost for each source
// image type, against the frame budget at [MaxFrameRate].
func BenchmarkGIFAddFrameSource(b *testing.B) {
	for _, src := range benchmarkSources() {
		b.Run(src.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				rec := &gifRecording{}
				if err := appendRecordingFrame(rec, OptimizeFrames, nil, false, src.img, time.Millisecond); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(b.Elapsed())/float64(b.N)/float64(time.Second/MaxFrameRate)*100, "%budget")
		})
	}
}

// BenchmarkGIFAddFrameLimited is steady-state delta capture of a terminal that
// changes a small block per frame, without a limit and with each kind, so the
// cost of dropping the oldest frame on every append shows.
func BenchmarkGIFAddFrameLimited(b *testing.B) {
	const variants = 64
	frames := make([]*image.NRGBA, variants)
	for i := range frames {
		f := testutil.TerminalFrame(0)
		draw.Draw(f, image.Rect(100+i*8, 100, 108+i*8, 140), image.NewUniform(color.NRGBA{R: uint8(i * 3), G: 0x20, B: 0xc0, A: 0xff}), image.Point{}, draw.Src) //nolint:gosec // bounded.
		frames[i] = f
	}
	cases := []struct {
		name     string
		maxFrame int
		maxBytes int64
	}{
		{"unlimited", 0, 0},
		{"frames", 100, 0},
		{"bytes", 0, 4 << 20},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			rec := &gifRecording{adaptive: palette.NewAdaptive(true, false), maxFrames: tc.maxFrame, maxBytes: tc.maxBytes}
			add := func(i int) {
				if err := appendRecordingFrame(rec, OptimizeFrames|OptimizeDirtyRects, nil, false, frames[i%variants], time.Millisecond); err != nil {
					b.Fatal(err)
				}
			}
			for i := range 300 { // Reach the limit before timing.
				add(i)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 300; b.Loop(); i++ {
				add(i)
			}
			b.ReportMetric(float64(len(rec.images)), "kept")
		})
	}
}

// BenchmarkGIFAddFrameDense is BenchmarkGIFAddFrameAdaptive with a frame of
// short text-like runs instead of nearly flat fills.
func BenchmarkGIFAddFrameDense(b *testing.B) {
	frameA := testutil.SpanFrame(1, testutil.TerminalWidth, testutil.TerminalHeight, false, "text")
	frameB := testutil.SpanFrame(2, testutil.TerminalWidth, testutil.TerminalHeight, false, "text")
	rec := &gifRecording{adaptive: palette.NewAdaptive(true, false)}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; b.Loop(); i++ {
		frame := frameA
		if i&1 == 1 {
			frame = frameB
		}
		if err := appendRecordingFrame(rec, OptimizeFrames, nil, false, frame, time.Millisecond); err != nil {
			b.Fatal(err)
		}
	}
}
