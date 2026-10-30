// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package draw_test

import (
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel/render/fonts"
	idraw "github.com/lrstanley/x/term/reel/render/internal/draw"
	"github.com/lrstanley/x/term/reel/render/types"
	"github.com/lrstanley/x/term/reel/render/units"
	"github.com/lrstanley/x/term/reel/vt"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type mockFrameContext struct {
	m    types.Metrics
	grid image.Rectangle
}

func (m mockFrameContext) At() time.Duration { return 0 }
func (m mockFrameContext) CursorBlinkSpeed() time.Duration {
	return 600 * time.Millisecond
}
func (m mockFrameContext) Palette() types.Palette       { return types.Palette{} }
func (m mockFrameContext) State() vt.State              { return vt.State{} }
func (m mockFrameContext) FaintFactor() float64         { return 0.5 }
func (m mockFrameContext) BackgroundOpacity() float64   { return 1 }
func (m mockFrameContext) BackgroundOpacityCells() bool { return false }
func (m mockFrameContext) Metrics() types.Metrics       { return m.m }
func (m mockFrameContext) GridBounds() image.Rectangle  { return m.grid }
func (m mockFrameContext) CellColorsNRGBA(_ *vt.Cell) (fg, bg color.NRGBA) {
	return color.NRGBA{R: 0xff, A: 0xff}, color.NRGBA{A: 0xff}
}
func (m mockFrameContext) ResolvePaletteColor(c color.Color) color.Color { return c }
func (m mockFrameContext) LinkUnderline() bool                           { return false }

func TestFallbackGlyphTarget(t *testing.T) {
	t.Parallel()

	ctx := mockFrameContext{
		m: types.Metrics{
			IconHeight:       units.Px(16),
			IconHeightSingle: units.Px(10),
		},
	}
	area := image.Rect(0, 0, 8, 20)

	single := idraw.FallbackGlyphTarget(ctx, area, &vt.Cell{Width: 1})
	if single.Dy() != 10 {
		t.Fatalf("single width target height = %d, want 10", single.Dy())
	}
	if single.Min.Y != 5 {
		t.Fatalf("single width target Y = %d, want 5", single.Min.Y)
	}

	wide := idraw.FallbackGlyphTarget(ctx, area, &vt.Cell{Width: 2})
	if wide.Dy() != 16 {
		t.Fatalf("wide cell target height = %d, want 16", wide.Dy())
	}

	if got := idraw.FallbackGlyphTarget(ctx, image.Rectangle{}, nil); !got.Empty() {
		t.Fatalf("empty area = %v, want empty", got)
	}
}

func TestPowerlineFallbackScale(t *testing.T) {
	t.Parallel()

	tests := []struct {
		glyph    string
		mode     idraw.ScaleMode
		alignEnd bool
	}{
		{"\ue0a0", idraw.ScaleFitCover1, false},
		{"\ue0b0", idraw.ScaleStretch, false},
		{"\ue0b2", idraw.ScaleStretch, true},
		{"\ue0ce", idraw.ScaleFitCover1, false},
		{"\ue0d4", idraw.ScaleStretch, true},
		{"A", idraw.ScaleDefault, false},
		{"", idraw.ScaleDefault, false},
	}
	for _, tt := range tests {
		mode, alignEnd := idraw.PowerlineFallbackScale(tt.glyph)
		if mode != tt.mode || alignEnd != tt.alignEnd {
			t.Fatalf("PowerlineFallbackScale(%q) = (%v,%v), want (%v,%v)", tt.glyph, mode, alignEnd, tt.mode, tt.alignEnd)
		}
	}
}

func TestFallbackGlyphNeedsScale(t *testing.T) {
	t.Parallel()

	area := image.Rect(0, 0, 10, 10)
	if idraw.FallbackGlyphNeedsScale(image.Rect(2, 2, 8, 8), area) {
		t.Fatal("contained ink should not need scale")
	}
	if !idraw.FallbackGlyphNeedsScale(image.Rect(-1, 0, 5, 5), area) {
		t.Fatal("left overflow should need scale")
	}
	if idraw.FallbackGlyphNeedsScale(image.Rectangle{}, area) {
		t.Fatal("empty dr should not need scale")
	}
}

func TestGlyphRasterBounds(t *testing.T) {
	t.Parallel()

	face := mustTestFace(t)
	dot := fixed.Point26_6{}
	dr, ok := idraw.GlyphRasterBounds(face, dot, "H")
	if !ok || dr.Empty() {
		t.Fatalf("GlyphRasterBounds() = %v, ok=%v", dr, ok)
	}
	if _, emptyOK := idraw.GlyphRasterBounds(face, dot, ""); emptyOK {
		t.Fatal("empty glyph should not produce bounds")
	}
}

func TestGlyphVerticalAdjust(t *testing.T) {
	t.Parallel()

	face := mustTestFace(t)
	area := image.Rect(0, 0, 20, 20)
	dot := fixed.Point26_6{Y: fixed.I(10)}
	adj := idraw.GlyphVerticalAdjust(face, dot, "H", area)
	dr, ok := idraw.GlyphRasterBounds(face, fixed.Point26_6{Y: dot.Y + fixed.I(adj)}, "H")
	if !ok {
		t.Fatal("adjusted glyph should have bounds")
	}
	if dr.Max.Y > area.Max.Y || dr.Min.Y < area.Min.Y {
		t.Fatalf("adjusted bounds %v not within area %v", dr, area)
	}
}

func mustTestFace(t *testing.T) font.Face {
	t.Helper()
	tf, err := fonts.LoadFace("JetBrains Mono", fonts.StyleRegular)
	if err != nil {
		t.Fatalf("load font: %v", err)
	}
	face, err := fonts.NewFace(tf, &opentype.FaceOptions{Size: 11, DPI: 96})
	if err != nil {
		t.Fatalf("new face: %v", err)
	}
	return face
}

func TestFallbackGlyphScaledStretchOverdraw(t *testing.T) {
	t.Parallel()

	tf, err := fonts.LoadFace("Symbols Nerd Font Mono", fonts.StyleRegular)
	if err != nil {
		t.Fatalf("load font: %v", err)
	}

	for _, size := range []struct{ w, h int }{{9, 20}, {18, 40}, {36, 80}, {72, 160}} {
		face, faceErr := fonts.NewFace(tf, &opentype.FaceOptions{Size: float64(size.h) / 2, DPI: 72})
		if faceErr != nil {
			t.Fatalf("new face: %v", faceErr)
		}
		target := image.Rect(size.w*2, size.h*2, size.w*3, size.h*3)
		dst := image.NewNRGBA(image.Rect(0, 0, size.w*5, size.h*5))
		dot := fixed.Point26_6{X: fixed.I(target.Min.X), Y: fixed.I(target.Max.Y - size.h/4)}
		idraw.FallbackGlyphScaled(dst, target, face, color.White, "", dot, idraw.ScaleStretch, false)

		// Padding is 3% horizontally (at least 1px, to hide the glyph's partly
		// transparent edge column) and 0.5% vertically, rounded to whole pixels.
		wantX := max(1, (size.w*3+50)/100)
		wantY := (size.h*5 + 500) / 1000
		ink := image.Rectangle{}
		for y := range dst.Bounds().Dy() {
			for x := range dst.Bounds().Dx() {
				if dst.NRGBAAt(x, y).A != 0 {
					ink = ink.Union(image.Rect(x, y, x+1, y+1))
				}
			}
		}
		if ink.Empty() {
			t.Fatalf("%dx%d: no ink drawn", size.w, size.h)
		}
		if got := target.Min.X - ink.Min.X; got < 0 || got > wantX {
			t.Errorf("%dx%d: left overdraw %d, want 0..%d", size.w, size.h, got, wantX)
		}
		if got := ink.Max.X - target.Max.X; got > wantX {
			t.Errorf("%dx%d: right overdraw %d, want <= %d", size.w, size.h, got, wantX)
		}
		if got := target.Min.Y - ink.Min.Y; got < 0 || got > wantY {
			t.Errorf("%dx%d: top overdraw %d, want 0..%d", size.w, size.h, got, wantY)
		}
		if got := ink.Max.Y - target.Max.Y; got > wantY {
			t.Errorf("%dx%d: bottom overdraw %d, want <= %d", size.w, size.h, got, wantY)
		}
	}
}
