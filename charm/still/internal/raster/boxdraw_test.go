// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package raster_test

import (
	"image"
	"testing"

	"github.com/lrstanley/x/charm/still/internal/raster"
)

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
