// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package testutil holds the helpers the tests of reel and its effects
// share. Only _test.go files import it.
package testutil

import (
	"image"
	"image/color"
	"strings"
	"time"

	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/reel/vt/screen"
)

// Rasterizer is a reel.Rasterizer that draws each cell as one pixel, so no
// fonts load: red is the first byte of the cell's text, green marks the
// cursor, and blue is set while the window is focused.
type Rasterizer struct{}

// Layout returns a layout of one pixel per cell, at scale 1.
func (Rasterizer) Layout(cols, rows int) render.Layout {
	r := image.Rect(0, 0, cols, rows)
	return render.Layout{Image: r, Grid: r, Cell: image.Pt(1, 1), Scale: 1}
}

// Draw draws scr with st into dst at dst.Rect.Min.
func (Rasterizer) Draw(dst *image.NRGBA, scr screen.Screen, st screen.State, _ time.Duration) {
	b := scr.Bounds()
	for y := range b.Dy() {
		for x := range b.Dx() {
			c := color.NRGBA{A: 0xff}
			if cell := scr.CellAt(x, y); cell != nil && cell.Content != "" {
				c.R = cell.Content[0]
			}
			if st.Cursor.Visible && st.Cursor.X == x && st.Cursor.Y == y {
				c.G = 0x80
			}
			if st.Focused {
				c.B = 0xff
			}
			dst.SetNRGBA(dst.Rect.Min.X+x, dst.Rect.Min.Y+y, c)
		}
	}
}

// Close does nothing.
func (Rasterizer) Close() error { return nil }

// Text returns the first byte of each cell of an image the [Rasterizer]
// drew, row after row, with spaces for empty cells and trailing spaces cut.
func Text(img *image.NRGBA) string {
	var b strings.Builder
	for i := 0; i < len(img.Pix); i += 4 {
		if c := img.Pix[i]; c != 0 {
			b.WriteByte(c)
		} else {
			b.WriteByte(' ')
		}
	}
	return strings.TrimRight(b.String(), " ")
}
