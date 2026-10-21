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

// BenchmarkGIFAddFrameAdaptive is BenchmarkGIFAddFrame with the default
// adaptive palette, after its colors are cached.
func BenchmarkGIFAddFrameAdaptive(b *testing.B) {
	frameA := terminalBenchmarkFrame(0)
	frameB := terminalBenchmarkFrame(1)
	rec := &gifRecording{adaptive: newAdaptivePalette(true)}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; b.Loop(); i++ {
		frame := frameA
		if i&1 == 1 {
			frame = frameB
		}
		if err := appendRecordingFrame(rec, nil, false, OptimizeFrames, nil, false, frame, time.Millisecond, 0); err != nil {
			b.Fatal(err)
		}
	}

	b.StopTimer()
	assertBenchmarkBudget(b, gifAddFrameBudget)
}

// BenchmarkAdaptivePaletteCold palettizes a frame of antialiased ramps into a
// fresh adaptive palette, so every color is new and median cut runs.
func BenchmarkAdaptivePaletteCold(b *testing.B) {
	src := terminalBenchmarkFrame(0)
	for i := 0; i < len(src.Pix); i += 4 {
		px := i / 4
		t := uint8(px % 251) //nolint:gosec // bounded.
		c := (px / 251) % 12
		src.Pix[i] = uint8(int(t) * (c*20 + 15) / 255)    //nolint:gosec // bounded.
		src.Pix[i+1] = uint8(int(t) * (255 - c*20) / 255) //nolint:gosec // bounded.
		src.Pix[i+2] = uint8(int(t)*(c*7+100)/255 + 30)   //nolint:gosec // bounded.
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		ap := newAdaptivePalette(true)
		dst := image.NewPaletted(src.Bounds(), ap.pal)
		ap.palettize(dst, src)
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

func BenchmarkOptimizeDisposals(b *testing.B) {
	const w, h = 1280, 864
	pal := defaultGIFPalette()
	base := image.NewPaletted(image.Rect(0, 0, w, h), pal)
	for i := range base.Pix {
		base.Pix[i] = 16
	}
	blink := clonePaletted(base)
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
			config: image.Config{Width: w, Height: h, ColorModel: pal},
			images: make([]*image.Paletted, len(frames)),
			delays: make([]time.Duration, len(frames)),
		}
		for i, frame := range frames {
			rec.images[i] = clonePaletted(frame)
			rec.delays[i] = 50 * time.Millisecond
		}
		optimizeDisposals(rec, nil, false)
	}
}

func BenchmarkPalettizeLUT(b *testing.B) {
	src := terminalBenchmarkFrame(0)
	pal := defaultGIFPalette()
	dst := image.NewPaletted(src.Bounds(), pal)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		palettizeInto(dst, src)
	}
}

// benchmarkSources returns the terminal benchmark frame as each image type a
// caller may send, opaque and with a translucent background.
func benchmarkSources() []struct {
	name string
	img  image.Image
} {
	opaque := terminalBenchmarkFrame(0)
	translucent := terminalBenchmarkFrame(0)
	for i := 3; i < len(translucent.Pix); i += 4 {
		translucent.Pix[i] = 0x99
	}
	return []struct {
		name string
		img  image.Image
	}{
		{"NRGBA", opaque},
		{"RGBA", toRGBA(opaque)},
		{"NRGBA-translucent", translucent},
		{"RGBA-translucent", toRGBA(translucent)},
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
				if err := appendRecordingFrame(rec, defaultGIFPalette(), true, OptimizeFrames, nil, false, src.img, time.Millisecond, 0); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(b.Elapsed())/float64(b.N)/float64(time.Second/MaxFrameRate)*100, "%budget")
		})
	}
}

// BenchmarkGIFAddFrameCustomPalette is BenchmarkGIFAddFrameSource with a
// custom palette, which skips the default palette lookup table.
func BenchmarkGIFAddFrameCustomPalette(b *testing.B) {
	pal := append(color.Palette(nil), defaultGIFPalette()[:128]...)
	for _, src := range benchmarkSources()[:2] {
		b.Run(src.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				rec := &gifRecording{}
				if err := appendRecordingFrame(rec, pal, false, OptimizeFrames, nil, false, src.img, time.Millisecond, 0); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
