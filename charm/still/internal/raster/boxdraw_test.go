// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package raster_test

import (
	"image"
	"testing"

	"github.com/lrstanley/x/charm/still/internal/raster"
)

func TestExtendBoxInkToEdgesVertical(t *testing.T) {
	t.Parallel()

	area := image.Rect(0, 0, 10, 20)
	ink := image.Rect(4, 5, 6, 15)
	got := raster.ExtendBoxInkToEdges(area, ink, raster.BoxStrokeVertical, 0, area)
	want := image.Rect(4, 0, 6, 20)
	if got != want {
		t.Fatalf("extended ink = %v, want %v", got, want)
	}
}

func TestExtendBoxInkToEdgesVerticalOverlapTop(t *testing.T) {
	t.Parallel()

	area := image.Rect(0, 10, 10, 30)
	ink := image.Rect(4, 15, 6, 25)
	got := raster.ExtendBoxInkToEdges(area, ink, raster.BoxStrokeVertical, 0, area)
	want := image.Rect(4, 9, 6, 30)
	if got != want {
		t.Fatalf("extended ink = %v, want %v", got, want)
	}
}

func TestExtendBoxInkToEdgesHorizontal(t *testing.T) {
	t.Parallel()

	area := image.Rect(0, 0, 20, 10)
	ink := image.Rect(5, 4, 15, 6)
	got := raster.ExtendBoxInkToEdges(area, ink, raster.BoxStrokeHorizontal, 0, area)
	want := image.Rect(0, 4, 20, 6)
	if got != want {
		t.Fatalf("extended ink = %v, want %v", got, want)
	}
}

func TestExtendBoxInkToEdgesBothFromInk(t *testing.T) {
	t.Parallel()

	// BoxStrokeBoth with no junction rune derives direction from ink centroid.
	area := image.Rect(0, 0, 20, 20)
	ink := image.Rect(0, 0, 4, 4)
	got := raster.ExtendBoxInkToEdges(area, ink, raster.BoxStrokeBoth, 0, area)
	want := image.Rect(0, 0, 20, 20)
	if got != want {
		t.Fatalf("extended ink = %v, want %v", got, want)
	}
}

func TestIsBoxJunctionRune(t *testing.T) {
	t.Parallel()

	if !raster.IsBoxJunctionRune('╭') || !raster.IsBoxJunctionRune('┼') {
		t.Fatal("corners and crosses should be junction runes")
	}
	if raster.IsBoxJunctionRune('│') || raster.IsBoxJunctionRune('─') {
		t.Fatal("straight strokes should not be junction runes")
	}
}

func TestBoxStrokeExtentsFromRune(t *testing.T) {
	t.Parallel()

	ext, ok := raster.BoxStrokeExtentsFromRune('╭')
	if !ok || !ext.Right || !ext.Down || ext.Left || ext.Up {
		t.Fatalf("╭ extents = %+v ok=%v, want right+down only", ext, ok)
	}
	ext, ok = raster.BoxStrokeExtentsFromRune('│')
	if ok {
		t.Fatalf("│ should not define junction extents, got %+v", ext)
	}
}

func TestBoxGlyphDilateRadius(t *testing.T) {
	t.Parallel()

	ink := image.Rect(0, 0, 20, 1)
	if rx, ry := raster.BoxGlyphDilateRadius(raster.BoxStrokeHorizontal, ink, 4); rx != 0 || ry != 2 {
		t.Fatalf("horizontal radius = (%d,%d), want (0,2)", rx, ry)
	}

	ink = image.Rect(0, 0, 1, 20)
	if rx, ry := raster.BoxGlyphDilateRadius(raster.BoxStrokeVertical, ink, 4); rx != 2 || ry != 0 {
		t.Fatalf("vertical radius = (%d,%d), want (2,0)", rx, ry)
	}

	ink = image.Rect(0, 0, 2, 2)
	if rx, ry := raster.BoxGlyphDilateRadius(raster.BoxStrokeBoth, ink, 4); rx != 1 || ry != 1 {
		t.Fatalf("both radius = (%d,%d), want (1,1)", rx, ry)
	}
}
