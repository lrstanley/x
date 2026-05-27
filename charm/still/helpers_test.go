// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image"
	"image/color"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func assertPanic(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatalf("%s did not panic", name)
		}
	}()
	fn()
}

type testScreen struct {
	*uv.Buffer
}

func newTestScreen(width, height int) testScreen {
	return testScreen{Buffer: uv.NewBuffer(width, height)}
}

func (s testScreen) WidthMethod() uv.WidthMethod {
	return testWidthMethod{}
}

type testWidthMethod struct{}

func (testWidthMethod) StringWidth(str string) int {
	if str == "" {
		return 0
	}
	return 1
}

func drawNRGBA(t *testing.T, d *Renderer, scr uv.Screen) *image.NRGBA {
	t.Helper()
	img, ok := d.Draw(scr).(*image.NRGBA)
	if !ok {
		t.Fatalf("Draw() image type = %T, want *image.NRGBA", img)
	}
	return img
}

func sample(img *image.NRGBA, area image.Rectangle) color.NRGBA {
	p := area.Min.Add(image.Pt(max(0, area.Dx()/2), max(0, area.Dy()/2)))
	if !p.In(area) {
		p = area.Min
	}
	return img.NRGBAAt(p.X, p.Y)
}

func inkBounds(img *image.NRGBA, area image.Rectangle, bg color.NRGBA) (image.Rectangle, bool) {
	bounds := image.Rectangle{
		Min: image.Pt(area.Max.X, area.Max.Y),
		Max: image.Pt(area.Min.X, area.Min.Y),
	}
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			if img.NRGBAAt(x, y) == bg {
				continue
			}
			if x < bounds.Min.X {
				bounds.Min.X = x
			}
			if y < bounds.Min.Y {
				bounds.Min.Y = y
			}
			if x+1 > bounds.Max.X {
				bounds.Max.X = x + 1
			}
			if y+1 > bounds.Max.Y {
				bounds.Max.Y = y + 1
			}
		}
	}
	return bounds, bounds.Min.X < bounds.Max.X && bounds.Min.Y < bounds.Max.Y
}

// boxStrokeWidthAtRow counts contiguous foreground columns at y within area
// that meet minR, starting from the leftmost qualifying pixel.
func boxStrokeWidthAtRow(img *image.NRGBA, area image.Rectangle, y int, minR uint8) int {
	left, right := -1, -1
	for x := area.Min.X; x < area.Max.X; x++ {
		if img.NRGBAAt(x, y).R >= minR {
			if left < 0 {
				left = x
			}
			right = x
		}
	}
	if left < 0 {
		return 0
	}
	return right - left + 1
}

func cellInkSignature(img *image.NRGBA, area image.Rectangle) string {
	var out strings.Builder
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			p := img.NRGBAAt(x, y)
			if p.R != 0 || p.G != 0 || p.B != 0 {
				out.WriteByte('#')
			} else {
				out.WriteByte('.')
			}
		}
	}
	return out.String()
}

type offsetScreen struct {
	bounds image.Rectangle
	cells  map[image.Point]*uv.Cell
}

func (s offsetScreen) Bounds() image.Rectangle {
	return s.bounds
}

func (s offsetScreen) CellAt(x, y int) *uv.Cell {
	if !image.Pt(x, y).In(s.bounds) {
		return nil
	}
	if cell := s.cells[image.Pt(x, y)]; cell != nil {
		return cell
	}
	return uv.EmptyCell.Clone()
}

func (s offsetScreen) SetCell(x, y int, cell *uv.Cell) {
	if s.cells == nil {
		s.cells = map[image.Point]*uv.Cell{}
	}
	s.cells[image.Pt(x, y)] = cell
}

func (s offsetScreen) WidthMethod() uv.WidthMethod {
	return testWidthMethod{}
}
