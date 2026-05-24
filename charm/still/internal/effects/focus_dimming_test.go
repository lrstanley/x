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
)

type focusWindowContext struct {
	cfg    config.Snapshot
	window image.Rectangle
}

func (m focusWindowContext) Snapshot() config.Snapshot { return m.cfg }
func (m focusWindowContext) WindowBounds() image.Rectangle {
	return m.window
}
func (m focusWindowContext) MarginColor() color.Color { return color.Black }

func TestApplyFocusDimming(t *testing.T) {
	t.Parallel()

	window := image.Rect(0, 0, 4, 4)
	ctx := focusWindowContext{
		cfg:    config.Snapshot{FocusDimming: 0.5},
		window: window,
	}
	img := image.NewNRGBA(window)
	for y := window.Min.Y; y < window.Max.Y; y++ {
		for x := window.Min.X; x < window.Max.X; x++ {
			img.SetNRGBA(x, y, color.NRGBA{G: 200, A: 255})
		}
	}

	effects.ApplyFocusDimming(ctx, img)
	if got := img.NRGBAAt(2, 2); got.G >= 200 {
		t.Fatalf("dimmed pixel G = %d, want less than 200", got.G)
	}
}

func TestApplyFocusDimmingDisabled(t *testing.T) {
	t.Parallel()

	window := image.Rect(0, 0, 2, 2)
	ctx := focusWindowContext{
		cfg:    config.Snapshot{FocusDimming: 0},
		window: window,
	}
	img := image.NewNRGBA(window)
	img.SetNRGBA(0, 0, color.NRGBA{R: 100, A: 255})

	effects.ApplyFocusDimming(ctx, img)
	if got := img.NRGBAAt(0, 0); got.R != 100 {
		t.Fatalf("zero dimming should not change pixel: %+v", got)
	}
}
