// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package sink

import (
	"errors"
	"fmt"
	"image"
	"io"
	"path/filepath"
	"strings"
	"time"
)

// Frame is one frame of a recording, as a [Sink] is given it.
type Frame struct {
	Image image.Image
	// Delay is how long the frame plays, and should be positive.
	Delay time.Duration
	// Isolated palettizes this frame on its own palette. It adds no colors to
	// the shared recording palette, and consecutive isolated frames share one
	// palette so they stay comparable to each other. GIF stores that palette
	// as a local color table on each of those frames, and other formats
	// ignore it. Terminal frames leave it false.
	Isolated bool
}

var (
	errNilFrame   = errors.New("sink: nil frame")
	errEmptyFrame = errors.New("sink: empty frame bounds")
)

// Sink writes frames into one output, such as [GIFEncoder] or
// [VideoEncoder]. Sinks are called from a single goroutine, so they need not
// be safe for concurrent use.
type Sink interface {
	// WriteFrame adds the next frame, which plays for f.Delay. f.Image is only
	// valid until WriteFrame returns, so a sink that keeps it must copy it.
	WriteFrame(f Frame) error
	// Close finishes the output. With no frames written, it writes nothing.
	Close() error
	// Discard abandons the output, leaving any existing destination alone.
	Discard() error
}

// ErrUnknownFormat is wrapped by the error [File] and [Writer] return for a
// format they do not write.
var ErrUnknownFormat = errors.New("sink: unknown format")

// Format is an output format, named by its file extension without the dot.
type Format string

const (
	// FormatGIF is an animated GIF ([GIFEncoder]).
	FormatGIF Format = "gif"
	// FormatPNG is a PNG of the last frame (see [PNG] for its encoding).
	FormatPNG Format = "png"
	// FormatMP4 is H.264 in MP4 (libx264), which plays nearly everywhere.
	FormatMP4 Format = "mp4"
	// FormatWebM is VP9 in WebM (libvpx-vp9).
	FormatWebM Format = "webm"
	// FormatWebP is animated WebP (libwebp). It keeps transparency and
	// [WithLoopCount], but ffmpeg builds without libwebp are common.
	FormatWebP Format = "webp"
)

var videoFormats = []Format{FormatMP4, FormatWebM, FormatWebP}

// FormatOf returns the format the extension of path names, compared without
// case, or an error wrapping [ErrUnknownFormat].
func FormatOf(path string) (Format, error) {
	f := Format(strings.ToLower(strings.TrimPrefix(filepath.Ext(path), ".")))
	switch f {
	case FormatGIF, FormatPNG, FormatMP4, FormatWebM, FormatWebP:
		return f, nil
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownFormat, filepath.Ext(path))
}

// File returns a sink writing to path in the format its extension names (see
// [FormatOf]): [CreateGIF] for ".gif", [CreateVideo] for ".mp4", ".webm", and
// ".webp", which probes ffmpeg here, and for ".png" a sink that writes the
// last frame it was given. Every one writes a temporary file beside path,
// renamed into place on Close.
func File(path string, opts ...Option) (Sink, error) {
	format, err := FormatOf(path)
	if err != nil {
		return nil, err
	}
	switch format {
	case FormatGIF:
		return CreateGIF(path, opts...)
	case FormatPNG:
		return createPNGSink(path, opts)
	case FormatMP4, FormatWebM, FormatWebP:
		return CreateVideo(path, format, opts...)
	}
	return nil, fmt.Errorf("%w: %q", ErrUnknownFormat, format)
}

// Writer returns a sink writing format to w, which receives the whole output
// on Close: [NewGIF], [NewVideo], or for [FormatPNG] the last frame. An
// unknown format is an error wrapping [ErrUnknownFormat].
func Writer(w io.Writer, format Format, opts ...Option) (Sink, error) {
	if w == nil {
		return nil, errors.New("sink: nil writer")
	}
	switch format {
	case FormatGIF:
		return NewGIF(w, opts...)
	case FormatPNG:
		return newPNGSink(w, opts)
	case FormatMP4, FormatWebM, FormatWebP:
		return NewVideo(w, format, opts...)
	}
	return nil, fmt.Errorf("%w: %q", ErrUnknownFormat, format)
}
