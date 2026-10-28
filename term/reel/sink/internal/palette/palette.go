// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package palette builds GIF and PNG palettes from the colors a recording
// uses ([Adaptive]), and compacts them to the colors their frames use
// ([Compact]).
//
// [TransparentIndex] is reserved for transparency: only fully transparent
// pixels map to it, and every palette built here holds a zero-alpha color
// there.
package palette

import (
	"image"
	"runtime"
	"sync"
)

// TransparentIndex is the palette index reserved for transparency.
const TransparentIndex uint8 = 255

const (
	palettizeParallelMinBytes       = 1 << 20
	palettizeParallelBytesPerWorker = 512 << 10
	palettizeParallelRowsPerWorker  = 64
)

// rgbaPixels is the pixel layout *image.NRGBA and *image.RGBA share.
type rgbaPixels struct {
	pix           []uint8
	stride        int
	rect          image.Rectangle
	premultiplied bool
}

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
// [Adaptive.writeSpan]), splitting large frames across goroutines.
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
