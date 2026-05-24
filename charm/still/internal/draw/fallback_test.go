// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package draw_test

import (
	"image"
	"image/color"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/charm/still/fonts"
	"github.com/lrstanley/x/charm/still/internal/config"
	idraw "github.com/lrstanley/x/charm/still/internal/draw"
	"github.com/lrstanley/x/charm/still/types"
	"github.com/lrstanley/x/charm/still/units"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type mockFrameContext struct {
	cfg  config.Snapshot
	m    types.Metrics
	grid image.Rectangle
}

func (m mockFrameContext) Snapshot() config.Snapshot { return m.cfg }
func (m mockFrameContext) Metrics() types.Metrics    { return m.m }
func (m mockFrameContext) GridBounds() image.Rectangle {
	return m.grid
}
func (m mockFrameContext) CellColors(cell *uv.Cell) (color.Color, color.Color) {
	return color.White, color.Black
}

func TestFallbackGlyphTarget(t *testing.T) {
	t.Parallel()

	ctx := mockFrameContext{
		m: types.Metrics{
			IconHeight:       units.Px(16),
			IconHeightSingle: units.Px(10),
		},
	}
	area := image.Rect(0, 0, 8, 20)

	single := idraw.FallbackGlyphTarget(ctx, area, &uv.Cell{Width: 1})
	if single.Dy() != 10 {
		t.Fatalf("single width target height = %d, want 10", single.Dy())
	}
	if single.Min.Y != 5 {
		t.Fatalf("single width target Y = %d, want 5", single.Min.Y)
	}

	wide := idraw.FallbackGlyphTarget(ctx, area, &uv.Cell{Width: 2})
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
	if _, ok := idraw.GlyphRasterBounds(face, dot, ""); ok {
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
	tf, err := fonts.Load("JetBrainsMono-Regular")
	if err != nil {
		t.Fatalf("load font: %v", err)
	}
	face, err := fonts.NewFace(tf, &opentype.FaceOptions{Size: 11, DPI: 96})
	if err != nil {
		t.Fatalf("new face: %v", err)
	}
	return face
}
