// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package draw_test

import (
	"image"
	"image/color"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	idraw "github.com/lrstanley/x/charm/still/internal/draw"
	"github.com/lrstanley/x/charm/still/types"
	"github.com/lrstanley/x/charm/still/units"
)

type decorationFrameContext struct {
	m    types.Metrics
	grid image.Rectangle
}

func (m decorationFrameContext) Now() time.Time { return time.Unix(0, 0) }
func (m decorationFrameContext) CursorBlinkSpeed() time.Duration {
	return 600 * time.Millisecond
}
func (m decorationFrameContext) Palette() types.Palette { return types.Palette{} }
func (m decorationFrameContext) EmulatorState() (types.EmulatorState, bool) {
	return types.EmulatorState{}, false
}
func (m decorationFrameContext) FaintFactor() float64         { return 0.5 }
func (m decorationFrameContext) BackgroundOpacity() float64   { return 1 }
func (m decorationFrameContext) BackgroundOpacityCells() bool { return false }
func (m decorationFrameContext) Metrics() types.Metrics       { return m.m }
func (m decorationFrameContext) GridBounds() image.Rectangle  { return m.grid }
func (m decorationFrameContext) CellColorsNRGBA(_ *uv.Cell) (fg, bg color.NRGBA) {
	return color.NRGBA{R: 0xff, A: 0xff}, color.NRGBA{A: 0xff}
}
func (m decorationFrameContext) ResolvePaletteColor(c color.Color) color.Color { return c }

func TestDecorationsUnderline(t *testing.T) {
	t.Parallel()

	ctx := decorationFrameContext{
		m: types.Metrics{
			UnderlinePosition:  units.Px(18),
			UnderlineThickness: units.Px(1),
		},
		grid: image.Rect(0, 0, 100, 100),
	}
	img := image.NewNRGBA(image.Rect(0, 0, 10, 20))
	area := image.Rect(0, 0, 10, 20)
	cell := &uv.Cell{Style: uv.Style{Underline: uv.UnderlineSingle}}

	idraw.Decorations(ctx, img, area, cell, color.NRGBA{R: 255, A: 255})

	found := false
	for x := range 10 {
		if img.NRGBAAt(x, 18).R == 255 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("single underline did not paint expected row")
	}
}
