// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package export

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
)

func TestPNGTransparency(t *testing.T) {
	t.Parallel()

	src := checkerboardAlpha(16, 16)

	t.Run("file", func(t *testing.T) {
		t.Parallel()
		path := t.TempDir() + "/alpha.png"
		if err := PNG(src, path, WithOptimize(OptimizeNone)); err != nil {
			t.Fatal(err)
		}
		decoded, err := readPNG(path)
		if err != nil {
			t.Fatal(err)
		}
		assertAlphaMatches(t, src, decoded)
	})

	t.Run("writer", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		if err := WritePNG(src, &buf, WithOptimize(OptimizeNone)); err != nil {
			t.Fatal(err)
		}
		decoded, err := png.Decode(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		assertAlphaMatches(t, src, decoded)
	})
}

func TestPNGRejectsGIFOnlyOptions(t *testing.T) {
	t.Parallel()

	src := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	path := t.TempDir() + "/bad.png"

	cases := []struct {
		name string
		opt  Option
	}{
		{"WithPalette", WithPalette(defaultGIFPalette())},
		{"WithBackground", WithBackground(color.Black)},
		{"WithFrameRate", WithFrameRate(30, false, func() image.Image { return src })},
		{"WithChannel", WithChannel(make(<-chan Frame))},
		{"WithMaxFrames", WithMaxFrames(10)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := PNG(src, path, tc.opt); err == nil {
				t.Fatalf("expected error for %s on PNG", tc.name)
			}
		})
	}
}

func TestPNGIndexedQuantization(t *testing.T) {
	t.Parallel()

	src := solidNRGBA(8, 8, color.NRGBA{R: 0xff, G: 0x00, B: 0x80, A: 0xff})
	path := t.TempDir() + "/indexed.png"
	if err := PNG(src, path, WithOptimize(OptimizeColorQuantization)); err != nil {
		t.Fatal(err)
	}

	assertIndexedPNG(t, path)
}

func TestPNGDefaultIndexed(t *testing.T) {
	t.Parallel()

	src := solidNRGBA(8, 8, color.NRGBA{R: 0xff, G: 0x00, B: 0x80, A: 0xff})
	path := t.TempDir() + "/default.png"
	if err := PNG(src, path); err != nil {
		t.Fatal(err)
	}

	assertIndexedPNG(t, path)
}

func TestPNGIgnoresGIFOnlyOptimizeFlags(t *testing.T) {
	t.Parallel()

	src := solidNRGBA(8, 8, color.NRGBA{R: 0xff, G: 0x00, B: 0x80, A: 0xff})
	path := t.TempDir() + "/frames-only.png"
	if err := PNG(src, path, WithOptimize(OptimizeFrames|OptimizeDirtyRects)); err != nil {
		t.Fatal(err)
	}

	decoded, err := readPNG(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded.(*image.Paletted); ok {
		t.Fatal("GIF-only optimize flags should not enable indexed PNG")
	}
}

func assertIndexedPNG(t *testing.T, path string) {
	t.Helper()

	decoded, err := readPNG(path)
	if err != nil {
		t.Fatal(err)
	}
	pm, ok := decoded.(*image.Paletted)
	if !ok {
		t.Fatalf("got %T, want *image.Paletted for indexed PNG", decoded)
	}
	if len(pm.Palette) > 2 {
		t.Fatalf("compact palette len = %d, want <= 2 for solid color", len(pm.Palette))
	}
}

func checkerboardAlpha(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			a := uint8(0xff)
			if (x+y)%2 == 0 {
				a = 0
			}
			img.SetNRGBA(x, y, color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: a})
		}
	}
	return img
}

func readPNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

func assertAlphaMatches(t *testing.T, want, got image.Image) {
	t.Helper()
	b := want.Bounds()
	if !got.Bounds().Eq(b) {
		t.Fatalf("bounds %v != %v", got.Bounds(), b)
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			_, _, _, wa := want.At(x, y).RGBA()
			_, _, _, ga := got.At(x, y).RGBA()
			if wa != ga {
				t.Fatalf("alpha at (%d,%d): got %d want %d", x, y, ga>>8, wa>>8)
			}
		}
	}
}
