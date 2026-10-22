// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package export

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"image"
	"math/rand/v2"
	"testing"
	"time"
)

// spanFrame fills a w×h frame with a deterministic mix of flat runs, text-like
// glyph noise, translucent pixels, and alpha 0 pixels with differing RGB.
func spanFrame(seed uint64, w, h int, premultiplied bool, kind string) (image.Image, []uint8) {
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	// Pad the stride and offset the rect so row slicing is exercised.
	rect := image.Rect(3, 5, 3+w, 5+h)
	stride := w*4 + 12
	pix := make([]uint8, stride*h)
	fills := [][4]uint8{{0, 0, 0, 255}, {30, 30, 46, 255}, {205, 214, 244, 255}, {243, 139, 168, 255}, {10, 20, 30, 0}, {99, 98, 97, 0}, {200, 100, 50, 128}, {1, 2, 3, 1}}
	for y := range h {
		row := pix[y*stride : y*stride+w*4]
		for x := 0; x < w; {
			n := 1 + rng.IntN(40)
			var c [4]uint8
			switch kind {
			case "noise":
				n = 1 + rng.IntN(2)
				c = [4]uint8{uint8(rng.UintN(256)), uint8(rng.UintN(256)), uint8(rng.UintN(256)), uint8(rng.UintN(256))} //nolint:gosec // bounded.
			case "flat":
				n = w
				c = fills[y%len(fills)]
			case "text":
				n = 1 + rng.IntN(6)
				c = fills[rng.IntN(3)]
				if rng.IntN(4) == 0 {
					v := uint8(rng.UintN(256)) //nolint:gosec // bounded.
					c = [4]uint8{v, v / 2, 255 - v, 255}
				}
			default:
				c = fills[rng.IntN(len(fills))]
			}
			for i := x; i < min(x+n, w); i++ {
				copy(row[i*4:], c[:])
			}
			x += n
		}
	}
	if premultiplied {
		// Make the data valid premultiplied RGBA (channels <= alpha).
		for y := range h {
			for x := range w {
				p := pix[y*stride+x*4 : y*stride+x*4+4]
				for i := range 3 {
					p[i] = uint8(uint32(p[i]) * uint32(p[3]) / 255) //nolint:gosec // bounded.
				}
			}
		}
		return &image.RGBA{Pix: pix, Stride: stride, Rect: rect}, pix
	}
	return &image.NRGBA{Pix: pix, Stride: stride, Rect: rect}, pix
}

// referenceWriteSpan is the per-pixel implementation writeSpan must match.
func referenceWriteSpan(p *adaptivePalette, dst *image.Paletted, px rgbaPixels, area image.Rectangle) map[uint32]int {
	misses := map[uint32]int{}
	width := area.Dx()
	for y := area.Min.Y; y < area.Max.Y; y++ {
		si := (y-px.rect.Min.Y)*px.stride + (area.Min.X-px.rect.Min.X)*4
		di := (y-dst.Rect.Min.Y)*dst.Stride + (area.Min.X - dst.Rect.Min.X)
		for x := range width {
			s := px.pix[si+x*4 : si+x*4+4]
			if s[3] == 0 {
				dst.Pix[di+x] = TransparentPaletteIndex
				continue
			}
			r, g, b := s[0], s[1], s[2]
			if px.premultiplied && s[3] != 0xff {
				r, g, b = unpremultiply(r, s[3]), unpremultiply(g, s[3]), unpremultiply(b, s[3])
			}
			key := uint32(r)<<16 | uint32(g)<<8 | uint32(b)
			idx, ok := p.cache[key]
			if !ok {
				misses[key]++
			}
			dst.Pix[di+x] = idx
		}
	}
	return misses
}

func TestAdaptiveWriteSpanMatchesReference(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"mixed", "noise", "flat", "text"} {
		for _, premultiplied := range []bool{false, true} {
			for _, w := range []int{1, 2, 7, 8, 9, 31, 64, 203} {
				src, _ := spanFrame(uint64(w), w, 9, premultiplied, kind) //nolint:gosec // small.
				var px rgbaPixels
				switch s := src.(type) {
				case *image.NRGBA:
					px = rgbaPixels{pix: s.Pix, stride: s.Stride, rect: s.Rect}
				case *image.RGBA:
					px = rgbaPixels{pix: s.Pix, stride: s.Stride, rect: s.Rect, premultiplied: true}
				}
				p := newAdaptivePalette(true)
				// Cache roughly half the colors so both hits and misses occur.
				for i := range px.pix {
					if i%8 == 0 && i+3 < len(px.pix) && px.pix[i+3] != 0 {
						key := uint32(px.pix[i])<<16 | uint32(px.pix[i+1])<<8 | uint32(px.pix[i+2])
						p.cache[key] = uint8(i % 200) //nolint:gosec // bounded.
					}
				}
				// Sub-area so row offsets within the source are non-zero.
				area := px.rect.Inset(0)
				if w > 3 {
					area.Min.X++
					area.Max.X--
				}
				want := image.NewPaletted(px.rect, p.pal)
				got := image.NewPaletted(px.rect, p.pal)
				wantMisses := referenceWriteSpan(p, want, px, area)
				p.resetSpans(1)
				p.writeSpan(0, got, px, area, area.Min.Y, area.Max.Y)

				if string(want.Pix) != string(got.Pix) {
					t.Fatalf("%s premul=%v w=%d: pixels differ", kind, premultiplied, w)
				}
				if len(wantMisses) != len(p.spanMisses[0]) {
					t.Fatalf("%s premul=%v w=%d: miss colors %d, want %d", kind, premultiplied, w, len(p.spanMisses[0]), len(wantMisses))
				}
				for k, v := range wantMisses {
					if p.spanMisses[0][k] != v {
						t.Fatalf("%s premul=%v w=%d: misses[%06x]=%d, want %d", kind, premultiplied, w, k, p.spanMisses[0][k], v)
					}
				}
			}
		}
	}
}

// adaptiveGoldenHash is the sha256 of the palette and palettized pixels
// across the sequence in TestAdaptivePalettizeGolden, recorded before runs
// were skipped. It guards against any change in palette growth or indices.
const adaptiveGoldenHash = "5e81d803307724b114801ad3b577a7acd53aa22b41c74310d829b28d0fcb1f75"

func TestAdaptivePalettizeGolden(t *testing.T) {
	t.Parallel()

	h := sha256.New()
	for _, reserve := range []bool{true, false} {
		ap := newAdaptivePalette(reserve)
		for i, kind := range []string{"flat", "text", "mixed", "text", "noise", "mixed", "text"} {
			for _, premultiplied := range []bool{false, true} {
				w, ht := 301, 97
				if i == 3 {
					w, ht = 700, 400 // large enough to split across workers.
				}
				src, _ := spanFrame(uint64(i)+1, w, ht, premultiplied, kind) //nolint:gosec // small.
				dst := image.NewPaletted(src.Bounds(), ap.pal)
				ap.palettize(dst, src)
				h.Write(dst.Pix)
				for _, c := range ap.pal {
					r, g, b, a := c.RGBA()
					_ = binary.Write(h, binary.LittleEndian, []uint32{r, g, b, a})
				}
			}
		}
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != adaptiveGoldenHash {
		t.Fatalf("hash = %s, want %s", got, adaptiveGoldenHash)
	}
}

// BenchmarkGIFAddFrameDense is BenchmarkGIFAddFrameAdaptive with a frame of
// short text-like runs instead of nearly flat fills.
func BenchmarkGIFAddFrameDense(b *testing.B) {
	frameA, _ := spanFrame(1, terminalBenchmarkWidth, terminalBenchmarkHeight, false, "text")
	frameB, _ := spanFrame(2, terminalBenchmarkWidth, terminalBenchmarkHeight, false, "text")
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
}

// BenchmarkAdaptiveWriteSpan measures the warm cache-hit path by frame shape.
func BenchmarkAdaptiveWriteSpan(b *testing.B) {
	for _, kind := range []string{"flat", "text", "mixed", "noise"} {
		b.Run(kind, func(b *testing.B) {
			src, _ := spanFrame(1, terminalBenchmarkWidth, terminalBenchmarkHeight, false, kind)
			ap := newAdaptivePalette(true)
			dst := image.NewPaletted(src.Bounds(), ap.pal)
			ap.palettize(dst, src)
			px := src.(*image.NRGBA)
			rp := rgbaPixels{pix: px.Pix, stride: px.Stride, rect: px.Rect}
			area := px.Rect.Intersect(dst.Bounds())
			ap.resetSpans(1)
			b.SetBytes(int64(area.Dx() * area.Dy() * 4))
			b.ResetTimer()
			for b.Loop() {
				ap.writeSpan(0, dst, rp, area, area.Min.Y, area.Max.Y)
			}
		})
	}
}
