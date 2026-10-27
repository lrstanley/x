// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package sink

import (
	"bytes"
	"image"
	"image/color"
	"testing"
	"time"
)

// farColors are at least 255 away in red from every color of
// [distinctColors] up to 256 entries, which all have red 0.
func farColors(n int) []color.NRGBA {
	out := make([]color.NRGBA, n)
	for i := range out {
		out[i] = color.NRGBA{R: 0xff, G: uint8(i * 255 / max(n-1, 1)), B: 0x80, A: 0xff} //nolint:gosec // bounded.
	}
	return out
}

// fullThenFar encodes a frame that fills the shared palette with solid colors,
// then a frame of the same size with far colors in the given number of
// 8x8 blocks.
func fullThenFar(t *testing.T, farBlocks int) (frames []image.Image, raw []byte, palettes int) {
	t.Helper()
	const colors = 250
	first := solidColorsFrame(distinctColors(colors), 8)
	second := cloneNRGBA(first)
	for i, c := range farColors(farBlocks) {
		paintRect(second, image.Rect(i*8, 0, (i+1)*8, 8), c)
	}
	delay := 40 * time.Millisecond
	g, raw := encodeFrames(t, []Frame{{Image: first, Delay: delay}, {Image: second, Delay: delay}})
	if len(g.Image) != 2 {
		t.Fatalf("got %d frames, want 2", len(g.Image))
	}
	palettes = 1
	if !samePalette(g.Image[0].Palette, g.Image[1].Palette) {
		palettes = 2
	}
	return []image.Image{first, second}, raw, palettes
}

func TestAdaptivePaletteRollsOverWhenFull(t *testing.T) {
	t.Parallel()

	frames, raw, palettes := fullThenFar(t, 16)
	if palettes != 2 {
		t.Fatal("frame of far colors on a full palette did not start a new palette")
	}
	replayed, err := ReplayGIF(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if pct, _ := replayMismatchPct(frames, replayed, color.NRGBA{A: 1}); pct != 0 {
		t.Fatalf("replay differs from the source in %.2f%% of pixels", pct)
	}
}

func TestAdaptivePaletteKeepsSmallMisses(t *testing.T) {
	t.Parallel()

	// One 8x8 block is exactly the rollover floor, which it must exceed.
	if _, _, palettes := fullThenFar(t, 1); palettes != 1 {
		t.Fatal("one poorly matched block started a new palette")
	}
}

func TestAdaptivePaletteDoesNotRollOverWithRoom(t *testing.T) {
	t.Parallel()

	// Far more colors than slots, so median cut leaves many of them far from
	// every entry, but the palette had room when the frames arrived.
	noise := func(mx, my int) *image.NRGBA {
		img := image.NewNRGBA(image.Rect(0, 0, 256, 64))
		for y := range 64 {
			for x := range 256 {
				img.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y * 4), B: uint8(x*mx + y*my), A: 0xff}) //nolint:gosec // bounded.
			}
		}
		return img
	}
	delay := 40 * time.Millisecond
	g, _ := encodeFrames(t, []Frame{{Image: noise(7, 13), Delay: delay}, {Image: noise(11, 5), Delay: delay}})
	global, _ := g.Config.ColorModel.(color.Palette)
	for i, frame := range g.Image {
		if !usesGlobalPalette(frame.Palette, global) {
			t.Fatalf("frame %d is on a palette of its own, want the shared one", i)
		}
	}
}
