// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"os"
	"testing"
	"time"
)

func TestMustGIFFrameCount(t *testing.T) {
	t.Parallel()

	frame := 0
	bounds := image.Rect(0, 0, 8, 8)

	path := t.TempDir() + "/out.gif"
	closer := MustGIF(120, path, func() image.Image {
		img := image.NewNRGBA(bounds)
		solidFrameInto(img, frame%4)
		frame++
		return img
	})

	time.Sleep(25 * time.Millisecond)

	time.Sleep(240 * time.Millisecond)

	closer()

	g, err := gifDecodeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(g.Image); n < 10 {
		t.Fatalf("got %d frames, want at least 10 distinct states captured", n)
	}
}

func TestMustGIFDedupesIdle(t *testing.T) {
	t.Parallel()

	path := t.TempDir() + "/idle.gif"
	closer := MustGIF(120, path, func() image.Image {
		return solidFrame(0, 4, 4)
	})

	time.Sleep(100 * time.Millisecond)
	closer()

	g, err := gifDecodeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) != 1 {
		t.Fatalf("got %d frames, want 1 deduped idle frame", len(g.Image))
	}
	if g.Delay[0] < 5 {
		t.Fatalf("delay = %d cs, want accumulated idle time", g.Delay[0])
	}
}

func TestMustGIFDistinctAfterQuantize(t *testing.T) {
	t.Parallel()

	a := solidFrame(1, 8, 8)
	b := solidFrame(2, 8, 8)
	pal := terminalGIFPalette()
	pa := palettize(a, pal)
	pb := palettize(b, pal)
	if bytes.Equal(pa.Pix, pb.Pix) {
		t.Fatal("expected distinct paletted output")
	}
}

func solidFrame(c, w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	solidFrameInto(img, c)
	return img
}

func solidFrameInto(img *image.NRGBA, c int) {
	col := color.NRGBA{R: uint8(c * 40), A: 0xff}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			img.SetNRGBA(x, y, col)
		}
	}
}

func palettize(src *image.NRGBA, pal color.Palette) *image.Paletted {
	pm := image.NewPaletted(src.Bounds(), pal)
	palettizeInto(pm, src)
	return pm
}

func BenchmarkPalettizeDraw(b *testing.B) {
	src := solidTerminalFrame(640, 480)
	pal := terminalGIFPalette()
	dst := image.NewPaletted(src.Bounds(), pal)
	b.ReportAllocs()
	for b.Loop() {
		draw.Draw(dst, src.Bounds(), src, src.Bounds().Min, draw.Src)
	}
}

func BenchmarkPalettizeLUT(b *testing.B) {
	src := solidTerminalFrame(640, 480)
	pal := terminalGIFPalette()
	dst := image.NewPaletted(src.Bounds(), pal)
	initTerminalGIFLUT()
	b.ReportAllocs()
	for b.Loop() {
		palettizeNRGBA(dst, src)
	}
}

func solidTerminalFrame(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8((x*6/w)*40 + 16), //nolint:gosec // bounded.
				G: uint8((y*6/h)*40 + 16), //nolint:gosec // bounded.
				B: 0x80,
				A: 0xff,
			})
		}
	}
	return img
}

func gifDecodeFile(path string) (*gif.GIF, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return gif.DecodeAll(f)
}
