// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects_test

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"math/rand/v2"
	"testing"

	icol "github.com/lrstanley/x/term/reel/render/internal/color"
	"github.com/lrstanley/x/term/reel/render/internal/effects"
	"github.com/lrstanley/x/term/reel/render/units"
)

type roundingWindowContext struct {
	borderRadius units.Px
	window       image.Rectangle
	margin       color.Color
}

func (m roundingWindowContext) WindowBounds() image.Rectangle { return m.window }
func (m roundingWindowContext) MarginColor() color.Color      { return m.margin }
func (m roundingWindowContext) FocusDimming() float64         { return 0 }
func (m roundingWindowContext) BorderRadius() units.Px        { return m.borderRadius }

func TestApplyRoundedMaskDisabled(t *testing.T) {
	t.Parallel()

	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			img.Set(x, y, color.NRGBA{B: 255, A: 255})
		}
	}
	before := img.NRGBAAt(4, 4)
	ctx := roundingWindowContext{
		borderRadius: 0,
		window:       img.Bounds(),
		margin:       color.Black,
	}
	effects.ApplyRoundedMask(ctx, img)
	if img.NRGBAAt(4, 4) != before {
		t.Fatal("zero radius should not modify pixels")
	}
}

// referenceRoundedMask is a copy of the original full-band algorithm, used as
// an oracle for the corner-only implementation.
func referenceRoundedMask(ctx roundingWindowContext, img draw.Image) {
	radius := ctx.BorderRadius().Int()
	window := ctx.WindowBounds()
	if radius <= 0 || window.Empty() {
		return
	}
	width, height := window.Dx(), window.Dy()
	radius = min(radius, min(width, height)/2)
	if radius <= 0 {
		return
	}
	outside := icol.NRGBA(ctx.MarginColor())
	wf, hf, rf := float64(width), float64(height), float64(radius)
	in := func(x, y float64) bool {
		cx := units.Clamp(x, rf, wf-rf)
		cy := units.Clamp(y, rf, hf-rf)
		return (x-cx)*(x-cx)+(y-cy)*(y-cy) <= rf*rf
	}
	for y := window.Min.Y; y < window.Max.Y; y++ {
		for x := window.Min.X; x < window.Max.X; x++ {
			lx, ly := x-window.Min.X, y-window.Min.Y
			px, py := float64(lx)+0.5, float64(ly)+0.5
			if ((lx >= radius && lx < width-radius) || (ly >= radius && ly < height-radius)) && in(px, py) {
				continue
			}
			coverage := 0.0
			if in(px, py) {
				covered := 0
				for sy := range 4 {
					for sx := range 4 {
						if in(float64(lx)+(float64(sx)+0.5)/4, float64(ly)+(float64(sy)+0.5)/4) {
							covered++
						}
					}
				}
				if coverage = float64(covered) / 16; coverage >= 1 {
					continue
				}
			}
			src := icol.NRGBA(img.At(x, y))
			coverage = units.Clamp(coverage, 0, 1)
			srcA := float64(src.A) / 255 * coverage
			dstA := float64(outside.A) / 255
			outA := srcA + dstA*(1-srcA)
			var out color.NRGBA
			if outA > 0 {
				ch := func(s, d uint8) uint8 {
					return uint8(math.Round((float64(s)*srcA + float64(d)*dstA*(1-srcA)) / outA))
				}
				out = color.NRGBA{ch(src.R, outside.R), ch(src.G, outside.G), ch(src.B, outside.B), uint8(math.Round(outA * 255))}
			}
			img.Set(x, y, out)
		}
	}
}

func TestApplyRoundedMaskMatchesReference(t *testing.T) {
	t.Parallel()

	margins := []color.Color{
		color.NRGBA{R: 255, A: 255},
		color.NRGBA{G: 90, B: 200, A: 128},
		color.NRGBA{},
		color.RGBA{R: 10, G: 20, B: 30, A: 255},
	}
	rng := rand.New(rand.NewPCG(1, 2))
	newImg := func(w, h int, nrgba bool) draw.Image {
		var img draw.Image = image.NewRGBA(image.Rect(0, 0, w, h))
		if nrgba {
			img = image.NewNRGBA(image.Rect(0, 0, w, h))
		}
		for y := range h {
			for x := range w {
				img.Set(x, y, color.NRGBA{uint8(rng.UintN(256)), uint8(rng.UintN(256)), uint8(rng.UintN(256)), uint8(rng.UintN(256))})
			}
		}
		return img
	}

	for _, radius := range []int{1, 2, 5, 12, 33, 200} {
		for _, size := range [][2]int{{1, 1}, {3, 40}, {9, 9}, {23, 24}, {25, 7}, {64, 49}, {101, 77}} {
			for i, margin := range margins {
				for _, nrgba := range []bool{true, false} {
					w, h := size[0]+16, size[1]+16
					ctx := roundingWindowContext{
						borderRadius: units.Px(radius),
						window:       image.Rect(8, 8, 8+size[0], 8+size[1]),
						margin:       margin,
					}
					got := newImg(w, h, nrgba)
					want := newImg(w, h, nrgba)
					copy(pixOf(want), pixOf(got))
					effects.ApplyRoundedMask(ctx, got)
					referenceRoundedMask(ctx, want)
					if !bytes.Equal(pixOf(got), pixOf(want)) {
						t.Fatalf("mismatch radius=%d size=%v margin=%d nrgba=%v", radius, size, i, nrgba)
					}
				}
			}
		}
	}
}

func pixOf(img draw.Image) []byte {
	switch m := img.(type) {
	case *image.NRGBA:
		return m.Pix
	case *image.RGBA:
		return m.Pix
	}
	panic(fmt.Sprintf("pixOf: unsupported image %T", img))
}
