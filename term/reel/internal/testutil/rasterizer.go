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
	"github.com/lrstanley/x/term/reel/vt"
)

// Rasterizer is a reel.Rasterizer that draws each cell as one flat color,
// so no fonts load: red is the first byte of the cell's text, green marks
// the cursor, and blue is set while the window is focused.
type Rasterizer struct {
	// Cell is the size of a cell in pixels. Zero is one pixel.
	Cell image.Point
	// Pad is the width in pixels of a grey border around the cell grid.
	Pad int
}

// padColor is the color of the border around the grid.
var padColor = color.NRGBA{R: 0x20, G: 0x20, B: 0x20, A: 0xff}

func (r Rasterizer) cell() image.Point {
	if r.Cell.X <= 0 || r.Cell.Y <= 0 {
		return image.Pt(1, 1)
	}
	return r.Cell
}

// Layout returns the layout of a cols by rows grid of cells of r.Cell,
// inset by r.Pad, at scale 1.
func (r Rasterizer) Layout(cols, rows int) render.Layout {
	cell := r.cell()
	grid := image.Rect(0, 0, cols*cell.X, rows*cell.Y).Add(image.Pt(r.Pad, r.Pad))
	return render.Layout{Image: grid.Inset(-r.Pad), Grid: grid, Cell: cell, Scale: 1}
}

// Draw draws scr with st into dst at dst.Rect.Min.
func (r Rasterizer) Draw(dst *image.NRGBA, scr vt.Screen, st vt.State, _ time.Duration) {
	b := scr.Bounds()
	l := r.Layout(b.Dx(), b.Dy())
	off := dst.Rect.Min
	for y := l.Image.Min.Y; y < l.Image.Max.Y; y++ {
		for x := l.Image.Min.X; x < l.Image.Max.X; x++ {
			dst.SetNRGBA(off.X+x, off.Y+y, padColor)
		}
	}
	for y := range b.Dy() {
		for x := range b.Dx() {
			c := color.NRGBA{A: 0xff}
			if cell := scr.CellAt(b.Min.X+x, b.Min.Y+y); cell != nil && cell.Content != "" {
				c.R = cell.Content[0]
			}
			if st.Cursor.Visible && st.Cursor.X == x && st.Cursor.Y == y {
				c.G = 0x80
			}
			if st.Focused {
				c.B = 0xff
			}
			rect := l.CellRect(x, y).Add(off)
			for py := rect.Min.Y; py < rect.Max.Y; py++ {
				for px := rect.Min.X; px < rect.Max.X; px++ {
					dst.SetNRGBA(px, py, c)
				}
			}
		}
	}
}

// Close does nothing.
func (Rasterizer) Close() error { return nil }

// Text returns the first byte of each cell of an image a zero [Rasterizer]
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
