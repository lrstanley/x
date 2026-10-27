// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package palette maps frames to palettes of at most 256 colors for GIF and
// PNG, built from the colors a recording uses ([Adaptive]). It also compacts
// palettes to the colors their frames use ([Compact]).
//
// Index 255, [TransparentIndex], is reserved for transparency: fully
// transparent pixels map to it, and opaque pixels never do. The palettes it
// builds fill that slot with a zero-alpha marker.
package palette

import (
	"image"
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

// rgbaPixels is the pixel layout *image.NRGBA and *image.RGBA share: four
// bytes per pixel, R, G, B, A. RGBA's color channels are premultiplied by
// alpha.
type rgbaPixels struct {
	pix           []uint8
	stride        int
	rect          image.Rectangle
	premultiplied bool
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

// writeSpans maps area of src into dst through the cache (see
// [Adaptive.writeSpan]), splitting large frames across goroutines started for
// the call, which end before it returns, the last span on the caller's own.
func (p *Adaptive) writeSpans(dst *image.Paletted, src rgbaPixels, area image.Rectangle) {
	spans := palettizeSpanCount(area)
	p.resetSpans(spans)
	if spans <= 1 {
		p.writeSpan(0, dst, src, area, area.Min.Y, area.Max.Y)
		return
	}

	rowsPerSpan := (area.Dy() + spans - 1) / spans
	var wg sync.WaitGroup
	y0 := area.Min.Y
	for i := range spans {
		minY, maxY := y0, min(y0+rowsPerSpan, area.Max.Y)
		if i == spans-1 {
			p.writeSpan(i, dst, src, area, minY, maxY)
		} else {
			wg.Go(func() { p.writeSpan(i, dst, src, area, minY, maxY) })
		}
		y0 = maxY
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

// unpremultiply restores a color channel premultiplied by alpha a, which must
// not be zero, rounding like [color.NRGBAModel].
func unpremultiply(c, a uint8) uint8 {
	return uint8(min(uint32(c)*0xffff/uint32(a)>>8, 0xff)) //nolint:gosec // clamped.
}
