// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package gifopt rewrites GIF frames as sub-rect frames with disposal
// methods, like gifsicle -O1/-O2. Unchanged pixels of a sub-rect frame are
// [palette.TransparentIndex].
package gifopt

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"time"

	"github.com/lrstanley/x/term/reel/sink/internal/palette"
)

const (
	// dirtyRectThreshold is the fraction of canvas area changed at which a
	// full keyframe is written instead of a sub-rect frame.
	dirtyRectThreshold = 0.5

	// smallOverlayThreshold and maxOverlayExtentPx bound the changed area
	// fraction and sub-rect size of a cursor-sized overlay that uses
	// DisposalPrevious.
	smallOverlayThreshold = 0.02
	maxOverlayExtentPx    = 64
)

type framePlanKind int

const (
	planFullKeyframe framePlanKind = iota
	planSubRect
	planReveal
)

// framePlan is one output frame.
type framePlan struct {
	kind     framePlanKind
	delay    time.Duration
	disposal byte
	image    *image.Paletted
}

// disposalCanvas is the simulated screen after each planned frame. view may
// alias an output image until owned, after which it is a private copy.
type disposalCanvas struct {
	view  *image.Paletted
	owned bool
}

// mutable returns view, copying it first if it is not owned.
func (c *disposalCanvas) mutable() *image.Paletted {
	if !c.owned {
		c.view = Clone(c.view)
		c.owned = true
	}
	return c.view
}

// Optimize rewrites full frames as sub-rect frames, returning the new frames
// with their delays and disposal methods. When deltaAt is not negative, the
// frames after frames[deltaAt] are instead crops of what changed since the
// frame before, as [BlitOpaque] applies them. screen must not be empty, and
// there must be at least two frames, with a delay for each.
//
// Only [gif.DisposalNone] and [gif.DisposalPrevious] are used, so the GIF's
// background color never shows.
func Optimize(
	screen image.Rectangle,
	frames []*image.Paletted,
	delays []time.Duration,
	deltaAt int,
) (outFrames []*image.Paletted, outDelays []time.Duration, disposals []byte) {
	// The first frame is the keyframe, aliased by the canvas until it is
	// written to.
	first := frameForDiff(frames[0], screen)
	canvas := disposalCanvas{view: first}

	plans, firstDelayExtra := planDisposalFrames(frames, delays, deltaAt, &canvas, screen)
	outFrames, outDelays, disposals = collectDisposalOutput(plans, 1+len(plans))

	outFrames[0] = first
	outDelays[0] = delays[0] + firstDelayExtra
	disposals[0] = gif.DisposalNone
	return outFrames, outDelays, disposals
}

// planDisposalFrames diffs each frame after the first against the simulated
// canvas, plans its output frame, and applies it to the canvas. A frame on a
// different palette than the canvas is diffed by color instead of index.
func planDisposalFrames(frames []*image.Paletted, delays []time.Duration, deltaAt int, canvas *disposalCanvas, screen image.Rectangle) (plans []framePlan, firstDelayExtra time.Duration) {
	n := len(frames)
	canvasArea := screen.Dx() * screen.Dy()
	plans = make([]framePlan, 0, n-1)

	// cur rebuilds full frames of the delta segment on a rolling clone.
	var cur *image.Paletted
	for i := 1; i < n; i++ {
		var full *image.Paletted
		switch {
		case deltaAt >= 0 && i > deltaAt:
			if cur == nil {
				base := frames[deltaAt]
				cur = Clone(frameForDiff(base, screen))
			}
			BlitOpaque(frames[i], cur)
			full = cur
		default:
			full = frameForDiff(frames[i], screen)
		}
		framePal := full.Palette
		// After a palette switch, pixels outside what changed color already
		// show what full has, so the canvas becomes full, on its palette.
		switched := !palette.Same(canvas.view.Palette, framePal)
		diff := Diff
		if switched {
			diff = diffColorBoundingBox
		}
		bbox, diffArea := diff(canvas.view, full, screen)
		if diffArea == 0 {
			if switched {
				canvas.view, canvas.owned = Clone(full), true
			}
			if len(plans) == 0 {
				firstDelayExtra += delays[i]
				continue
			}
			planIdleMergeOrReveal(&plans, framePal, delays[i])
			continue
		}

		var plan framePlan
		switch {
		case float64(diffArea)/float64(canvasArea) >= dirtyRectThreshold:
			plan = framePlan{
				kind:     planFullKeyframe,
				delay:    delays[i],
				disposal: gif.DisposalNone,
				image:    Clone(full),
			}
		case switched:
			// An opaque crop, as subRectFrame compares indices, which name
			// different colors on the two palettes.
			plan = framePlan{
				kind:     planSubRect,
				delay:    delays[i],
				disposal: gif.DisposalNone,
				image:    Crop(full, bbox),
			}
		default:
			disposal := byte(gif.DisposalNone)
			if overlayEligible(bbox, diffArea, canvasArea) {
				disposal = gif.DisposalPrevious
			}
			// DisposalBackground is never used: it clears the whole sub-rect,
			// including transparent pixels that show the canvas beneath.
			//
			// A bbox of mostly changed pixels encodes smaller as an opaque
			// crop, since transparent holes fragment runs of one index.
			var patch *image.Paletted
			bboxArea := bbox.Dx() * bbox.Dy()
			unchanged := bboxArea - diffArea
			if bboxArea > 0 && unchanged >= 0 && unchanged*4 < bboxArea {
				patch = Crop(full, bbox)
			} else {
				patch = subRectFrame(canvas.view, full, bbox)
			}
			plan = framePlan{
				kind:     planSubRect,
				delay:    delays[i],
				disposal: disposal,
				image:    patch,
			}
		}

		plans = append(plans, plan)
		if switched && plan.kind == planSubRect {
			canvas.view, canvas.owned = Clone(full), true
			continue
		}
		applyPlanToCanvas(canvas, plan)
	}

	return plans, firstDelayExtra
}

// applyPlanToCanvas updates the simulated canvas after plan. DisposalPrevious
// restores the screen before the frame, so it leaves the canvas as is.
func applyPlanToCanvas(canvas *disposalCanvas, plan framePlan) {
	switch plan.kind {
	case planFullKeyframe:
		canvas.view = plan.image
		canvas.owned = false
	case planReveal:
		// Only triggers the previous frame's disposal.
	case planSubRect:
		if plan.disposal != gif.DisposalPrevious {
			blitTransparent(plan.image, canvas.mutable())
		}
	}
}

// planIdleMergeOrReveal handles a frame identical to the canvas. Its delay
// merges into the previous output frame, unless that frame used
// DisposalPrevious: viewers still show it until the next frame, so a
// [revealFrame] is added to trigger the disposal.
func planIdleMergeOrReveal(plans *[]framePlan, pal color.Palette, delay time.Duration) {
	last := len(*plans) - 1
	if (*plans)[last].disposal == gif.DisposalPrevious {
		*plans = append(*plans, framePlan{
			kind:     planReveal,
			delay:    delay,
			disposal: gif.DisposalNone,
			image:    revealFrame(pal),
		})
		return
	}
	(*plans)[last].delay += delay
}

// collectDisposalOutput copies plans into output slices from index 1, leaving
// index 0 for the keyframe.
func collectDisposalOutput(plans []framePlan, n int) (images []*image.Paletted, delays []time.Duration, disposals []byte) {
	images = make([]*image.Paletted, n)
	delays = make([]time.Duration, n)
	disposals = make([]byte, n)
	for i, plan := range plans {
		images[i+1] = plan.image
		delays[i+1] = plan.delay
		disposals[i+1] = plan.disposal
	}
	return images, delays, disposals
}

func overlayEligible(bbox image.Rectangle, diffArea, canvasArea int) bool {
	if canvasArea <= 0 || diffArea <= 0 {
		return false
	}
	if float64(diffArea)/float64(canvasArea) >= smallOverlayThreshold {
		return false
	}
	return bbox.Dx() <= maxOverlayExtentPx && bbox.Dy() <= maxOverlayExtentPx
}

// frameForDiff returns frame drawn on the full screen. A frame that already
// covers it is returned as is and must not be mutated.
func frameForDiff(frame *image.Paletted, screen image.Rectangle) *image.Paletted {
	if frame.Bounds().Eq(screen) {
		return frame
	}
	dst := image.NewPaletted(screen, frame.Palette)
	draw.Draw(dst, frame.Bounds().Intersect(screen), frame, frame.Bounds().Min, draw.Src)
	return dst
}

// revealFrame is a 1×1 transparent frame, which triggers the previous frame's
// disposal without changing the canvas.
func revealFrame(pal color.Palette) *image.Paletted {
	sub := image.NewPaletted(image.Rect(0, 0, 1, 1), pal)
	sub.SetColorIndex(0, 0, palette.TransparentIndex)
	return sub
}

// Diff returns the bounding box of the pixel indices that differ between
// canvas and full within screen, and how many differ.
func Diff(canvas, full *image.Paletted, screen image.Rectangle) (bbox image.Rectangle, diff int) {
	if canvas != nil && full != nil && canvas.Rect.Eq(screen) && full.Rect.Eq(screen) {
		if fast, n, ok := diffBoundingBoxRows(canvas, full, screen); ok {
			return fast, n
		}
	}
	return diffBoundingBoxFunc(screen, func(x, y int) bool {
		return indexAt(canvas, x, y) == indexAt(full, x, y)
	})
}

// diffBoundingBoxFunc returns the smallest rectangle enclosing the pixels of
// screen that same reports differ, and how many there are.
func diffBoundingBoxFunc(screen image.Rectangle, same func(x, y int) bool) (bbox image.Rectangle, diff int) {
	for y := screen.Min.Y; y < screen.Max.Y; y++ {
		for x := screen.Min.X; x < screen.Max.X; x++ {
			if !same(x, y) {
				diff++
				bbox = bbox.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	return bbox, diff
}

// diffBoundingBoxRows is the fast path of [Diff] for frames covering screen.
// ok is false when the pixel storage cannot cover the screen.
func diffBoundingBoxRows(canvas, full *image.Paletted, screen image.Rectangle) (bbox image.Rectangle, diff int, ok bool) {
	width := screen.Dx()
	height := screen.Dy()
	if width <= 0 || height <= 0 || canvas.Stride < width || full.Stride < width {
		return image.Rectangle{}, 0, false
	}
	needC := (height-1)*canvas.Stride + width
	needF := (height-1)*full.Stride + width
	if len(canvas.Pix) < needC || len(full.Pix) < needF {
		return image.Rectangle{}, 0, false
	}

	minX, maxX := width, -1
	minY, maxY := -1, -1
	for row := range height {
		ca := canvas.Pix[row*canvas.Stride : row*canvas.Stride+width]
		fa := full.Pix[row*full.Stride : row*full.Stride+width]
		if bytes.Equal(ca, fa) {
			continue
		}
		lo := 0
		for lo < width && ca[lo] == fa[lo] {
			lo++
		}
		hi := width - 1
		for hi > lo && ca[hi] == fa[hi] {
			hi--
		}
		rowDiff := 0
		for x := lo; x <= hi; x++ {
			if ca[x] != fa[x] {
				rowDiff++
			}
		}
		diff += rowDiff
		if lo < minX {
			minX = lo
		}
		if hi > maxX {
			maxX = hi
		}
		if minY < 0 {
			minY = row
		}
		maxY = row
	}
	if diff == 0 {
		return image.Rectangle{}, 0, true
	}
	return image.Rect(screen.Min.X+minX, screen.Min.Y+minY, screen.Min.X+maxX+1, screen.Min.Y+maxY+1), diff, true
}

// diffColorBoundingBox is [Diff] for frames on different palettes: a pixel
// differs when the colors its indices name differ.
func diffColorBoundingBox(canvas, full *image.Paletted, screen image.Rectangle) (bbox image.Rectangle, diff int) {
	var cc, fc [256]uint32
	pack := func(dst *[256]uint32, pal color.Palette) {
		for i := range dst {
			if i < len(pal) && pal[i] != nil {
				r, g, b, a := pal[i].RGBA()
				dst[i] = r>>8<<24 | g>>8<<16 | b>>8<<8 | a>>8
			}
		}
	}
	pack(&cc, canvas.Palette)
	pack(&fc, full.Palette)
	return diffBoundingBoxFunc(screen, func(x, y int) bool {
		return cc[indexAt(canvas, x, y)] == fc[indexAt(full, x, y)]
	})
}

// indexAt returns the palette index of pm at (x, y), or
// [palette.TransparentIndex] outside its bounds.
func indexAt(pm *image.Paletted, x, y int) uint8 {
	if !image.Pt(x, y).In(pm.Bounds()) {
		return palette.TransparentIndex
	}
	return pm.Pix[(y-pm.Rect.Min.Y)*pm.Stride+(x-pm.Rect.Min.X)]
}

// subRectFrame returns bbox of full, on its palette, with pixels unchanged
// from canvas marked transparent.
func subRectFrame(canvas, full *image.Paletted, bbox image.Rectangle) *image.Paletted {
	sub := image.NewPaletted(bbox, full.Palette)
	width := bbox.Dx()
	for y := bbox.Min.Y; y < bbox.Max.Y; y++ {
		dst, okD := palettedSpan(sub, bbox.Min.X, y, width)
		cSpan, okC := palettedSpan(canvas, bbox.Min.X, y, width)
		fSpan, okF := palettedSpan(full, bbox.Min.X, y, width)
		if !okD || !okC || !okF {
			for x := bbox.Min.X; x < bbox.Max.X; x++ {
				if indexAt(canvas, x, y) == indexAt(full, x, y) {
					sub.SetColorIndex(x, y, palette.TransparentIndex)
				} else {
					sub.SetColorIndex(x, y, indexAt(full, x, y))
				}
			}
			continue
		}
		for x := range width {
			if cSpan[x] == fSpan[x] {
				dst[x] = palette.TransparentIndex
			} else {
				dst[x] = fSpan[x]
			}
		}
	}
	return sub
}

// Crop copies bbox out of src into a tightly packed frame.
func Crop(src *image.Paletted, bbox image.Rectangle) *image.Paletted {
	dst := image.NewPaletted(bbox, src.Palette)
	width := bbox.Dx()
	for y := bbox.Min.Y; y < bbox.Max.Y; y++ {
		out := dst.Pix[(y-bbox.Min.Y)*dst.Stride : (y-bbox.Min.Y)*dst.Stride+width]
		span, ok := palettedSpan(src, bbox.Min.X, y, width)
		if !ok {
			for x := bbox.Min.X; x < bbox.Max.X; x++ {
				dst.SetColorIndex(x, y, indexAt(src, x, y))
			}
			continue
		}
		copy(out, span)
	}
	return dst
}

// palettedSpan returns n palette indices starting at (x, y), or false when
// they are not all in pm.
func palettedSpan(pm *image.Paletted, x, y, n int) ([]byte, bool) {
	if pm == nil || n <= 0 || y < pm.Rect.Min.Y || y >= pm.Rect.Max.Y || x < pm.Rect.Min.X || x+n > pm.Rect.Max.X {
		return nil, false
	}
	i := (y-pm.Rect.Min.Y)*pm.Stride + (x - pm.Rect.Min.X)
	if i < 0 || i+n > len(pm.Pix) {
		return nil, false
	}
	return pm.Pix[i : i+n], true
}

// blitTransparent copies non-transparent pixels from frame onto canvas.
func blitTransparent(frame, canvas *image.Paletted) {
	r := frame.Bounds().Intersect(canvas.Bounds())
	width := r.Dx()
	if width <= 0 {
		return
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		src, okS := palettedSpan(frame, r.Min.X, y, width)
		dst, okD := palettedSpan(canvas, r.Min.X, y, width)
		if !okS || !okD {
			for x := r.Min.X; x < r.Max.X; x++ {
				idx := indexAt(frame, x, y)
				if idx != palette.TransparentIndex {
					canvas.SetColorIndex(x, y, idx)
				}
			}
			continue
		}
		for x := range width {
			if src[x] != palette.TransparentIndex {
				dst[x] = src[x]
			}
		}
	}
}

// Clone returns a copy of src's pixels on the same palette.
func Clone(src *image.Paletted) *image.Paletted {
	return &image.Paletted{
		Pix:     bytes.Clone(src.Pix),
		Stride:  src.Stride,
		Rect:    src.Rect,
		Palette: src.Palette,
	}
}

// BlitOpaque copies every pixel of patch onto canvas.
func BlitOpaque(patch, canvas *image.Paletted) {
	r := patch.Rect.Intersect(canvas.Rect)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		copy(canvas.Pix[canvas.PixOffset(r.Min.X, y):], patch.Pix[patch.PixOffset(r.Min.X, y):][:r.Dx()])
	}
}
