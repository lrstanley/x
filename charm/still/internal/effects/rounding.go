// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	icol "github.com/lrstanley/x/charm/still/internal/color"
	"github.com/lrstanley/x/charm/still/units"
)

// ApplyRoundedMask clips the terminal window to a rounded rectangle.
func ApplyRoundedMask(ctx WindowContext, img draw.Image) {
	cfg := ctx.Snapshot()
	radius := cfg.BorderRadius.Int()
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

	outside := icol.NRGBA(ctx.MarginColor())
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

		src := icol.NRGBA(img.At(x, y))
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

func inRoundedRectCross(lx, ly, width, height, radius int) bool {
	return (lx >= radius && lx < width-radius) || (ly >= radius && ly < height-radius)
}

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

func inRoundedRect(width, height, radius, x, y float64) bool {
	cx := units.Clamp(x, radius, width-radius)
	cy := units.Clamp(y, radius, height-radius)
	dx := x - cx
	dy := y - cy
	return dx*dx+dy*dy <= radius*radius
}

func compositeCovered(src, dst color.NRGBA, coverage float64) color.NRGBA {
	coverage = units.Clamp(coverage, 0.0, 1.0)
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
