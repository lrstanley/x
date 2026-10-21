// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package export

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"runtime"
	"sync"
)

// TransparentPaletteIndex is the palette index reserved for transparency during
// GIF capture and optimization. See package documentation for the full invariant.
const TransparentPaletteIndex uint8 = 255

const (
	palettizeParallelMinBytes       = 1 << 20
	palettizeParallelBytesPerWorker = 512 << 10
	palettizeParallelRowsPerWorker  = 64
)

// palettizeJob is one disjoint row span for the process-lifetime workers.
type palettizeJob struct {
	dst  *image.Paletted
	src  rgbaPixels
	area image.Rectangle
	minY int
	maxY int
	wg   *sync.WaitGroup
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

// palettizeJobs is filled by palettizePixels and drained by a fixed worker set.
// The workers live for the process so a frame does not allocate goroutines.
var (
	palettizeJobs chan palettizeJob
	palettizeN    int
	// palettizeWaiters reuses the per-frame WaitGroup. A fresh one escapes and
	// allocates; the pool does not.
	palettizeWaiters      = sync.Pool{New: func() any { return new(sync.WaitGroup) }}
	startPalettizeWorkers = sync.OnceFunc(func() {
		palettizeN = max(runtime.GOMAXPROCS(0), 1)
		// One buffered slot per worker lets a frame enqueue every span without
		// waiting for the first span to finish.
		palettizeJobs = make(chan palettizeJob, palettizeN)
		for range palettizeN {
			go palettizeWorker()
		}
	})
)

func palettizeWorker() {
	for job := range palettizeJobs {
		palettizeRows(job.dst, job.src, job.area, job.minY, job.maxY)
		job.wg.Done()
	}
}

// defaultGIFPalette returns the standard xterm-256color palette for GIF
// encoding. Terminal renders mostly use ANSI, cube, and grayscale slots;
// mapping through this palette preserves lipgloss and emulator colors better
// than generic palettes such as Plan9.
func defaultGIFPalette() color.Palette {
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

	// Reserve the last slot for GIF transparency (see TransparentPaletteIndex).
	p[TransparentPaletteIndex] = transparentMarkerColor()

	return p
}

func cubeComponent(v int) uint8 {
	if v == 0 {
		return 0
	}
	return uint8(55 + v*40) //nolint:gosec // bounded by caller.
}

// defaultGIFLUT maps 5-bit-per-channel RGB to the nearest xterm-256 palette
// index. Built once from defaultGIFPalette; avoids draw.Draw's per-pixel
// palette search when targeting image.Paletted.
var (
	defaultGIFLUT     [32][32][32]uint8
	initDefaultGIFLUT = sync.OnceFunc(func() {
		pal := defaultGIFPalette()
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

// palettizeInto maps src onto dst's palette. NRGBA sources (what still
// renders) and RGBA sources (what image/draw callers usually produce) use
// defaultGIFLUT; anything else falls back to draw.Draw's per-pixel palette
// search, which is hundreds of times slower.
func palettizeInto(dst *image.Paletted, src image.Image) {
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
// across the palettize workers.
func palettizePixels(dst *image.Paletted, src rgbaPixels) {
	initDefaultGIFLUT()
	area := src.rect.Intersect(dst.Bounds())
	workers := palettizeWorkerCount(area)
	if workers > 1 {
		startPalettizeWorkers()
		if workers > palettizeN {
			workers = palettizeN
		}
	}
	if workers <= 1 {
		palettizeRows(dst, src, area, area.Min.Y, area.Max.Y)
		return
	}

	height := area.Dy()
	rowsPerWorker := (height + workers - 1) / workers
	spans := (height + rowsPerWorker - 1) / rowsPerWorker
	wg := palettizeWaiters.Get().(*sync.WaitGroup)
	wg.Add(spans)
	y0 := area.Min.Y
	for range spans {
		y1 := min(y0+rowsPerWorker, area.Max.Y)
		palettizeJobs <- palettizeJob{
			dst:  dst,
			src:  src,
			area: area,
			minY: y0,
			maxY: y1,
			wg:   wg,
		}
		y0 = y1
	}
	wg.Wait()
	palettizeWaiters.Put(wg)
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
// Alpha 0 writes TransparentPaletteIndex. Other pixels map by their color
// alone, so a translucent pixel becomes its opaque color; premultiplied
// colors are restored first so RGBA and NRGBA sources match. Opaque LUT hits
// on TransparentPaletteIndex are remapped to TransparentPaletteIndex-1 so the
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
				dstPix[x] = TransparentPaletteIndex
				continue
			}
			r, g, b := srcPix[sx], srcPix[sx+1], srcPix[sx+2]
			if src.premultiplied && a != 0xff {
				r, g, b = unpremultiply(r, a), unpremultiply(g, a), unpremultiply(b, a)
			}
			idx := defaultGIFLUT[r>>3][g>>3][b>>3]
			if idx == TransparentPaletteIndex {
				idx = TransparentPaletteIndex - 1
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

func clonePaletted(src *image.Paletted) *image.Paletted {
	return &image.Paletted{
		Pix:     bytes.Clone(src.Pix),
		Stride:  src.Stride,
		Rect:    src.Rect,
		Palette: src.Palette,
	}
}
