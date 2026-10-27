// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package palette maps frames to palettes of at most 256 colors for GIF and
// PNG: through the xterm-256color palette ([Default], [Palettize]), or
// through one built from the colors a recording uses ([Adaptive]). It also
// compacts palettes to the colors their frames use ([Compact]).
//
// Index 255, [TransparentIndex], is reserved for transparency: fully
// transparent pixels map to it, and opaque pixels never do. The palettes it
// builds fill that slot with a zero-alpha marker.
package palette

import (
	"image"
	"image/color"
	"image/draw"
	"runtime"
	"sync"
)

// TransparentIndex is the palette index reserved for transparency during
// GIF capture and optimization.
const TransparentIndex uint8 = 255

const (
	palettizeParallelMinBytes       = 1 << 20
	palettizeParallelBytesPerWorker = 512 << 10
	palettizeParallelRowsPerWorker  = 64
)

// palettizeJob is one disjoint row span of a frame.
type palettizeJob struct {
	dst  *image.Paletted
	src  rgbaPixels
	area image.Rectangle
	minY int
	maxY int
	// adaptive, when non-nil, maps through its cache instead of the LUT and
	// records uncached colors for span.
	adaptive *Adaptive
	span     int
}

// rgbaPixels is the pixel layout *image.NRGBA and *image.RGBA share: four
// bytes per pixel, R, G, B, A. RGBA's color channels are premultiplied by
// alpha.
type rgbaPixels struct {
	pix           []uint8
	stride        int
	rect          image.Rectangle
	premultiplied bool
}

// Default returns the standard xterm-256color palette for GIF
// encoding. Terminal renders mostly use ANSI, cube, and grayscale slots;
// mapping through this palette preserves lipgloss and emulator colors better
// than generic palettes such as Plan9.
func Default() color.Palette {
	p := make(color.Palette, 256)

	ansi := [16]color.RGBA{
		{0x00, 0x00, 0x00, 0xff},
		{0x80, 0x00, 0x00, 0xff},
		{0x00, 0x80, 0x00, 0xff},
		{0x80, 0x80, 0x00, 0xff},
		{0x00, 0x00, 0x80, 0xff},
		{0x80, 0x00, 0x80, 0xff},
		{0x00, 0x80, 0x80, 0xff},
		{0xc0, 0xc0, 0xc0, 0xff},
		{0x80, 0x80, 0x80, 0xff},
		{0xff, 0x00, 0x00, 0xff},
		{0x00, 0xff, 0x00, 0xff},
		{0xff, 0xff, 0x00, 0xff},
		{0x00, 0x00, 0xff, 0xff},
		{0xff, 0x00, 0xff, 0xff},
		{0x00, 0xff, 0xff, 0xff},
		{0xff, 0xff, 0xff, 0xff},
	}
	for i, c := range ansi {
		p[i] = c
	}

	for r := range 6 {
		for g := range 6 {
			for b := range 6 {
				i := 16 + 36*r + 6*g + b
				p[i] = color.RGBA{
					R: cubeComponent(r),
					G: cubeComponent(g),
					B: cubeComponent(b),
					A: 0xff,
				}
			}
		}
	}

	for i := range 23 {
		v := uint8(8 + i*10) //nolint:gosec // bounded by loop.
		p[232+i] = color.RGBA{R: v, G: v, B: v, A: 0xff}
	}

	// Reserve the last slot for GIF transparency (see TransparentIndex).
	p[TransparentIndex] = transparentMarkerColor()

	return p
}

func cubeComponent(v int) uint8 {
	if v == 0 {
		return 0
	}
	return uint8(55 + v*40) //nolint:gosec // bounded by caller.
}

// defaultGIFLUT maps 5-bit-per-channel RGB to the nearest xterm-256 palette
// index. Built once from Default; avoids draw.Draw's per-pixel
// palette search when targeting image.Paletted.
var (
	defaultGIFLUT     [32][32][32]uint8
	initDefaultGIFLUT = sync.OnceFunc(func() {
		pal := Default()
		for ri := range 32 {
			r := uint8(ri * 255 / 31) //nolint:gosec // ri is bounded.
			for gi := range 32 {
				g := uint8(gi * 255 / 31) //nolint:gosec // gi is bounded.
				for bi := range 32 {
					b := uint8(bi * 255 / 31)                                                           //nolint:gosec // bi is bounded.
					defaultGIFLUT[ri][gi][bi] = uint8(pal.Index(color.RGBA{R: r, G: g, B: b, A: 0xff})) //nolint:gosec // Index is bounded.
				}
			}
		}
	})
)

// Palettize maps src onto dst's palette. NRGBA sources (what the render
// package draws) and RGBA sources (what image/draw callers usually produce) use
// defaultGIFLUT; anything else falls back to draw.Draw's per-pixel palette
// search, which is hundreds of times slower.
func Palettize(dst *image.Paletted, src image.Image) {
	switch src := src.(type) {
	case *image.NRGBA:
		palettizePixels(dst, rgbaPixels{pix: src.Pix, stride: src.Stride, rect: src.Rect})
	case *image.RGBA:
		palettizePixels(dst, rgbaPixels{pix: src.Pix, stride: src.Stride, rect: src.Rect, premultiplied: true})
	default:
		draw.Draw(dst, src.Bounds(), src, src.Bounds().Min, draw.Src)
	}
}

// palettizePixels maps src through defaultGIFLUT, splitting large frames
// across goroutines.
func palettizePixels(dst *image.Paletted, src rgbaPixels) {
	initDefaultGIFLUT()
	runPalettizeSpans(dst, src, src.rect.Intersect(dst.Bounds()), nil)
}

// palettizeSpanCount returns how many row spans area is split into.
func palettizeSpanCount(area image.Rectangle) int {
	workers := palettizeWorkerCount(area)
	if workers <= 1 {
		return 1
	}
	height := area.Dy()
	rowsPerWorker := (height + workers - 1) / workers
	return (height + rowsPerWorker - 1) / rowsPerWorker
}

// runPalettizeSpans maps area of src into dst, through adaptive when non-nil
// and defaultGIFLUT otherwise, splitting large frames across goroutines
// started for the call, which end before it returns, the last span on the
// caller's own.
func runPalettizeSpans(dst *image.Paletted, src rgbaPixels, area image.Rectangle, adaptive *Adaptive) {
	spans := palettizeSpanCount(area)
	if adaptive != nil {
		adaptive.resetSpans(spans)
	}
	if spans <= 1 {
		palettizeSpan(palettizeJob{dst: dst, src: src, area: area, minY: area.Min.Y, maxY: area.Max.Y, adaptive: adaptive})
		return
	}

	rowsPerSpan := (area.Dy() + spans - 1) / spans
	var wg sync.WaitGroup
	y0 := area.Min.Y
	for i := range spans {
		y1 := min(y0+rowsPerSpan, area.Max.Y)
		job := palettizeJob{dst: dst, src: src, area: area, minY: y0, maxY: y1, adaptive: adaptive, span: i}
		if i == spans-1 {
			palettizeSpan(job)
		} else {
			wg.Go(func() { palettizeSpan(job) })
		}
		y0 = y1
	}
	wg.Wait()
}

func palettizeSpan(job palettizeJob) {
	if job.adaptive != nil {
		job.adaptive.writeSpan(job.span, job.dst, job.src, job.area, job.minY, job.maxY)
		return
	}
	palettizeRows(job.dst, job.src, job.area, job.minY, job.maxY)
}

func palettizeWorkerCount(area image.Rectangle) int {
	width, height := area.Dx(), area.Dy()
	if width <= 0 || height <= 0 {
		return 1
	}

	byteCount := width * height * 4
	if byteCount < palettizeParallelMinBytes {
		return 1
	}

	return max(min(
		runtime.GOMAXPROCS(0),
		height/palettizeParallelRowsPerWorker,
		byteCount/palettizeParallelBytesPerWorker,
	), 1)
}

// palettizeRows maps rows minY through maxY of src through defaultGIFLUT.
// Alpha 0 writes TransparentIndex. Other pixels map by their color
// alone, so a translucent pixel becomes its opaque color; premultiplied
// colors are restored first so RGBA and NRGBA sources match. Opaque LUT hits
// on TransparentIndex are remapped to TransparentIndex-1 so the
// transparent slot stays reserved.
func palettizeRows(dst *image.Paletted, src rgbaPixels, area image.Rectangle, minY, maxY int) {
	width := area.Dx()
	for y := minY; y < maxY; y++ {
		si := (y-src.rect.Min.Y)*src.stride + (area.Min.X-src.rect.Min.X)*4
		di := (y-dst.Rect.Min.Y)*dst.Stride + (area.Min.X - dst.Rect.Min.X)
		srcPix := src.pix[si : si+width*4]
		dstPix := dst.Pix[di : di+width]
		for x := range dstPix {
			sx := x * 4
			a := srcPix[sx+3]
			if a == 0 {
				dstPix[x] = TransparentIndex
				continue
			}
			r, g, b := srcPix[sx], srcPix[sx+1], srcPix[sx+2]
			if src.premultiplied && a != 0xff {
				r, g, b = unpremultiply(r, a), unpremultiply(g, a), unpremultiply(b, a)
			}
			idx := defaultGIFLUT[r>>3][g>>3][b>>3]
			if idx == TransparentIndex {
				idx = TransparentIndex - 1
			}
			dstPix[x] = idx
		}
	}
}

// unpremultiply restores a color channel premultiplied by alpha a, which must
// not be zero, rounding like [color.NRGBAModel].
func unpremultiply(c, a uint8) uint8 {
	return uint8(min(uint32(c)*0xffff/uint32(a)>>8, 0xff)) //nolint:gosec // clamped.
}
