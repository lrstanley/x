// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package palette

import (
	"bytes"
	"image"
	"image/color"
	"math/rand/v2"
	"testing"

	"github.com/lrstanley/x/term/reel/sink/internal/testutil"
)

// syntheticPalettedFrames returns deterministic frames using a skewed spread of
// palette indices, with transparent pixels when transparent is true.
func syntheticPalettedFrames(w, h, n int, transparent bool) []*image.Paletted {
	rng := rand.New(rand.NewPCG(1, 2))
	pal := testutil.GrayPalette()
	frames := make([]*image.Paletted, n)
	for i := range frames {
		pm := image.NewPaletted(image.Rect(0, 0, w, h), pal)
		for j := range pm.Pix {
			switch r := rng.IntN(100); {
			case r < 70:
				pm.Pix[j] = 16
			case transparent && r < 80:
				pm.Pix[j] = TransparentIndex
			default:
				pm.Pix[j] = uint8(rng.IntN(int(TransparentIndex))) //nolint:gosec // bounded.
			}
		}
		frames[i] = pm
	}
	return frames
}

func cloneFrames(frames []*image.Paletted) []*image.Paletted {
	out := make([]*image.Paletted, len(frames))
	for i, f := range frames {
		out[i] = &image.Paletted{Pix: bytes.Clone(f.Pix), Stride: f.Stride, Rect: f.Rect, Palette: f.Palette}
	}
	return out
}

func gray(c color.Color) uint32 {
	y, _, _, _ := c.RGBA()
	return y
}

// handFrame is a w×1 frame on pal showing the indices given.
func handFrame(pal color.Palette, indices ...uint8) *image.Paletted {
	pm := image.NewPaletted(image.Rect(0, 0, len(indices), 1), pal)
	copy(pm.Pix, indices)
	return pm
}

// TestCompactKeepsColors checks that Compact keeps the colors every pixel
// shows, keeps the used entries in order, and keeps a zero-alpha slot last
// only when a frame uses it.
func TestCompactKeepsColors(t *testing.T) {
	t.Parallel()
	pal := testutil.GrayPalette()
	for _, tc := range []struct {
		name        string
		frames      []*image.Paletted
		transparent bool
	}{
		{"opaque", syntheticPalettedFrames(97, 31, 4, false), false},
		{"transparent", syntheticPalettedFrames(97, 31, 4, true), true},
		// The keyframe uses only indices compaction leaves in place.
		{"keyframe", []*image.Paletted{handFrame(pal, 0, 1, 2), handFrame(pal, 2, TransparentIndex)}, true},
		{"gap", []*image.Paletted{handFrame(pal, 0, 7), handFrame(pal, 3, TransparentIndex)}, true},
	} {
		got := cloneFrames(tc.frames)
		cpal := Compact(got)

		_, _, _, a := cpal[len(cpal)-1].RGBA()
		if hasSlot := a == 0; hasSlot != tc.transparent {
			t.Fatalf("%s: zero-alpha last slot = %v", tc.name, hasSlot)
		}
		for i := 1; i < len(cpal)-1; i++ {
			if gray(cpal[i]) <= gray(cpal[i-1]) {
				t.Fatalf("%s: palette[%d] = %v after %v, want used entries in order", tc.name, i, cpal[i], cpal[i-1])
			}
		}
		for i, src := range tc.frames {
			if !Same(got[i].Palette, cpal) {
				t.Fatalf("%s: frame %d not on the compacted palette", tc.name, i)
			}
			for j, idx := range src.Pix {
				if want, have := src.Palette[idx], cpal[got[i].Pix[j]]; want != have {
					t.Fatalf("%s: frame %d pixel %d = %v, want %v", tc.name, i, j, have, want)
				}
			}
		}
	}
}

func BenchmarkCompactPalette(b *testing.B) {
	for _, tc := range []struct {
		name string
		w, h int
	}{
		{"80x24", testutil.TerminalWidth, testutil.TerminalHeight},
		{"240x72", testutil.TerminalWidth * 3, testutil.TerminalHeight * 3},
	} {
		src := syntheticPalettedFrames(tc.w, tc.h, 1, true)
		b.Run(tc.name, func(b *testing.B) {
			work := cloneFrames(src[:1])[0]
			b.ReportAllocs()
			b.SetBytes(int64(len(work.Pix)))
			for b.Loop() {
				copy(work.Pix, src[0].Pix)
				Compact([]*image.Paletted{work})
			}
		})
	}
}
