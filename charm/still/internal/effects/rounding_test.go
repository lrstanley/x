// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects_test

import (
	"image"
	"image/color"
	"testing"

	"github.com/lrstanley/x/charm/still/internal/config"
	"github.com/lrstanley/x/charm/still/internal/effects"
	"github.com/lrstanley/x/charm/still/units"
)

type roundingWindowContext struct {
	cfg    config.Snapshot
	window image.Rectangle
	margin color.Color
}

func (m roundingWindowContext) Snapshot() config.Snapshot { return m.cfg }
func (m roundingWindowContext) WindowBounds() image.Rectangle {
	return m.window
}
func (m roundingWindowContext) MarginColor() color.Color { return m.margin }

func TestApplyRoundedMask(t *testing.T) {
	t.Parallel()

	window := image.Rect(0, 0, 20, 20)
	margin := color.NRGBA{R: 255, A: 255}
	ctx := roundingWindowContext{
		cfg:    config.Snapshot{BorderRadius: units.Px(4)},
		window: window,
		margin: margin,
	}

	img := image.NewNRGBA(window)
	for y := window.Min.Y; y < window.Max.Y; y++ {
		for x := window.Min.X; x < window.Max.X; x++ {
			img.SetNRGBA(x, y, color.NRGBA{G: 128, A: 255})
		}
	}

	effects.ApplyRoundedMask(ctx, img)

	corner := img.NRGBAAt(0, 0)
	if corner.R != 255 || corner.G != 0 {
		t.Fatalf("top-left corner = %+v, want margin red", corner)
	}
	center := img.NRGBAAt(10, 10)
	if center.G != 128 {
		t.Fatalf("center = %+v, want original green", center)
	}
}

func TestApplyRoundedMaskNoOp(t *testing.T) {
	t.Parallel()

	window := image.Rect(0, 0, 10, 10)
	ctx := roundingWindowContext{
		cfg:    config.Snapshot{BorderRadius: 0},
		window: window,
		margin: color.NRGBA{R: 255, A: 255},
	}
	img := image.NewNRGBA(window)
	img.SetNRGBA(0, 0, color.NRGBA{B: 1, A: 255})

	effects.ApplyRoundedMask(ctx, img)
	if got := img.NRGBAAt(0, 0); got.B != 1 {
		t.Fatalf("zero radius should not modify pixel: %+v", got)
	}
}
