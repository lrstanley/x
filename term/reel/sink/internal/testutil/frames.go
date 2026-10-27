// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package testutil holds the frames the tests of sink and its internal
// packages share. Only _test.go files import it.
package testutil

import (
	"image"
	"image/color"
	"image/draw"
	"math/rand/v2"
)

const (
	// TerminalWidth and TerminalHeight are the size of [TerminalFrame]:
	// render.MustNew(20pt) drawing an 80×24 terminal screen.
	TerminalWidth  = 1280
	TerminalHeight = 864
)

// TerminalFrame returns an NRGBA frame sized like render.MustNew(20pt)
// rendering an 80×24 terminal screen, with a gradient fill that exercises the
// palette builder.
func TerminalFrame(offset int) *image.NRGBA {
	w, h := TerminalWidth, TerminalHeight
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

// SpanFrame fills a w×h frame with a deterministic mix of flat runs, text-like
// glyph noise, translucent pixels, and alpha 0 pixels with differing RGB.
// kind is "flat", "text", "noise", or "mixed". A premultiplied frame is an
// *image.RGBA, and any other an *image.NRGBA.
func SpanFrame(seed uint64, w, h int, premultiplied bool, kind string) image.Image {
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)) //nolint:gosec // deterministic, not secret.
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
		return &image.RGBA{Pix: pix, Stride: stride, Rect: rect}
	}
	return &image.NRGBA{Pix: pix, Stride: stride, Rect: rect}
}

// PaintRect fills r of img with c.
func PaintRect(img *image.NRGBA, r image.Rectangle, c color.NRGBA) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
}

// ToNRGBA returns img as an *image.NRGBA, converting it unless it is one.
func ToNRGBA(img image.Image) *image.NRGBA {
	if nrgba, ok := img.(*image.NRGBA); ok {
		return nrgba
	}
	b := img.Bounds()
	dst := image.NewNRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dst.Set(x, y, img.At(x, y))
		}
	}
	return dst
}

// ToRGBA converts img to a premultiplied *image.RGBA.
func ToRGBA(img image.Image) *image.RGBA {
	b := img.Bounds()
	dst := image.NewRGBA(b)
	draw.Draw(dst, b, img, b.Min, draw.Src)
	return dst
}

// DistinctColors returns n opaque colors, 16 apart on each channel.
func DistinctColors(n int) []color.NRGBA {
	out := make([]color.NRGBA, 0, n)
	for r := 0; r < 256 && len(out) < n; r += 16 {
		for g := 0; g < 256 && len(out) < n; g += 16 {
			for b := 0; b < 256 && len(out) < n; b += 16 {
				out = append(out, color.NRGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 0xff}) //nolint:gosec // bounded.
			}
		}
	}
	return out
}

// GrayPalette returns 255 distinct grays, then a zero-alpha color in slot 255,
// the slot package palette reserves for transparency, for tests that build
// paletted frames by hand.
func GrayPalette() color.Palette {
	pal := make(color.Palette, 256)
	for i := range 255 {
		pal[i] = color.Gray{Y: uint8(i)} //nolint:gosec // bounded.
	}
	pal[255] = color.RGBA{}
	return pal
}
