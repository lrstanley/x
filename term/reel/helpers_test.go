// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"os"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/reel/sink"
	"github.com/lrstanley/x/term/reel/vt/screen"
)

const ms = time.Millisecond

// fakeRasterizer draws each cell as one pixel, so no fonts load: red is the
// first byte of the cell's text, green marks the cursor, and blue is set
// while the window is focused.
type fakeRasterizer struct {
	draws  int
	closed bool
}

func (*fakeRasterizer) Layout(cols, rows int) render.Layout {
	r := image.Rect(0, 0, cols, rows)
	return render.Layout{Image: r, Grid: r, Cell: image.Pt(1, 1), Scale: 1}
}

func (f *fakeRasterizer) Draw(dst *image.NRGBA, scr screen.Screen, st screen.State, _ time.Duration) {
	f.draws++
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

func (f *fakeRasterizer) Close() error { f.closed = true; return nil }

// fakeAnimator is a [fakeRasterizer] whose frames animate while the title is
// "spin", or "breathe", which only changes every 200ms. Repaint stores the
// offset, in units of 10ms, in the alpha of the bottom right pixel.
type fakeAnimator struct{ fakeRasterizer }

func (*fakeAnimator) Animates(st screen.State) bool {
	return st.Title == "spin" || st.Title == "breathe"
}

func (*fakeAnimator) Changes(st screen.State, from, to time.Duration) bool {
	if st.Title == "breathe" {
		return from/(200*ms) != to/(200*ms)
	}
	return st.Title == "spin"
}

func (*fakeAnimator) Repaint(dst *image.NRGBA, _ screen.Screen, _ screen.State, at time.Duration) {
	p := dst.Rect.Max.Sub(image.Pt(1, 1))
	c := dst.NRGBAAt(p.X, p.Y)
	c.A = uint8(at / (10 * ms))
	dst.SetNRGBA(p.X, p.Y, c)
}

// collectSink records the frames it is given, with copies of their images
// in images.
type collectSink struct {
	frames    []sink.Frame
	images    []*image.NRGBA
	closed    bool
	discarded bool
}

func (s *collectSink) WriteFrame(f sink.Frame) error {
	src, ok := f.Image.(*image.NRGBA)
	if !ok {
		return fmt.Errorf("frame image is %T", f.Image)
	}
	img := &image.NRGBA{Pix: bytes.Clone(src.Pix), Stride: src.Stride, Rect: src.Rect}
	f.Image = img
	s.frames = append(s.frames, f)
	s.images = append(s.images, img)
	return nil
}

func (s *collectSink) Close() error   { s.closed = true; return nil }
func (s *collectSink) Discard() error { s.discarded = true; return nil }

func (s *collectSink) delays() []time.Duration {
	out := make([]time.Duration, len(s.frames))
	for i, f := range s.frames {
		out[i] = f.Delay
	}
	return out
}

// labelImage is a 1x1 image whose red channel identifies the frame.
func labelImage(label uint8) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: label, A: 0xff})
	return img
}

func imageLabel(img image.Image) uint8 {
	r, _, _, _ := img.At(0, 0).RGBA()
	return uint8(r >> 8)
}

// buildTimeline returns a timeline with header h and events, each at its own
// offset.
func buildTimeline(h Header, events ...Event) *Timeline {
	tl := newTimeline(h)
	for _, ev := range events {
		tl.appendAt(ev, ev.At())
	}
	return tl
}

// outAt returns an [Output] event of s at offset atMS milliseconds.
func outAt(atMS int, s string) Output { return Output{event: at(atMS), Data: []byte(s)} }

// effectFunc is an [Effect] whose stage is fn.
type effectFunc func(f Frame, emit func(Frame))

func (e effectFunc) Start(*Context) (Stage, error) { return Stage(e), nil }

// readGIF decodes the GIF at path.
func readGIF(t *testing.T, path string) *gif.GIF {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return g
}
