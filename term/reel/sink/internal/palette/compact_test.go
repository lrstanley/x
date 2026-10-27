// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package palette

import (
	"bytes"
	"image"
	"image/color"
	"math/rand/v2"
	"sort"
	"testing"

	"github.com/lrstanley/x/term/reel/sink/internal/testutil"
)

// compactPaletteReference is the original map-based implementation, kept to
// verify the array-based Compact is output-identical.
func compactPaletteReference(frames []*image.Paletted, oldPal color.Palette) color.Palette {
	freq := make(map[uint8]int)
	usesTransparent := false
	for _, frame := range frames {
		for _, idx := range frame.Pix {
			if idx == TransparentIndex {
				usesTransparent = true
				continue
			}
			freq[idx]++
		}
	}
	type colorFreq struct {
		idx   uint8
		count int
	}
	used := make([]colorFreq, 0, len(freq))
	for idx, count := range freq {
		used = append(used, colorFreq{idx: idx, count: count})
	}
	sort.Slice(used, func(i, j int) bool {
		if used[i].count != used[j].count {
			return used[i].count > used[j].count
		}
		return used[i].idx < used[j].idx
	})
	newPal := make(color.Palette, 0, len(used)+1)
	remap := make(map[uint8]uint8, len(used)+1)
	for i, cf := range used {
		newPal = append(newPal, oldPal[cf.idx])
		remap[cf.idx] = uint8(i)
	}
	newTransparentIdx := uint8(0)
	if usesTransparent {
		newTransparentIdx = uint8(len(newPal))
		newPal = append(newPal, transparentMarkerColor())
		remap[TransparentIndex] = newTransparentIdx
	}
	for _, frame := range frames {
		frame.Palette = newPal
		for i, idx := range frame.Pix {
			if idx == TransparentIndex {
				frame.Pix[i] = newTransparentIdx
				continue
			}
			frame.Pix[i] = remap[idx]
		}
	}
	return newPal
}

// syntheticPalettedFrames returns deterministic frames using a skewed spread of
// palette indices, with transparent pixels when transparent is true.
func syntheticPalettedFrames(w, h, n int, transparent bool) []*image.Paletted {
	rng := rand.New(rand.NewPCG(1, 2))
	pal := Default()
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

func TestCompactPaletteMatchesReference(t *testing.T) {
	t.Parallel()
	for _, transparent := range []bool{false, true} {
		src := syntheticPalettedFrames(97, 31, 4, transparent)
		want, got := cloneFrames(src), cloneFrames(src)
		wantPal := compactPaletteReference(want, Default())
		gotPal := Compact(got, Default())
		if len(wantPal) != len(gotPal) {
			t.Fatalf("transparent=%v: palette len = %d, want %d", transparent, len(gotPal), len(wantPal))
		}
		for i := range wantPal {
			if wantPal[i] != gotPal[i] {
				t.Fatalf("transparent=%v: palette[%d] = %v, want %v", transparent, i, gotPal[i], wantPal[i])
			}
		}
		for i := range want {
			if !bytes.Equal(want[i].Pix, got[i].Pix) {
				t.Fatalf("transparent=%v: frame %d pixels differ", transparent, i)
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
		pal := Default()
		b.Run(tc.name, func(b *testing.B) {
			work := cloneFrames(src[:1])[0]
			b.ReportAllocs()
			b.SetBytes(int64(len(work.Pix)))
			for b.Loop() {
				copy(work.Pix, src[0].Pix)
				Compact([]*image.Paletted{work}, pal)
			}
		})
	}
}
