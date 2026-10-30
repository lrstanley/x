// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"image"
	"image/color"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	idraw "github.com/lrstanley/x/term/reel/render/internal/draw"
	"github.com/lrstanley/x/term/reel/render/internal/fontset"
	"github.com/lrstanley/x/term/reel/vt"
	"golang.org/x/image/font"
)

// scaleFor returns the scale at which the font is size points at 96 DPI, so
// tests can ask for a known cell geometry.
func scaleFor[T int | float64](size T) float64 {
	return float64(size) / 11
}

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
	*vt.Buffer
}

func newTestScreen(width, height int) testScreen {
	return testScreen{Buffer: vt.NewBuffer(width, height)}
}

// putText writes s into row y of scr from column x, one grapheme per cell.
func putText(scr vt.Screen, x, y int, s string, style vt.Style) {
	var state byte
	for s != "" {
		seq, width, n, st := ansi.DecodeSequence(s, state, nil)
		s, state = s[n:], st
		if width == 0 {
			continue
		}
		scr.SetCell(x, y, &vt.Cell{Content: seq, Width: width, Style: style})
		x += width
	}
}

// focused is the state most tests draw with: a focused window, so it is not
// dimmed, with nothing else set.
var focused = vt.State{Focused: true}

// newImage returns an image that fits scr drawn by d.
func newImage(d *Renderer, scr vt.Screen) *image.NRGBA {
	return image.NewNRGBA(d.Layout(scr.Bounds().Dx(), scr.Bounds().Dy()).Image)
}

// drawNRGBA draws scr with the [focused] state, as of offset 0, into a new
// image.
func drawNRGBA(t *testing.T, d *Renderer, scr vt.Screen) *image.NRGBA {
	t.Helper()
	return drawAt(t, d, scr, focused, 0)
}

// drawAt draws scr with st, as of offset at, into a new image.
func drawAt(t *testing.T, d *Renderer, scr vt.Screen, st vt.State, at time.Duration) *image.NRGBA {
	t.Helper()
	img := newImage(d, scr)
	d.Draw(img, scr, st, at)
	return img
}

// drawFresh draws scr with st, as of offset at, into a new image without the
// frame cache: what [Renderer.Draw] should copy out.
func drawFresh(d *Renderer, scr vt.Screen, st vt.State, at time.Duration) *image.NRGBA {
	d.mu.Lock()
	defer d.mu.Unlock()
	f := d.borrowRenderFrame(image.Point{}, scr, st, at)
	defer releaseRenderFrame(f)
	img := image.NewNRGBA(f.imageBounds)
	d.render(img, scr, f)
	return img
}

// rowColors draws scr with st on d and samples each cell of its top row. It
// clears scr's dirty rows first, so d can reuse its last frame.
func rowColors(t *testing.T, d *Renderer, scr *vt.Buffer, st vt.State) []color.NRGBA {
	t.Helper()
	scr.ClearDirty()
	img := drawAt(t, d, scr, st, 0)
	f := d.layoutSnapshot(t, image.Point{}, scr)
	out := make([]color.NRGBA, scr.Width())
	for x := range out {
		out[x] = sample(img, f.cellBounds(x, 0))
	}
	return out
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

// cellInkSignature marks each pixel of area that differs from bg.
func cellInkSignature(img *image.NRGBA, area image.Rectangle, bg color.NRGBA) string {
	var out strings.Builder
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			if img.NRGBAAt(x, y) != bg {
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
	cells  map[image.Point]*vt.Cell
}

func (s offsetScreen) Bounds() image.Rectangle {
	return s.bounds
}

func (s offsetScreen) CellAt(x, y int) *vt.Cell {
	if !image.Pt(x, y).In(s.bounds) {
		return nil
	}
	if cell := s.cells[image.Pt(x, y)]; cell != nil {
		return cell
	}
	return vt.EmptyCell.Clone()
}

func (s offsetScreen) SetCell(x, y int, cell *vt.Cell) {
	if s.cells == nil {
		s.cells = map[image.Point]*vt.Cell{}
	}
	s.cells[image.Pt(x, y)] = cell
}

// faceForCell resolves the face for cell's own content.
func faceForCell(fs *fontset.Set, cell *vt.Cell) font.Face {
	cluster, _ := idraw.NewCluster(cell.Content)
	return fs.FaceForCell(cell, cluster)
}

// layoutSnapshot prepares per-draw geometry for tests, with a zero state at
// offset 0. Snapshotted metrics, palette, and layout rectangles are safe after
// the lock is released. FontFace still reads the renderer font set, which is
// immutable until the next rebuild.
func (d *Renderer) layoutSnapshot(t interface{ Helper() }, origin image.Point, scr vt.Screen) *renderFrame {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	f := &renderFrame{}
	d.populateRenderFrame(f, origin, scr, vt.State{}, 0)
	return f
}
