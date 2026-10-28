// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"errors"
	"fmt"

	"github.com/lrstanley/x/term/reel/render/internal/config"
)

const (
	// minFitFontPx is the smallest font size [FitScale] tries.
	minFitFontPx = 1.0
	// maxFitFontPx is the largest font size [FitScale] tries, as drawing
	// glyphs much larger than any screen is slow and uses a lot of memory.
	maxFitFontPx = 4096.0
	// fitFontPxStep is the finest font size [FitScale] tells apart: faces are
	// sized in 26.6 fixed point, so a smaller step draws the same cells.
	fitFontPxStep = 1.0 / 64
)

// ScaleForFontSize returns the [WithScale] scale at which the font is px
// pixels tall, where a scale of 1 is the default size (11pt at 96 DPI, about
// 14.7px).
func ScaleForFontSize(px float64) float64 {
	return px / config.BaseFontSizePx
}

// Fit returns the most columns and rows of cells whose [Renderer.Layout] image
// fits within width by height pixels, counting the margin, padding, window
// chrome and scrollbar. Either is 0 when not even the surroundings fit. The
// result only grows as width and height do.
func (d *Renderer) Fit(width, height int) (cols, rows int) {
	d.mu.Lock()
	defer d.mu.Unlock()

	around := d.layout(0, 0).Image.Size()
	cell := d.metrics.CellSize()
	return max(0, (width-around.X)/cell.X), max(0, (height-around.Y)/cell.Y)
}

// FitScale returns the largest scale at which the image of a cols by rows
// screen drawn with opts fits within width by height pixels, to be given to
// [WithScale], so text is drawn at that size rather than an image resized to
// it. The image may be smaller than that in one direction, as the cell size
// is a whole number of pixels, and the fixed pixel lengths ([WithPadding],
// [WithMargin]) do not scale. A width or height of 0 sets no limit in that
// direction, so the other alone decides the scale.
//
// It is an error for opts to include [WithScale], for width and height to be
// negative or both 0, or when the image does not fit at the smallest font
// size tried (1px) or still fits at the largest (4096px).
func FitScale(cols, rows, width, height int, opts ...Option) (float64, error) {
	if cols < 1 || rows < 1 {
		return 0, fmt.Errorf("render: fit: screen %dx%d must be at least 1x1", cols, rows)
	}
	if width < 0 || height < 0 || width == 0 && height == 0 {
		return 0, fmt.Errorf("render: fit: size %dx%d needs a width or height, and neither negative", width, height)
	}
	d, err := New(opts...)
	if err != nil {
		return 0, err
	}
	defer d.Close()
	if d.opts.ScaleSet {
		return 0, errors.New("render: fit: the scale is chosen, so WithScale cannot be set")
	}

	fits := func(scale float64) (bool, error) {
		d.opts.Scale = scale
		if buildErr := d.buildRenderer(); buildErr != nil {
			return false, buildErr
		}
		size := d.layout(cols, rows).Image.Size()
		return (width == 0 || size.X <= width) && (height == 0 || size.Y <= height), nil
	}

	lo := ScaleForFontSize(minFitFontPx)
	ok, err := fits(lo)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, fmt.Errorf("render: fit: %dx%d cells do not fit in %dx%d pixels", cols, rows, width, height)
	}
	// The image grows with the scale, so double until it no longer fits, then
	// bisect. Every scale lo takes has been checked to fit.
	hi := lo
	for ok {
		lo, hi = hi, hi*2
		if hi > ScaleForFontSize(maxFitFontPx) {
			return 0, fmt.Errorf("render: fit: %dx%d cells still fit in %dx%d pixels at the largest font size (%gpx)", cols, rows, width, height, maxFitFontPx)
		}
		if ok, err = fits(hi); err != nil {
			return 0, err
		}
	}
	for step := ScaleForFontSize(fitFontPxStep); hi-lo > step; {
		mid := lo + (hi-lo)/2
		if ok, err = fits(mid); err != nil {
			return 0, err
		}
		if ok {
			lo = mid
		} else {
			hi = mid
		}
	}
	return lo, nil
}
