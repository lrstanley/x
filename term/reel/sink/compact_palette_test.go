// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package sink

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"math/rand/v2"
	"sort"
	"testing"
	"time"
)

// compactPaletteReference is the original map-based implementation, kept to
// verify the array-based compactPalette is output-identical.
func compactPaletteReference(frames []*image.Paletted, oldPal color.Palette) color.Palette {
	freq := make(map[uint8]int)
	usesTransparent := false
	for _, frame := range frames {
		for _, idx := range frame.Pix {
			if idx == TransparentPaletteIndex {
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
		remap[TransparentPaletteIndex] = newTransparentIdx
	}
	for _, frame := range frames {
		frame.Palette = newPal
		for i, idx := range frame.Pix {
			if idx == TransparentPaletteIndex {
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
	pal := defaultGIFPalette()
	frames := make([]*image.Paletted, n)
	for i := range frames {
		pm := image.NewPaletted(image.Rect(0, 0, w, h), pal)
		for j := range pm.Pix {
			switch r := rng.IntN(100); {
			case r < 70:
				pm.Pix[j] = 16
			case transparent && r < 80:
				pm.Pix[j] = TransparentPaletteIndex
			default:
				pm.Pix[j] = uint8(rng.IntN(int(TransparentPaletteIndex))) //nolint:gosec // bounded.
			}
		}
		frames[i] = pm
	}
	return frames
}

func cloneFrames(frames []*image.Paletted) []*image.Paletted {
	out := make([]*image.Paletted, len(frames))
	for i, f := range frames {
		out[i] = clonePaletted(f)
	}
	return out
}

func TestCompactPaletteMatchesReference(t *testing.T) {
	t.Parallel()
	for _, transparent := range []bool{false, true} {
		src := syntheticPalettedFrames(97, 31, 4, transparent)
		want, got := cloneFrames(src), cloneFrames(src)
		wantPal := compactPaletteReference(want, defaultGIFPalette())
		gotPal := compactPalette(got, defaultGIFPalette())
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

// encodeSyntheticGIF records deterministic gradient frames through the public
// recorder and returns the encoded bytes.
func encodeSyntheticGIF(tb testing.TB, frames int) []byte {
	tb.Helper()
	ch := make(chan Frame)
	var buf bytes.Buffer
	enc, err := NewGIF(&buf)
	if err != nil {
		tb.Fatal(err)
	}
	rec, err := Record(enc, WithChannel(ch))
	if err != nil {
		tb.Fatal(err)
	}
	for i := range frames {
		img := terminalBenchmarkFrame(i / 2)
		// small localized change per frame so disposal planning emits sub-rects.
		for x := 100; x < 140; x++ {
			img.SetNRGBA(x, 100+i, color.NRGBA{R: 0xff, A: 0xff})
		}
		ch <- Frame{Image: img, Delay: 50 * time.Millisecond}
	}
	close(ch)
	if err = rec.Close(); err != nil {
		tb.Fatal(err)
	}
	return buf.Bytes()
}

// TestEncodeAllDeterministic prints the SHA-256 of a synthetic multi-frame GIF
// so before/after output identity can be compared (go test -v -run Deterministic).
func TestEncodeAllDeterministic(t *testing.T) {
	a, b := encodeSyntheticGIF(t, 6), encodeSyntheticGIF(t, 6)
	if !bytes.Equal(a, b) {
		t.Fatal("encode is not deterministic")
	}
	sum := sha256.Sum256(a)
	t.Logf("gif sha256=%s len=%d", hex.EncodeToString(sum[:]), len(a))
}

func BenchmarkCompactPalette(b *testing.B) {
	for _, tc := range []struct {
		name string
		w, h int
	}{
		{"80x24", terminalBenchmarkWidth, terminalBenchmarkHeight},
		{"240x72", terminalBenchmarkWidth * 3, terminalBenchmarkHeight * 3},
	} {
		src := syntheticPalettedFrames(tc.w, tc.h, 1, true)
		pal := defaultGIFPalette()
		b.Run(tc.name, func(b *testing.B) {
			work := clonePaletted(src[0])
			b.ReportAllocs()
			b.SetBytes(int64(len(work.Pix)))
			for b.Loop() {
				copy(work.Pix, src[0].Pix)
				compactPalette([]*image.Paletted{work}, pal)
			}
		})
	}
}

func BenchmarkEncodeAll(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		encodeSyntheticGIF(b, 8)
	}
}
