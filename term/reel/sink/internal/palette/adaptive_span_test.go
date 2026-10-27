// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package palette

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"image"
	"testing"

	"github.com/lrstanley/x/term/reel/sink/internal/testutil"
)

// referenceWriteSpan is the per-pixel implementation writeSpan must match.
func referenceWriteSpan(p *Adaptive, dst *image.Paletted, px rgbaPixels, area image.Rectangle) map[uint32]int {
	misses := map[uint32]int{}
	width := area.Dx()
	for y := area.Min.Y; y < area.Max.Y; y++ {
		si := (y-px.rect.Min.Y)*px.stride + (area.Min.X-px.rect.Min.X)*4
		di := (y-dst.Rect.Min.Y)*dst.Stride + (area.Min.X - dst.Rect.Min.X)
		for x := range width {
			s := px.pix[si+x*4 : si+x*4+4]
			if s[3] == 0 {
				dst.Pix[di+x] = TransparentIndex
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
				src := testutil.SpanFrame(uint64(w), w, 9, premultiplied, kind) //nolint:gosec // small.
				var px rgbaPixels
				switch s := src.(type) {
				case *image.NRGBA:
					px = rgbaPixels{pix: s.Pix, stride: s.Stride, rect: s.Rect}
				case *image.RGBA:
					px = rgbaPixels{pix: s.Pix, stride: s.Stride, rect: s.Rect, premultiplied: true}
				}
				p := NewAdaptive(true, false)
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

				if !bytes.Equal(want.Pix, got.Pix) {
					t.Fatalf("%s premul=%v w=%d: pixels differ", kind, premultiplied, w)
				}
				if len(wantMisses) != len(p.spanMisses[0]) {
					t.Fatalf("%s premul=%v w=%d: miss colors %d, want %d", kind, premultiplied, w, len(p.spanMisses[0]), len(wantMisses))
				}
				for k, v := range wantMisses {
					if got := p.spanMisses[0][k]; int(got.count) != v {
						t.Fatalf("%s premul=%v w=%d: misses[%06x]=%+v, want count %d", kind, premultiplied, w, k, got, v)
					}
				}
			}
		}
	}
}

// adaptiveGoldenHash is the sha256 of the palette and palettized pixels
// across the sequence in TestAdaptivePalettizeGolden, recorded when small
// flat shapes started counting as solid. It guards against any change in
// palette growth or indices.
const adaptiveGoldenHash = "cada4e351d94b6824f6ac2d18624dfde46fab6026d9309cc5cef87168b697709"

func TestAdaptivePalettizeGolden(t *testing.T) {
	t.Parallel()

	h := sha256.New()
	for _, reserve := range []bool{true, false} {
		ap := NewAdaptive(reserve, false)
		for i, kind := range []string{"flat", "text", "mixed", "text", "noise", "mixed", "text"} {
			for _, premultiplied := range []bool{false, true} {
				w, ht := 301, 97
				if i == 3 {
					w, ht = 700, 400 // large enough to split across workers.
				}
				src := testutil.SpanFrame(uint64(i)+1, w, ht, premultiplied, kind) //nolint:gosec // small.
				dst := image.NewPaletted(src.Bounds(), ap.pal)
				ap.Palettize(dst, src)
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

// BenchmarkAdaptiveWriteSpan measures the warm cache-hit path by frame shape.
func BenchmarkAdaptiveWriteSpan(b *testing.B) {
	for _, kind := range []string{"flat", "text", "mixed", "noise"} {
		b.Run(kind, func(b *testing.B) {
			src := testutil.SpanFrame(1, testutil.TerminalWidth, testutil.TerminalHeight, false, kind)
			ap := NewAdaptive(true, false)
			dst := image.NewPaletted(src.Bounds(), ap.pal)
			ap.Palettize(dst, src)
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
