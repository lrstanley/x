// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package export

import (
	"bytes"
	"cmp"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"slices"
	"time"
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
		c.view = clonePaletted(c.view)
		c.owned = true
	}
	return c.view
}

// optimizeDisposals rewrites rec in place with gifsicle -O1/-O2 style sub-rect
// frames and disposal methods. Input frames are full-canvas captures; output
// frames may be smaller offset rects with transparent unchanged pixels.
func optimizeDisposals(rec *gifRecording, bg color.Color, bgSet bool) {
	n := len(rec.images)
	if n <= 1 {
		if n == 1 && len(rec.disposal) == 0 {
			rec.disposal = []byte{gif.DisposalNone}
		}
		return
	}

	w, h := rec.config.Width, rec.config.Height
	if w <= 0 || h <= 0 {
		return
	}
	screen := image.Rect(0, 0, w, h)
	pal := paletteFrom(rec.config.ColorModel, rec.images)
	bgIdx := gifBackgroundIndex(pal, rec.images[0], screen, bg, bgSet)

	// The first capture is the keyframe. Reuse it when it is already full-screen
	// so a cursor-sized blink does not clone the canvas. Later mutations copy
	// on write.
	first := frameForDiff(rec.images[0], screen, pal)
	canvas := disposalCanvas{view: first}

	plans, firstDelayExtra := planDisposalFrames(rec, &canvas, screen, pal, w*h)
	images, delays, disposals := collectDisposalOutput(plans, 1+len(plans))

	images[0] = first
	delays[0] = rec.delays[0] + firstDelayExtra
	disposals[0] = gif.DisposalNone

	rec.images = images
	rec.delays = delays
	rec.disposal = disposals
	rec.last, rec.spare = nil, nil
	rec.backgroundIndex = bgIdx
}

// planDisposalFrames walks input captures: diff and decide each output frame,
// build its image, then apply it to the simulated canvas for subsequent diffs.
func planDisposalFrames(rec *gifRecording, canvas *disposalCanvas, screen image.Rectangle, pal color.Palette, canvasArea int) (plans []framePlan, firstDelayExtra time.Duration) {
	n := len(rec.images)
	plans = make([]framePlan, 0, n-1)

	// Delta recordings rebuild each full frame on a rolling clone.
	var cur *image.Paletted
	if rec.last != nil {
		cur = clonePaletted(canvas.view)
	}

	for i := 1; i < n; i++ {
		full := cur
		if cur == nil {
			full = frameForDiff(rec.images[i], screen, pal)
		} else {
			blitOpaque(rec.images[i], cur)
		}
		bbox, diffArea := diffBoundingBox(canvas.view, full, screen)
		if diffArea == 0 {
			if len(plans) == 0 {
				firstDelayExtra += rec.delays[i]
				continue
			}
			planIdleMergeOrReveal(&plans, pal, rec.delays[i])
			continue
		}

		var plan framePlan
		if float64(diffArea)/float64(canvasArea) >= dirtyRectThreshold {
			plan = framePlan{
				kind:     planFullKeyframe,
				delay:    rec.delays[i],
				disposal: gif.DisposalNone,
				image:    clonePaletted(full),
			}
		} else {
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
				patch = cropPaletted(full, bbox)
			} else {
				patch = subRectFrame(canvas.view, full, bbox, pal)
			}
			plan = framePlan{
				kind:     planSubRect,
				delay:    rec.delays[i],
				disposal: disposal,
				image:    patch,
			}
		}

		plans = append(plans, plan)
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
// reserved for the initial full keyframe filled in by optimizeDisposals.
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

// ensureTransparentPaletteSlot marks TransparentPaletteIndex as a zero-alpha
// palette color so GIF encoders emit a transparent color index for sub-rect
// frames. Custom palettes may still use an opaque xterm slot 255.
func ensureTransparentPaletteSlot(out *gif.GIF) {
	pal := paletteFrom(out.Config.ColorModel, out.Image)
	if len(pal) <= int(TransparentPaletteIndex) {
		return
	}
	if _, _, _, a := pal[TransparentPaletteIndex].RGBA(); a == 0 {
		return
	}
	newPal := make(color.Palette, len(pal))
	copy(newPal, pal)
	newPal[TransparentPaletteIndex] = transparentMarkerColor()
	out.Config.ColorModel = newPal
	for _, frame := range out.Image {
		frame.Palette = newPal
	}
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

// gifBackgroundIndex returns the palette index used for DisposalBackground
// clearing. When bgSet, bg is mapped through pal; otherwise the most frequent
// opaque index in the first frame is used (avoids margin/corner pixels).
func gifBackgroundIndex(pal color.Palette, first *image.Paletted, screen image.Rectangle, bg color.Color, bgSet bool) uint8 {
	if bgSet {
		idx := uint8(pal.Index(bg)) //nolint:gosec // palette index is 0-255.
		if idx != TransparentPaletteIndex {
			return idx
		}
	}
	return dominantPaletteIndex(first, screen, pal)
}

// dominantPaletteIndex returns the most frequent non-transparent palette index.
// A count past half the screen is a unique winner and stops the scan.
func dominantPaletteIndex(first *image.Paletted, screen image.Rectangle, pal color.Palette) uint8 {
	full := frameForDiff(first, screen, pal)
	width := screen.Dx()
	height := screen.Dy()
	if width <= 0 || height <= 0 {
		return 0
	}
	half := width * height / 2
	var freq [256]int
	if _, ok := palettedSpan(full, screen.Min.X, screen.Min.Y, width); ok {
		for y := screen.Min.Y; y < screen.Max.Y; y++ {
			span, _ := palettedSpan(full, screen.Min.X, y, width)
			for _, idx := range span {
				if idx == TransparentPaletteIndex {
					continue
				}
				freq[idx]++
				if freq[idx] > half {
					return idx
				}
			}
		}
	} else if idx, ok := dominantPaletteIndexAt(full, screen, &freq, half); ok {
		return idx
	}
	return mostFrequentIndex(freq)
}

// dominantPaletteIndexAt counts pixels that are not stored in a tight row span.
func dominantPaletteIndexAt(full *image.Paletted, screen image.Rectangle, freq *[256]int, half int) (uint8, bool) {
	for y := screen.Min.Y; y < screen.Max.Y; y++ {
		for x := screen.Min.X; x < screen.Max.X; x++ {
			idx := paletteIndexAt(full, x, y)
			if idx == TransparentPaletteIndex {
				continue
			}
			freq[idx]++
			if freq[idx] > half {
				return idx, true
			}
		}
	}
	return 0, false
}

// mostFrequentIndex returns the lowest index with the highest count.
func mostFrequentIndex(freq [256]int) uint8 {
	bestIdx := uint8(0)
	bestCount := 0
	for i, count := range freq {
		if count > bestCount {
			bestCount = count
			bestIdx = uint8(i) //nolint:gosec // i is 0..255.
		}
	}
	return bestIdx
}

func paletteFrom(colorModel color.Model, frames []*image.Paletted) color.Palette {
	if p, ok := colorModel.(color.Palette); ok && p != nil {
		return p
	}
	if len(frames) > 0 && frames[0].Palette != nil {
		return frames[0].Palette
	}
	return defaultGIFPalette()
}

// frameForDiff returns a full-screen view of frame for diffing against canvas.
// A frame that is already full-screen is returned as-is and must not be mutated.
func frameForDiff(frame *image.Paletted, screen image.Rectangle, pal color.Palette) *image.Paletted {
	if frame.Bounds().Eq(screen) && frame.Palette != nil {
		return frame
	}
	dst := image.NewPaletted(screen, pal)
	draw.Draw(dst, frame.Bounds().Intersect(screen), frame, frame.Bounds().Min, draw.Src)
	return dst
}

// revealFrame is a 1×1 transparent sub-rect emitted when the next capture
// matches simulated canvas but the previous frame used DisposalPrevious.
// It triggers disposal in viewers without changing state.
func revealFrame(pal color.Palette) *image.Paletted {
	sub := image.NewPaletted(image.Rect(0, 0, 1, 1), pal)
	sub.SetColorIndex(0, 0, TransparentPaletteIndex)
	return sub
}

// diffBoundingBox returns the smallest rectangle enclosing pixels that differ
// between canvas and full, and the count of differing pixels.
func diffBoundingBox(canvas, full *image.Paletted, screen image.Rectangle) (bbox image.Rectangle, diff int) {
	if canvas != nil && full != nil && canvas.Rect.Eq(screen) && full.Rect.Eq(screen) {
		if fast, n, ok := diffBoundingBoxRows(canvas, full, screen); ok {
			return fast, n
		}
	}
	minX, minY := screen.Max.X, screen.Max.Y
	maxX, maxY := screen.Min.X-1, screen.Min.Y-1
	diff = 0
	for y := screen.Min.Y; y < screen.Max.Y; y++ {
		for x := screen.Min.X; x < screen.Max.X; x++ {
			if paletteIndexAt(canvas, x, y) == paletteIndexAt(full, x, y) {
				continue
			}
			diff++
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x > maxX {
				maxX = x
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	if diff == 0 {
		return image.Rectangle{}, 0
	}
	return image.Rect(minX, minY, maxX+1, maxY+1), diff
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

func paletteIndexAt(pm *image.Paletted, x, y int) uint8 {
	if !image.Pt(x, y).In(pm.Bounds()) {
		return TransparentPaletteIndex
	}
	return pm.Pix[(y-pm.Rect.Min.Y)*pm.Stride+(x-pm.Rect.Min.X)]
}

// subRectFrame builds an offset sub-image: changed pixels copy from full,
// unchanged pixels within bbox are marked transparent (-O2).
func subRectFrame(canvas, full *image.Paletted, bbox image.Rectangle, pal color.Palette) *image.Paletted {
	sub := image.NewPaletted(bbox, pal)
	width := bbox.Dx()
	for y := bbox.Min.Y; y < bbox.Max.Y; y++ {
		dst, okD := palettedSpan(sub, bbox.Min.X, y, width)
		cSpan, okC := palettedSpan(canvas, bbox.Min.X, y, width)
		fSpan, okF := palettedSpan(full, bbox.Min.X, y, width)
		if !okD || !okC || !okF {
			for x := bbox.Min.X; x < bbox.Max.X; x++ {
				if paletteIndexAt(canvas, x, y) == paletteIndexAt(full, x, y) {
					sub.SetColorIndex(x, y, TransparentPaletteIndex)
				} else {
					sub.SetColorIndex(x, y, paletteIndexAt(full, x, y))
				}
			}
			continue
		}
		for x := range width {
			if cSpan[x] == fSpan[x] {
				dst[x] = TransparentPaletteIndex
			} else {
				dst[x] = fSpan[x]
			}
		}
	}
	return sub
}

// cropPaletted copies bbox out of src into a tightly packed frame with no
// transparent holes. Used when almost every pixel in the bbox changed.
func cropPaletted(src *image.Paletted, bbox image.Rectangle) *image.Paletted {
	dst := image.NewPaletted(bbox, src.Palette)
	width := bbox.Dx()
	for y := bbox.Min.Y; y < bbox.Max.Y; y++ {
		out := dst.Pix[(y-bbox.Min.Y)*dst.Stride : (y-bbox.Min.Y)*dst.Stride+width]
		span, ok := palettedSpan(src, bbox.Min.X, y, width)
		if !ok {
			for x := bbox.Min.X; x < bbox.Max.X; x++ {
				dst.SetColorIndex(x, y, paletteIndexAt(src, x, y))
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
				idx := paletteIndexAt(frame, x, y)
				if idx != TransparentPaletteIndex {
					canvas.SetColorIndex(x, y, idx)
				}
			}
			continue
		}
		for x := range width {
			if src[x] != TransparentPaletteIndex {
				dst[x] = src[x]
			}
		}
	}
}

// compactPalette compacts oldPal to only entries referenced across frames,
// reordering by descending frequency so low indices dominate (gifsicle-style
// colormap canonicalization). Transparent pixels map to a dedicated slot
// marked with zero alpha in the palette for replay detection. Frames are
// updated in place with remapped indices and the new palette.
func compactPalette(frames []*image.Paletted, oldPal color.Palette) color.Palette {
	var freq [256]int
	for _, frame := range frames {
		for _, idx := range frame.Pix {
			freq[idx]++
		}
	}

	usesTransparent := freq[TransparentPaletteIndex] > 0
	freq[TransparentPaletteIndex] = 0

	var used []uint8
	for idx, count := range freq {
		if count > 0 {
			used = append(used, uint8(idx)) //nolint:gosec // idx is 0..255.
		}
	}
	slices.SortFunc(used, func(a, b uint8) int {
		return cmp.Or(cmp.Compare(freq[b], freq[a]), cmp.Compare(a, b))
	})

	var remap [256]uint8
	newPal := make(color.Palette, 0, len(used)+1)
	for i, idx := range used {
		newPal = append(newPal, oldPal[idx])
		remap[idx] = uint8(i) //nolint:gosec // i is bounded by palette size.
	}
	if usesTransparent {
		remap[TransparentPaletteIndex] = uint8(len(newPal)) //nolint:gosec // bounded by 256 colors.
		newPal = append(newPal, transparentMarkerColor())
	}

	for _, frame := range frames {
		frame.Palette = newPal
		for i, idx := range frame.Pix {
			frame.Pix[i] = remap[idx]
		}
	}
	return newPal
}

// optimizePalette compacts out to only palette entries referenced across frames.
func optimizePalette(out *gif.GIF) {
	if len(out.Image) == 0 {
		return
	}
	out.Config.ColorModel = compactPalette(out.Image, paletteFrom(out.Config.ColorModel, out.Image))
}

// transparentMarkerColor is stored in compact palettes to mark the transparent
// slot; ReplayGIF detects it via zero alpha.
func transparentMarkerColor() color.Color {
	return color.RGBA{A: 0}
}
