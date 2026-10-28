// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"cmp"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"time"

	"github.com/lrstanley/x/term/reel"
)

// highlightDim is the default of [DimOthers].
const highlightDim = 0.5

// outlineColor is the default color of an [Outline], xterm 221.
var outlineColor = color.NRGBA{R: 0xff, G: 0xd7, B: 0x5f, A: 0xff}

// HighlightOption configures [Highlight]: [DimOthers], [Outline],
// [Padding], and the [Timing] options.
type HighlightOption interface {
	applyHighlight(h *highlight)
}

type highlightOption func(*highlight)

func (o highlightOption) applyHighlight(h *highlight) { o(h) }

// DimOthers sets how much [Highlight] darkens everything outside its
// region, window chrome and margins included, from 0, unchanged, to 1,
// black. The default is 0.5. A factor outside 0 to 1 is an error from
// Start.
func DimOthers(factor float64) HighlightOption {
	return highlightOption(func(h *highlight) { h.dim = factor })
}

// Outline draws a border px pixels wide around each rectangle of cells the
// [Highlight] region covers, outside its [Padding]. A nil c is a warm
// yellow. A px of zero is an eighth of a cell's height, at least 1. A px
// below zero is an error from Start.
func Outline(c color.Color, px int) HighlightOption {
	return highlightOption(func(h *highlight) {
		h.outline, h.outlinePx = outlineColor, px
		if c != nil {
			h.outline, _ = color.NRGBAModel.Convert(c).(color.NRGBA)
		}
	})
}

// Padding sets how many pixels around each rectangle of cells the
// [Highlight] region covers are left undimmed, on every side, with the
// [Outline] drawn outside them. Zero means no padding, so the outline sits
// against the cells. The default is a third of a cell's width, at least 1.
// Near the edges of the image, the padding shrinks so the outline still
// fits, but never into the cells. A px below zero is an error from Start.
func Padding(px int) HighlightOption {
	return highlightOption(func(h *highlight) { h.pad, h.padSet = px, true })
}

// Highlight returns an effect that draws attention to the cells of region:
// it darkens all of the frame but the region and its [Padding] (see
// [DimOthers]), and can draw an [Outline] around them. The region is matched
// on each terminal frame, so a [Text] region follows its text. It takes the
// [Timing] options and works with any [reel.Rasterizer]. See the package
// documentation for where to list it.
//
// A nil region is an error from Start.
func Highlight(region Region, opts ...HighlightOption) reel.Effect {
	h := highlight{region: region, dim: highlightDim}
	for _, opt := range opts {
		if opt != nil {
			opt.applyHighlight(&h)
		}
	}
	return h
}

type highlight struct {
	region    Region
	timing    timing
	dim       float64
	outline   color.NRGBA
	outlinePx int
	// pad is the padding when padSet, and scales with the font otherwise.
	pad    int
	padSet bool
}

func (e highlight) Start(ctx *reel.Context) (reel.Stage, error) {
	switch {
	case e.region == nil:
		return nil, errors.New("effects: highlight without a region")
	case e.dim < 0 || e.dim > 1 || math.IsNaN(e.dim):
		return nil, fmt.Errorf("effects: highlight dim %v is outside 0 to 1", e.dim)
	case e.outlinePx < 0:
		return nil, fmt.Errorf("effects: highlight outline width %d is below zero", e.outlinePx)
	case e.pad < 0:
		return nil, fmt.Errorf("effects: highlight padding %d is below zero", e.pad)
	}
	w, err := e.timing.start(ctx, easeAfterMark)
	if err != nil {
		return nil, err
	}
	if e.dim == 0 && e.outline.A == 0 {
		return nil, nil //nolint:nilnil // A nil stage leaves the recording alone.
	}
	s := &highlightStage{
		pixels:  regionPixels{region: e.region, rast: ctx.Rasterizer},
		window:  w,
		dim:     e.dim,
		outline: e.outline,
		pad:     e.pad,
	}
	if e.outline.A > 0 {
		s.outlinePx = cmp.Or(e.outlinePx, max(1, ctx.Layout.Cell.Y/8))
	}
	if !e.padSet {
		s.pad = max(1, ctx.Layout.Cell.X/3)
	}
	return s.run, nil
}

// highlightStage is the highlight for one recording.
type highlightStage struct {
	pixels    regionPixels
	window    *window
	dim       float64
	outline   color.NRGBA
	outlinePx int // Zero without an outline.
	pad       int
}

func (s *highlightStage) run(f reel.Frame, emit func(reel.Frame)) {
	if f.Cells == nil {
		emit(f)
		return
	}
	pieces := s.window.pieces(f)
	if len(pieces) == 1 && pieces[0].level == 0 {
		emit(f)
		return
	}
	cells := s.pixels.find(f.Cells)
	if len(cells) == 0 {
		emit(f)
		return
	}
	rects := make([]image.Rectangle, len(cells))
	for i, r := range cells {
		// Union keeps the cells whole where the image's edge cuts into
		// the padding.
		rects[i] = r.Inset(-s.pad).Intersect(f.Image.Rect.Inset(s.outlinePx)).Union(r)
	}
	for _, p := range pieces {
		out := f
		out.At = f.At + time.Duration(float64(p.start)*s.window.speed)
		out.Delay = p.delay
		if p.level > 0 {
			out.Image = s.draw(f.Image, rects, p.level)
			out.Isolated = f.Isolated || s.dim > 0
		}
		emit(out)
	}
}

// draw returns a copy of src with the highlight at strength level drawn
// around rects, the region's cells with their padding, which are left as
// src has them.
func (s *highlightStage) draw(src *image.NRGBA, rects []image.Rectangle, level float64) *image.NRGBA {
	img := cloneNRGBA(src)
	if s.dim > 0 {
		dimNRGBA(img, 1-s.dim*level)
	}
	if c := s.outline; c.A > 0 {
		c.A = uint8(math.Round(float64(c.A) * level))
		u := image.NewUniform(c)
		for _, r := range rects {
			drawOutline(img, r, s.outlinePx, u)
		}
	}
	for _, r := range rects {
		draw.Draw(img, r, src, r.Min, draw.Src)
	}
	return img
}

// dimNRGBA scales the color channels of every pixel of img by keep, leaving
// alpha alone.
func dimNRGBA(img *image.NRGBA, keep float64) {
	var scale [256]uint8
	for i := range scale {
		scale[i] = uint8(math.Round(float64(i) * keep))
	}
	b := img.Rect
	for y := b.Min.Y; y < b.Max.Y; y++ {
		row := img.Pix[img.PixOffset(b.Min.X, y):img.PixOffset(b.Max.X, y)]
		for i := 0; i+3 < len(row); i += 4 {
			row[i], row[i+1], row[i+2] = scale[row[i]], scale[row[i+1]], scale[row[i+2]]
		}
	}
}

// drawOutline draws a border px wide around the outside of r, in src.
func drawOutline(img *image.NRGBA, r image.Rectangle, px int, src *image.Uniform) {
	o := r.Inset(-px)
	for _, side := range []image.Rectangle{
		image.Rect(o.Min.X, o.Min.Y, o.Max.X, r.Min.Y),
		image.Rect(o.Min.X, r.Max.Y, o.Max.X, o.Max.Y),
		image.Rect(o.Min.X, r.Min.Y, r.Min.X, r.Max.Y),
		image.Rect(r.Max.X, r.Min.Y, o.Max.X, r.Max.Y),
	} {
		draw.Draw(img, side.Intersect(img.Rect), src, image.Point{}, draw.Over)
	}
}
