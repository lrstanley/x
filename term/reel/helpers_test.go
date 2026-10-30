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

	"github.com/lrstanley/x/term/reel/internal/testutil"
	"github.com/lrstanley/x/term/reel/sink"
	"github.com/lrstanley/x/term/reel/vt"
)

const ms = time.Millisecond

// fakeAnimator is a [testutil.Rasterizer] whose frames animate while the
// title is "spin", or "breathe", which only changes every 200ms. Repaint
// stores the offset, in units of 10ms, in the alpha of the bottom right
// pixel.
type fakeAnimator struct{ testutil.Rasterizer }

func (*fakeAnimator) Animates(st vt.State) bool {
	return st.Title == "spin" || st.Title == "breathe"
}

func (*fakeAnimator) Changes(st vt.State, from, to time.Duration) bool {
	if st.Title == "breathe" {
		return from/(200*ms) != to/(200*ms)
	}
	return st.Title == "spin"
}

func (*fakeAnimator) Repaint(dst *image.NRGBA, _ vt.Screen, _ vt.State, _, at time.Duration) {
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
