// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"encoding/base64"
	"encoding/binary"
	"image/color"
	"testing"
)

// squareGlyf is a glyf record of one square contour, an em on each side.
func squareGlyf() string {
	b := []byte{0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 3, 0, 0, 1, 1, 1, 1}
	for _, d := range []int16{0, 0, 1000, 0, 0, 1000, 0, -1000} {
		b = binary.BigEndian.AppendUint16(b, uint16(d)) //nolint:gosec // deltas are signed.
	}
	return base64.StdEncoding.EncodeToString(b)
}

// TestDrawRegisteredGlyphs checks that a rune the program registered a
// Glyph Protocol glyph for draws it, in the cell's foreground, across as
// many cells as it asked for, and that a renderer that drew the screen
// before the registration repaints.
func TestDrawRegisteredGlyphs(t *testing.T) {
	t.Parallel()

	red := color.NRGBA{R: 0xff, A: 0xff}
	d := newRenderer(t)
	scr, st := kittyScreen(t, 4, 1,
		"\x1b_25a1;r;cp=e000;reply=0;"+squareGlyf()+"\x1b\\"+
			"\x1b_25a1;r;cp=100000;reply=0;width=2;"+squareGlyf()+"\x1b\\"+
			"\x1b[?25l\x1b[38;2;255;0;0m\ue000 \U00100000 ")
	l := d.Layout(4, 1)

	unregistered := st
	unregistered.Glyphs = nil
	scr.ClearDirty()
	if got := sample(drawAt(t, d, scr, unregistered, 0), l.CellRect(0, 0)); got == red {
		t.Fatal("an unregistered rune drew as the glyph")
	}
	scr.ClearDirty()
	img := drawAt(t, d, scr, st, 0)
	// The two-cell glyph is centered across both its cells.
	for x, want := range []bool{true, false, true, true} {
		if got := sample(img, l.CellRect(x, 0)) == red; got != want {
			t.Errorf("cell %d center drawn = %v, want %v", x, got, want)
		}
	}
}
