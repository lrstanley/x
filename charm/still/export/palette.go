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

func palettizeInto(dst *image.Paletted, src image.Image) {
	if nrgba, ok := src.(*image.NRGBA); ok {
		initDefaultGIFLUT()
		palettizeNRGBA(dst, nrgba)
		return
	}
	draw.Draw(dst, src.Bounds(), src, src.Bounds().Min, draw.Src)
}

// palettizeNRGBA maps opaque NRGBA pixels through defaultGIFLUT. Alpha 0
// writes TransparentPaletteIndex; opaque LUT hits on that index are remapped
// to TransparentPaletteIndex-1 so the transparent slot stays reserved.
func palettizeNRGBA(dst *image.Paletted, src *image.NRGBA) {
	area := src.Bounds().Intersect(dst.Bounds())
	workers := palettizeWorkerCount(area)
	if workers == 1 {
		palettizeNRGBARows(dst, src, area, area.Min.Y, area.Max.Y)
		return
	}

	rowsPerWorker := (area.Dy() + workers - 1) / workers
	var wg sync.WaitGroup
	for y0 := area.Min.Y; y0 < area.Max.Y; y0 += rowsPerWorker {
		y1 := min(y0+rowsPerWorker, area.Max.Y)
		wg.Go(func() {
			palettizeNRGBARows(dst, src, area, y0, y1)
		})
	}
	wg.Wait()
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

func palettizeNRGBARows(dst *image.Paletted, src *image.NRGBA, area image.Rectangle, minY, maxY int) {
	width := area.Dx()
	for y := minY; y < maxY; y++ {
		si := (y-src.Rect.Min.Y)*src.Stride + (area.Min.X-src.Rect.Min.X)*4
		di := (y-dst.Rect.Min.Y)*dst.Stride + (area.Min.X - dst.Rect.Min.X)
		srcPix := src.Pix[si : si+width*4]
		dstPix := dst.Pix[di : di+width]
		for x := range dstPix {
			sx := x * 4
			if srcPix[sx+3] == 0 {
				dstPix[x] = TransparentPaletteIndex
				continue
			}
			r, g, b := srcPix[sx], srcPix[sx+1], srcPix[sx+2]
			idx := defaultGIFLUT[r>>3][g>>3][b>>3]
			if idx == TransparentPaletteIndex {
				idx = TransparentPaletteIndex - 1
			}
			dstPix[x] = idx
		}
	}
}

func clonePaletted(src *image.Paletted) *image.Paletted {
	return &image.Paletted{
		Pix:     bytes.Clone(src.Pix),
		Stride:  src.Stride,
		Rect:    src.Rect,
		Palette: src.Palette,
	}
}
