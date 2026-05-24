// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image"
	"image/color"
	"image/draw"
	"math"
)

// applyFocusDimming darkens the terminal window when the caller marked the
// emulator unfocused ([EmulatorState.Focused] == false) and [frameConfig.focusDimming]
// is positive. It blends a black semitransparent layer over [Context.WindowBounds]
// using [overlay] ([draw.Over]), so existing pixel color remains underneath.
//
// Callers gate this from [renderLocked] with emulator state; zero dimming is a
// no-op. The outside margin is excluded because [Context.WindowBounds] is already
// inset by the margin.
//
// Ghostty dims inactive split panes with configurable opacity/fill rather than
// this exact overlay.
func applyFocusDimming(ctx Context, img draw.Image) {
	if ctx.cfg.focusDimming <= 0 {
		return
	}
	overlay(img, ctx.WindowBounds(), color.NRGBA{A: uint8(math.Round(255 * ctx.cfg.focusDimming))})
}

// applyRoundedMask clips the terminal window to a rounded rectangle with
// antialiased edges. Only the perimeter band (width [frameConfig.borderRadius]) is
// scanned; interior pixels are skipped. Within that band, pixels fully inside
// the rounded shape skip [roundedCoverage]; boundary pixels use subpixel sampling
// and [compositeCovered] for antialiasing.
//
// [frameConfig.borderRadius] is clamped to half the smaller window dimension so
// corners remain valid. Radius 0 or an empty window skips the pass.
func applyRoundedMask(ctx Context, img draw.Image) {
	radius := ctx.cfg.borderRadius.Int()
	window := ctx.WindowBounds()
	if radius <= 0 || window.Empty() {
		return
	}

	width := window.Dx()
	height := window.Dy()
	radius = min(radius, min(width, height)/2)
	if radius <= 0 {
		return
	}

	outside := nrgba(ctx.MarginColor())
	innerL := window.Min.X + radius
	innerR := window.Max.X - radius
	innerT := window.Min.Y + radius
	innerB := window.Max.Y - radius

	wf := float64(width)
	hf := float64(height)
	rf := float64(radius)

	apply := func(x, y int) {
		lx := x - window.Min.X
		ly := y - window.Min.Y
		px := float64(lx) + 0.5
		py := float64(ly) + 0.5

		if inRoundedRectCross(lx, ly, width, height, radius) &&
			inRoundedRect(wf, hf, rf, px, py) {
			return
		}

		coverage := 0.0
		if inRoundedRect(wf, hf, rf, px, py) {
			coverage = roundedCoverage(window, radius, x, y)
			if coverage >= 1 {
				return
			}
		}

		src := nrgba(img.At(x, y))
		img.Set(x, y, compositeCovered(src, outside, coverage))
	}

	for y := window.Min.Y; y < innerT; y++ {
		for x := window.Min.X; x < window.Max.X; x++ {
			apply(x, y)
		}
	}
	for y := innerB; y < window.Max.Y; y++ {
		for x := window.Min.X; x < window.Max.X; x++ {
			apply(x, y)
		}
	}
	for y := innerT; y < innerB; y++ {
		for x := window.Min.X; x < innerL; x++ {
			apply(x, y)
		}
		for x := innerR; x < window.Max.X; x++ {
			apply(x, y)
		}
	}
}

// inRoundedRectCross reports whether local pixel (lx, ly) lies in the axis-aligned
// cross inset by radius from the window edges. Points in this region are fully
// inside the rounded rectangle and do not need subpixel coverage sampling.
func inRoundedRectCross(lx, ly, width, height, radius int) bool {
	return (lx >= radius && lx < width-radius) || (ly >= radius && ly < height-radius)
}

// roundedCoverage returns the fraction of a 4x4 subpixel grid, centered on
// (x, y), that lies inside the rounded rectangle defined by rect and radius.
// Values are in [0, 1] and feed [compositeCovered] for edge antialiasing.
func roundedCoverage(rect image.Rectangle, radius, x, y int) float64 {
	const samples = 4

	var covered int
	for sy := range samples {
		for sx := range samples {
			px := float64(x-rect.Min.X) + (float64(sx)+0.5)/samples
			py := float64(y-rect.Min.Y) + (float64(sy)+0.5)/samples
			if inRoundedRect(float64(rect.Dx()), float64(rect.Dy()), float64(radius), px, py) {
				covered++
			}
		}
	}
	return float64(covered) / (samples * samples)
}

// inRoundedRect reports whether point (x, y) lies inside an axis-aligned
// rectangle of size width x height with circular corners of the given radius
// (a rounded rect: union of the center "cross" and four quarter-circles).
func inRoundedRect(width, height, radius, x, y float64) bool {
	cx := clamp(x, radius, width-radius)
	cy := clamp(y, radius, height-radius)
	dx := x - cx
	dy := y - cy
	return dx*dx+dy*dy <= radius*radius
}

// compositeCovered alpha-composites src (interior pixel) with dst (margin color)
// using coverage as the weight of src: out = src*coverage + dst*(1-coverage) in
// premultiplied form, then unpremultiplies for [color.NRGBA].
//
// TODO: consider optional linear-corrected blending if renders need to match
// renderer gamma behavior more closely.
func compositeCovered(src, dst color.NRGBA, coverage float64) color.NRGBA {
	coverage = clamp(coverage, 0.0, 1.0)
	srcA := float64(src.A) / 255 * coverage
	dstA := float64(dst.A) / 255
	outA := srcA + dstA*(1-srcA)
	if outA <= 0 {
		return color.NRGBA{}
	}
	r := (float64(src.R)*srcA + float64(dst.R)*dstA*(1-srcA)) / outA
	g := (float64(src.G)*srcA + float64(dst.G)*dstA*(1-srcA)) / outA
	b := (float64(src.B)*srcA + float64(dst.B)*dstA*(1-srcA)) / outA
	return color.NRGBA{
		R: uint8(math.Round(r)),
		G: uint8(math.Round(g)),
		B: uint8(math.Round(b)),
		A: uint8(math.Round(outA * 255)),
	}
}
