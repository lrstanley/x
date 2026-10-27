// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package palette

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"testing"

	"github.com/lrstanley/x/term/reel/sink/internal/testutil"
)

// palettize maps src through pal with [Palettize].
func palettize(src *image.NRGBA, pal color.Palette) *image.Paletted {
	pm := image.NewPaletted(src.Bounds(), pal)
	Palettize(pm, src)
	return pm
}

// BenchmarkAdaptivePaletteCold palettizes a frame of antialiased ramps into a
// fresh adaptive palette, so every color is new and median cut runs.
func BenchmarkAdaptivePaletteCold(b *testing.B) {
	src := testutil.TerminalFrame(0)
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
		ap := NewAdaptive(true, false)
		dst := image.NewPaletted(src.Bounds(), ap.pal)
		ap.Palettize(dst, src)
	}
}

func BenchmarkPalettizeDraw(b *testing.B) {
	src := testutil.TerminalFrame(0)
	pal := Default()
	dst := image.NewPaletted(src.Bounds(), pal)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		draw.Draw(dst, src.Bounds(), src, src.Bounds().Min, draw.Src)
	}
}

func BenchmarkPalettizeLUT(b *testing.B) {
	src := testutil.TerminalFrame(0)
	pal := Default()
	dst := image.NewPaletted(src.Bounds(), pal)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		Palettize(dst, src)
	}
}

func TestPalettizeRGBAMatchesNRGBA(t *testing.T) {
	t.Parallel()
	src := image.NewNRGBA(image.Rect(0, 0, 64, 4))
	for x := range 64 {
		// Rows: opaque, translucent, barely visible, and fully transparent.
		for y, a := range []uint8{0xff, 0x99, 0x01, 0x00} {
			src.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 4), G: 0x80, B: uint8(255 - x*4), A: a}) //nolint:gosec // bounded.
		}
	}
	rgba := testutil.ToRGBA(src)
	// Premultiplying loses precision at low alpha, so compare against the
	// NRGBA image the RGBA one converts back to.
	back := testutil.ToNRGBA(rgba)

	pal := Default()
	want := palettize(back, pal)
	got := image.NewPaletted(src.Bounds(), pal)
	Palettize(got, rgba)
	for y := range 4 {
		for x := range 64 {
			if w, g := want.ColorIndexAt(x, y), got.ColorIndexAt(x, y); w != g {
				t.Errorf("pixel (%d,%d): RGBA source index %d, NRGBA source index %d", x, y, g, w)
			}
		}
	}
	if !bytes.Equal(palettize(src, pal).Pix[:64], got.Pix[:64]) {
		t.Error("opaque pixels differ between RGBA and NRGBA sources")
	}
	for x := range 64 {
		if got.ColorIndexAt(x, 3) != TransparentIndex {
			t.Errorf("pixel (%d,3) is fully transparent, want TransparentIndex", x)
		}
	}
}
