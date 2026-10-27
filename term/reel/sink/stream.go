// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package sink

import (
	"errors"
	"image"
	"image/draw"
)

// Chan returns a sink that sends each frame to ch, with a copy of its image
// that the receiver owns. WriteFrame blocks until ch takes the frame. Close
// and Discard close ch, and frames written after are an error.
func Chan(ch chan<- Frame) Sink {
	return &chanSink{ch: ch}
}

type chanSink struct {
	ch     chan<- Frame
	closed bool
}

func (s *chanSink) WriteFrame(f Frame) error {
	switch {
	case s.closed:
		return errors.New("sink: write to closed channel sink")
	case f.Image == nil:
		return errNilFrame
	}
	b := f.Image.Bounds()
	img := image.NewNRGBA(b)
	draw.Draw(img, b, f.Image, b.Min, draw.Src)
	s.ch <- Frame{Image: img, Delay: f.Delay, Isolated: f.Isolated}
	return nil
}

func (s *chanSink) Close() error {
	if !s.closed {
		s.closed = true
		close(s.ch)
	}
	return nil
}

func (s *chanSink) Discard() error { return s.Close() }

// Tee returns a sink that writes every frame to each of sinks, in order. A
// frame goes to every sink even when one fails, and the errors are joined.
// Close closes, and Discard discards, every sink.
func Tee(sinks ...Sink) Sink {
	return tee(sinks)
}

type tee []Sink

func (t tee) WriteFrame(f Frame) error {
	return t.each(func(s Sink) error { return s.WriteFrame(f) })
}

func (t tee) Close() error   { return t.each(Sink.Close) }
func (t tee) Discard() error { return t.each(Sink.Discard) }

// each calls fn for every sink, and joins the errors.
func (t tee) each(fn func(Sink) error) error {
	var err error
	for _, s := range t {
		if serr := fn(s); serr != nil {
			err = errors.Join(err, serr)
		}
	}
	return err
}
