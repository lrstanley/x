// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"time"

	"github.com/lrstanley/x/term/reel"
)

// highlightDim is how much [Highlight] darkens the rest of the frame unless
// [DimOthers] says otherwise.
const highlightDim = 0.5

// outlineColor is the default color of an [Outline], a warm yellow, xterm
// 221.
var outlineColor = color.NRGBA{R: 0xff, G: 0xd7, B: 0x5f, A: 0xff}

// HighlightOption configures [Highlight]: [DimOthers], [Outline], and the
// [Timing] options every region effect takes.
type HighlightOption interface {
	applyHighlight(h *highlight)
}

type highlightOption func(*highlight)

func (o highlightOption) applyHighlight(h *highlight) { o(h) }

// DimOthers sets how much [Highlight] darkens everything outside its
// region, window chrome and margins included: each color is moved toward
// black by factor, from 0, which leaves it alone, to 1, which makes it
// black. The default is 0.5, half as bright. A factor outside 0 to 1 is an
// error from Start.
func DimOthers(factor float64) HighlightOption {
	return highlightOption(func(h *highlight) { h.dim = factor })
}

// Outline draws a border px pixels wide around each rectangle of cells the
// [Highlight] region covers, outside it, so it hides none of the region. A
// nil c is a warm yellow. A px of zero scales with the font: an eighth of a
// cell's height, at least 1. A px below zero is an error from Start.
func Outline(c color.Color, px int) HighlightOption {
	return highlightOption(func(h *highlight) {
		h.outline, h.outlinePx = outlineColor, px
		if c != nil {
			h.outline, _ = color.NRGBAModel.Convert(c).(color.NRGBA)
		}
	})
}

// Highlight returns an effect that draws attention to the cells of region:
// it darkens the rest of the frame (see [DimOthers]), and can draw an
// [Outline] around the region. The region is matched on each terminal frame,
// from its cells, so a [Text] region follows its text as it moves. A frame
// the region covers no cells of is left alone, as are frames without a
// terminal, such as cards. [Between] limits the highlight to a window of
// the timeline, and [Ease], [EaseIn], and [EaseOut] fade it in and out.
//
// The cells are mapped to pixels with the layout of the recording's
// rasterizer, so it works with any [reel.Rasterizer]. The highlight is drawn
// on copies of the frames. Dimmed frames are palettized on their own GIF
// palette ([reel.Frame.Isolated]), so their darker colors leave the
// recording's palette alone. List Highlight before effects whose overlays
// should stay bright, such as [Keys] and [PauseBadge].
//
// A nil region is an error from Start. A highlight that neither dims nor
// outlines leaves the recording alone.
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
}

func (e highlight) Start(ctx *reel.Context) (reel.Stage, error) {
	switch {
	case e.region == nil:
		return nil, errors.New("effects: highlight without a region")
	case e.dim < 0 || e.dim > 1 || math.IsNaN(e.dim):
		return nil, fmt.Errorf("effects: highlight dim %v is outside 0 to 1", e.dim)
	case e.outlinePx < 0:
		return nil, fmt.Errorf("effects: highlight outline width %d is below zero", e.outlinePx)
	}
	w, err := e.timing.start(ctx, easeAfterMark)
	if err != nil {
		return nil, err
	}
	if e.dim == 0 && e.outline.A == 0 {
		return nil, nil //nolint:nilnil // A nil stage leaves the recording alone.
	}
	px := e.outlinePx
	if px == 0 {
		px = max(1, ctx.Layout.Cell.Y/8)
	}
	s := &highlightStage{
		pixels:    regionPixels{region: e.region, rast: ctx.Rasterizer},
		window:    w,
		dim:       e.dim,
		outline:   e.outline,
		outlinePx: px,
	}
	return s.run, nil
}

// highlightStage is the highlight for one recording.
type highlightStage struct {
	pixels    regionPixels
	window    *window
	dim       float64
	outline   color.NRGBA
	outlinePx int
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
	rects := s.pixels.find(f.Cells)
	if len(rects) == 0 {
		emit(f)
		return
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
// over rects: the rest dimmed, the outlines drawn around rects, and then
// rects as src has them, so neither covers them.
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

// dimNRGBA scales the color channels of every pixel of img by keep, from 0
// to 1, leaving alpha alone.
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

// drawOutline draws a border px wide around r, outside it, in src, clipped
// to img.
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
