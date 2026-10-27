// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package sink

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"os"
)

// PNG writes img to path using opts. By default [OptimizeAll] applies (indexed
// paletted PNG via the internal palette). Pass [WithOptimize]([OptimizeNone])
// to encode full RGBA with alpha preserved. Options are checked before path
// is opened, so an invalid option leaves an existing file alone.
func PNG(img image.Image, path string, opts ...Option) error {
	cfg, err := collectOptions(validatePNG, opts...)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	return encodePNG(img, f, cfg)
}

// WritePNG encodes img to w. See [PNG] for format behavior and option notes.
func WritePNG(img image.Image, w io.Writer, opts ...Option) error {
	cfg, err := collectOptions(validatePNG, opts...)
	if err != nil {
		return err
	}
	return encodePNG(img, w, cfg)
}

// MustPNG is a convenience wrapper around [PNG] that panics on error.
func MustPNG(img image.Image, path string, opts ...Option) {
	if err := PNG(img, path, opts...); err != nil {
		panic(err)
	}
}

func encodePNG(img image.Image, w io.Writer, cfg options) error {
	if cfg.optimize.has(OptimizeColorQuantization) {
		adaptive := newAdaptivePalette(false)
		pm := image.NewPaletted(img.Bounds(), adaptive.pal)
		adaptive.palettize(pm, img)
		compactPalette([]*image.Paletted{pm}, adaptive.pal)
		return png.Encode(w, pm)
	}
	return png.Encode(w, img)
}

// pngSink keeps a copy of the last frame, which Close encodes as a PNG to w,
// or to file when it writes one.
type pngSink struct {
	opts options
	w    io.Writer
	file *pendingFile
	last *image.NRGBA
	done bool
}

// createPNGSink returns a pngSink writing to a temporary file beside path,
// renamed into place on Close.
func createPNGSink(path string, opts []Option) (*pngSink, error) {
	cfg, err := collectOptions(validatePNG, opts...)
	if err != nil {
		return nil, err
	}
	file, err := createPending(path)
	if err != nil {
		return nil, fmt.Errorf("sink: creating png output: %w", err)
	}
	return &pngSink{opts: cfg, w: file.f, file: file}, nil
}

func newPNGSink(w io.Writer, opts []Option) (*pngSink, error) {
	cfg, err := collectOptions(validatePNG, opts...)
	if err != nil {
		return nil, err
	}
	return &pngSink{opts: cfg, w: w}, nil
}

func (s *pngSink) WriteFrame(f Frame) error {
	switch {
	case s.done:
		return errors.New("sink: write to closed png sink")
	case f.Image == nil:
		return errNilFrame
	case f.Image.Bounds().Empty():
		return errEmptyFrame
	}
	b := f.Image.Bounds()
	if s.last == nil || s.last.Rect != b {
		s.last = image.NewNRGBA(b)
	}
	draw.Draw(s.last, b, f.Image, b.Min, draw.Src)
	return nil
}

// Close writes the last frame. With no frames written, it writes nothing.
func (s *pngSink) Close() error {
	if s.done {
		return nil
	}
	s.done = true
	if s.last == nil {
		return s.Discard()
	}
	if err := encodePNG(s.last, s.w, s.opts); err != nil {
		_ = s.abort()
		return err
	}
	if s.file != nil {
		return s.file.commit()
	}
	return nil
}

func (s *pngSink) Discard() error {
	s.done = true
	return s.abort()
}

func (s *pngSink) abort() error {
	if s.file == nil {
		return nil
	}
	err := s.file.abort()
	s.file = nil
	return err
}
