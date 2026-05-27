// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package export

import (
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"sort"
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

	canvas := image.NewPaletted(screen, pal)
	first := normalizeFullFrame(rec.images[0], screen, pal)
	blitOpaque(canvas, first, screen)

	plans, firstDelayExtra := planDisposalFrames(rec, canvas, beforeBuffer(screen, pal), screen, pal, w*h, bgIdx)
	images, delays, disposals := collectDisposalOutput(plans, 1+len(plans))

	images[0] = first
	delays[0] = rec.delays[0] + firstDelayExtra
	disposals[0] = gif.DisposalNone

	rec.images = images
	rec.delays = delays
	rec.disposal = disposals
	rec.backgroundIndex = bgIdx
}

func beforeBuffer(screen image.Rectangle, pal color.Palette) *image.Paletted {
	return image.NewPaletted(screen, pal)
}

// planDisposalFrames walks input captures: diff and decide each output frame,
// build its image, then apply it to the simulated canvas for subsequent diffs.
func planDisposalFrames(rec *gifRecording, canvas, before *image.Paletted, screen image.Rectangle, pal color.Palette, canvasArea int, bgIdx uint8) (plans []framePlan, firstDelayExtra time.Duration) {
	n := len(rec.images)
	plans = make([]framePlan, 0, n-1)

	for i := 1; i < n; i++ {
		full := frameForDiff(rec.images[i], screen, pal)
		bbox, diffArea := diffBoundingBox(canvas, full, screen)
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
			plan = framePlan{
				kind:     planSubRect,
				delay:    rec.delays[i],
				disposal: disposal,
				image:    subRectFrame(canvas, full, bbox, pal),
			}
		}

		plans = append(plans, plan)
		applyPlanToCanvas(canvas, before, plan, full, screen, bgIdx)
	}

	return plans, firstDelayExtra
}

// applyPlanToCanvas updates simulated canvas state after an output frame is planned.
func applyPlanToCanvas(canvas, before *image.Paletted, plan framePlan, full *image.Paletted, screen image.Rectangle, bgIdx uint8) {
	switch plan.kind {
	case planFullKeyframe:
		blitOpaque(canvas, full, screen)
	case planSubRect:
		applyFrame(canvas, before, plan.image, plan.disposal, bgIdx)
	case planReveal:
	}
}

// planIdleMergeOrReveal handles canvas-identical captures after the first
// output frame exists. When the previous planned frame used DisposalPrevious
// or DisposalBackground, viewers still show that frame until disposal runs;
// revealFrame triggers disposal without changing simulated canvas state.
// Otherwise the delay merges into the prior output frame.
func planIdleMergeOrReveal(plans *[]framePlan, pal color.Palette, delay time.Duration) {
	switch (*plans)[len(*plans)-1].disposal {
	case gif.DisposalPrevious, gif.DisposalBackground:
		*plans = append(*plans, framePlan{
			kind:     planReveal,
			delay:    delay,
			disposal: gif.DisposalNone,
			image:    revealFrame(pal),
		})
		return
	}
	last := len(*plans) - 1
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
func dominantPaletteIndex(first *image.Paletted, screen image.Rectangle, pal color.Palette) uint8 {
	full := normalizeFullFrame(first, screen, pal)
	freq := make(map[uint8]int)
	for y := screen.Min.Y; y < screen.Max.Y; y++ {
		for x := screen.Min.X; x < screen.Max.X; x++ {
			idx := paletteIndexAt(full, x, y)
			if idx == TransparentPaletteIndex {
				continue
			}
			freq[idx]++
		}
	}
	var bestIdx uint8
	bestCount := -1
	for idx, count := range freq {
		if count > bestCount {
			bestCount = count
			bestIdx = idx
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
// The result is read-only; callers must clone before storing in output GIFs.
func frameForDiff(frame *image.Paletted, screen image.Rectangle, pal color.Palette) *image.Paletted {
	if frame.Bounds().Eq(screen) && frame.Palette != nil {
		return frame
	}
	dst := image.NewPaletted(screen, pal)
	draw.Draw(dst, frame.Bounds().Intersect(screen), frame, frame.Bounds().Min, draw.Src)
	return dst
}

// normalizeFullFrame clones frame onto a full-screen paletted buffer for output.
func normalizeFullFrame(frame *image.Paletted, screen image.Rectangle, pal color.Palette) *image.Paletted {
	if frame.Bounds().Eq(screen) && frame.Palette != nil {
		return clonePaletted(frame)
	}
	return frameForDiff(frame, screen, pal)
}

// revealFrame is a 1×1 transparent sub-rect emitted when the next capture
// matches simulated canvas but the previous frame used DisposalPrevious or
// DisposalBackground. It triggers disposal in viewers without changing state.
func revealFrame(pal color.Palette) *image.Paletted {
	sub := image.NewPaletted(image.Rect(0, 0, 1, 1), pal)
	sub.SetColorIndex(0, 0, TransparentPaletteIndex)
	return sub
}

// diffBoundingBox returns the smallest rectangle enclosing pixels that differ
// between canvas and full, and the count of differing pixels.
func diffBoundingBox(canvas, full *image.Paletted, screen image.Rectangle) (bbox image.Rectangle, diff int) {
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
	for y := bbox.Min.Y; y < bbox.Max.Y; y++ {
		for x := bbox.Min.X; x < bbox.Max.X; x++ {
			if paletteIndexAt(canvas, x, y) == paletteIndexAt(full, x, y) {
				sub.SetColorIndex(x, y, TransparentPaletteIndex)
			} else {
				sub.SetColorIndex(x, y, paletteIndexAt(full, x, y))
			}
		}
	}
	return sub
}

// applyFrame composites frame onto canvas, then applies disposal to prepare
// canvas for the next frame.
func applyFrame(canvas, before, frame *image.Paletted, disposal byte, bgIdx uint8) {
	copyPaletted(before, canvas)
	blitTransparent(frame, canvas)
	switch disposal {
	case gif.DisposalPrevious:
		copyPaletted(canvas, before)
	case gif.DisposalBackground:
		clearRect(canvas, frame.Bounds(), bgIdx)
	case gif.DisposalNone:
	}
}

// blitOpaque copies all pixels from frame onto canvas within clip.
func blitOpaque(canvas, frame *image.Paletted, clip image.Rectangle) {
	r := frame.Bounds().Intersect(clip)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			canvas.SetColorIndex(x, y, paletteIndexAt(frame, x, y))
		}
	}
}

// blitTransparent copies non-transparent pixels from frame onto canvas.
func blitTransparent(frame, canvas *image.Paletted) {
	r := frame.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			idx := paletteIndexAt(frame, x, y)
			if idx != TransparentPaletteIndex {
				canvas.SetColorIndex(x, y, idx)
			}
		}
	}
}

func clearRect(pm *image.Paletted, r image.Rectangle, bgIdx uint8) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if image.Pt(x, y).In(pm.Bounds()) {
				pm.SetColorIndex(x, y, bgIdx)
			}
		}
	}
}

func copyPaletted(dst, src *image.Paletted) {
	if dst.Rect.Eq(src.Rect) && dst.Stride == src.Stride && len(dst.Pix) == len(src.Pix) {
		copy(dst.Pix, src.Pix)
		return
	}
	draw.Draw(dst, src.Bounds(), src, src.Bounds().Min, draw.Src)
}

// compactPalette compacts oldPal to only entries referenced across frames,
// reordering by descending frequency so low indices dominate (gifsicle-style
// colormap canonicalization). Transparent pixels map to a dedicated slot
// marked with zero alpha in the palette for replay detection. Frames are
// updated in place with remapped indices and the new palette.
func compactPalette(frames []*image.Paletted, oldPal color.Palette) color.Palette {
	freq := make(map[uint8]int)
	usesTransparent := false

	for _, frame := range frames {
		for _, idx := range frame.Pix {
			if idx == TransparentPaletteIndex {
				usesTransparent = true
				continue
			}
			freq[idx]++
		}
	}

	type colorFreq struct {
		idx   uint8
		count int
	}
	used := make([]colorFreq, 0, len(freq))
	for idx, count := range freq {
		used = append(used, colorFreq{idx: idx, count: count})
	}
	sort.Slice(used, func(i, j int) bool {
		if used[i].count != used[j].count {
			return used[i].count > used[j].count
		}
		return used[i].idx < used[j].idx
	})

	newPal := make(color.Palette, 0, len(used)+1)
	remap := make(map[uint8]uint8, len(used)+1)

	for i, cf := range used {
		newPal = append(newPal, oldPal[cf.idx])
		remap[cf.idx] = uint8(i) //nolint:gosec // i is bounded by palette size.
	}

	newTransparentIdx := uint8(0)
	if usesTransparent {
		newTransparentIdx = uint8(len(newPal)) //nolint:gosec // bounded by 256 colors.
		newPal = append(newPal, transparentMarkerColor())
		remap[TransparentPaletteIndex] = newTransparentIdx
	}

	for _, frame := range frames {
		frame.Palette = newPal
		for i, idx := range frame.Pix {
			if idx == TransparentPaletteIndex {
				frame.Pix[i] = newTransparentIdx
				continue
			}
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
