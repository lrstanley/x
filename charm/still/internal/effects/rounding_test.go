// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects_test

import (
	"image"
	"image/color"
	"testing"

	"github.com/lrstanley/x/charm/still/internal/effects"
	"github.com/lrstanley/x/charm/still/units"
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

func TestApplyRoundedMask(t *testing.T) {
	t.Parallel()

	img := image.NewNRGBA(image.Rect(0, 0, 20, 20))
	for y := range 20 {
		for x := range 20 {
			img.Set(x, y, color.NRGBA{G: 255, A: 255})
		}
	}
	ctx := roundingWindowContext{
		borderRadius: units.Px(4),
		window:       image.Rect(4, 4, 16, 16),
		margin:       color.NRGBA{R: 255, A: 255},
	}
	effects.ApplyRoundedMask(ctx, img)

	if img.NRGBAAt(0, 0).G != 255 {
		t.Fatal("corner margin should use margin color")
	}
	if img.NRGBAAt(10, 10).G != 255 {
		t.Fatal("interior window should remain green")
	}
}

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
