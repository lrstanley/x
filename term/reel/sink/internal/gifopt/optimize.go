// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package gifopt optimizes the frames of a GIF: it diffs palettized frames,
// and rewrites a recording of full frames as sub-rect frames with disposal
// methods, as gifsicle -O1/-O2 does. Unchanged pixels of a sub-rect frame
// are [palette.TransparentIndex].
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
	// dirtyRectThreshold is the maximum fraction of canvas area a diff bbox may
	// occupy before keeping a full keyframe instead of a sub-rect frame.
	dirtyRectThreshold = 0.5

	// smallOverlayThreshold is the maximum fraction of canvas area treated as a
	// cursor-sized overlay eligible for DisposalPrevious.
	smallOverlayThreshold = 0.02

	// maxOverlayExtentPx is the maximum sub-rect width or height eligible for
	// DisposalPrevious (cursor blink, caret overlays).
	maxOverlayExtentPx = 64
)

type framePlanKind int

const (
	planFullKeyframe framePlanKind = iota
	planSubRect
	planReveal
)

// framePlan captures per-output-frame disposal metadata decided during planning.
type framePlan struct {
	kind     framePlanKind
	delay    time.Duration
	disposal byte
	image    *image.Paletted // output frame; built during planning
}

// disposalCanvas is the simulated screen after each planned frame's disposal.
// view may alias an output image; owned is set once view is a private copy
// that later frames are allowed to mutate.
type disposalCanvas struct {
	view  *image.Paletted
	owned bool
}

// mutable returns a canvas buffer that is safe to write. The first write
// copies view so an output keyframe that aliases it stays unchanged.
func (c *disposalCanvas) mutable() *image.Paletted {
	if !c.owned {
		c.view = Clone(c.view)
		c.owned = true
	}
	return c.view
}

// Optimize rewrites frames, which are full frames on screen, as gifsicle
// -O1/-O2 style sub-rect frames with disposal methods: frames that are
// smaller offset rects, with transparent unchanged pixels. When deltaAt is
// not negative, the frames after frames[deltaAt] are instead crops of what
// changed since the frame before them, which [BlitOpaque] applies. screen is
// not empty, and there are at least two frames, with a delay for each.
//
// It returns the new frames with their delays and disposal methods. Only
// [gif.DisposalNone] and [gif.DisposalPrevious] are used, so the GIF's
// background color never shows.
func Optimize(
	screen image.Rectangle,
	frames []*image.Paletted,
	delays []time.Duration,
	deltaAt int,
) (outFrames []*image.Paletted, outDelays []time.Duration, disposals []byte) {
	// The first capture is the keyframe. Reuse it when it is already full-screen
	// so a cursor-sized blink does not clone the canvas. Later mutations copy
	// on write.
	first := frameForDiff(frames[0], screen)
	canvas := disposalCanvas{view: first}

	plans, firstDelayExtra := planDisposalFrames(frames, delays, deltaAt, &canvas, screen)
	outFrames, outDelays, disposals = collectDisposalOutput(plans, 1+len(plans))

	outFrames[0] = first
	outDelays[0] = delays[0] + firstDelayExtra
	disposals[0] = gif.DisposalNone
	return outFrames, outDelays, disposals
}

// planDisposalFrames walks input captures: diff and decide each output frame,
// build its image, then apply it to the simulated canvas for subsequent diffs.
// Frames are only diffed by index against the canvas when both use the same
// palette. A palette change diffs by color instead and writes what changed
// color, or a full keyframe when most of the screen did.
func planDisposalFrames(frames []*image.Paletted, delays []time.Duration, deltaAt int, canvas *disposalCanvas, screen image.Rectangle) (plans []framePlan, firstDelayExtra time.Duration) {
	n := len(frames)
	canvasArea := screen.Dx() * screen.Dy()
	plans = make([]framePlan, 0, n-1)

	// Delta recordings rebuild each full frame of the current segment on a
	// rolling clone. Earlier segments are already full frames.
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
		// Indices on two palettes name different colors, so a palette change
		// diffs by color. Pixels outside what changed color already show what
		// full has, so afterwards the screen is full, on its palette.
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
			// An opaque crop: subRectFrame finds the holes by comparing
			// indices, which name different colors on the two palettes.
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
			// DisposalBackground clears the entire sub-rect to BackgroundIndex,
			// including pixels encoded transparent that were showing prior canvas
			// content. Scroll strips and selection rows rely on those pixels, so
			// leave the composited canvas intact instead.
			//
			// A bbox that is mostly new pixels encodes smaller and builds faster
			// as an opaque crop. Punching those pixels transparent fragments
			// runs that would otherwise be one index. Sparse changes (a cursor
			// inside a larger box) still use a transparent hole.
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

// applyPlanToCanvas updates simulated canvas state after an output frame is planned.
// DisposalPrevious restores the pre-frame screen, so the canvas is left as-is.
// Other sub-rects composite onto a copy-on-write canvas.
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

// planIdleMergeOrReveal handles canvas-identical captures after the first
// output frame exists. When the previous planned frame used DisposalPrevious,
// viewers still show that frame until disposal runs; revealFrame triggers
// disposal without changing simulated canvas state. Otherwise the delay
// merges into the prior output frame.
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

// collectDisposalOutput copies planned frames into output slices. Index 0 is
// reserved for the initial full keyframe filled in by Optimize.
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

// overlayEligible reports whether a diff bbox is small enough for
// DisposalPrevious (cursor blink, caret overlays).
func overlayEligible(bbox image.Rectangle, diffArea, canvasArea int) bool {
	if canvasArea <= 0 || diffArea <= 0 {
		return false
	}
	if float64(diffArea)/float64(canvasArea) >= smallOverlayThreshold {
		return false
	}
	return bbox.Dx() <= maxOverlayExtentPx && bbox.Dy() <= maxOverlayExtentPx
}

// frameForDiff returns a full-screen view of frame for diffing against canvas.
// A frame that is already full-screen is returned as-is and must not be mutated.
func frameForDiff(frame *image.Paletted, screen image.Rectangle) *image.Paletted {
	if frame.Bounds().Eq(screen) {
		return frame
	}
	dst := image.NewPaletted(screen, frame.Palette)
	draw.Draw(dst, frame.Bounds().Intersect(screen), frame, frame.Bounds().Min, draw.Src)
	return dst
}

// revealFrame is a 1×1 transparent sub-rect emitted when the next capture
// matches simulated canvas but the previous frame used DisposalPrevious.
// It triggers disposal in viewers without changing state.
func revealFrame(pal color.Palette) *image.Paletted {
	sub := image.NewPaletted(image.Rect(0, 0, 1, 1), pal)
	sub.SetColorIndex(0, 0, palette.TransparentIndex)
	return sub
}

// Diff returns the smallest rectangle enclosing pixels that differ
// between canvas and full, and the count of differing pixels.
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

// diffBoundingBoxRows compares full-canvas frames by row. Identical rows are
// skipped with bytes.Equal. ok is false when the pixel storage cannot cover
// the screen, and the caller falls back to per-pixel indexing.
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

// diffColorBoundingBox is [Diff] for frames on different
// palettes: a pixel differs when the colors its indices name differ.
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

// subRectFrame builds an offset sub-image: changed pixels copy from full,
// unchanged pixels within bbox are marked transparent (-O2). It is on full's
// palette.
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

// Crop copies bbox out of src into a tightly packed frame with no
// transparent holes. Used when almost every pixel in the bbox changed.
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

// palettedSpan returns n palette indices starting at (x, y).
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
