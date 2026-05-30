// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package export

import (
	"image"
	"image/color"
	"time"
)

// FrameRecorder captures palettized GIF frames using the production append path.
type FrameRecorder struct {
	rec     gifRecording
	palette color.Palette
	useLUT  bool
}

// NewFrameRecorder returns a recorder configured like default GIF capture.
func NewFrameRecorder() *FrameRecorder {
	pal := defaultGIFPalette()
	return &FrameRecorder{
		palette: pal,
		useLUT:  true,
		rec: gifRecording{
			loopCount: 0,
			config:    image.Config{ColorModel: pal},
		},
	}
}

// AddFrame palettizes and appends one frame.
func (fr *FrameRecorder) AddFrame(frame image.Image, delay time.Duration) error {
	return appendRecordingFrame(
		&fr.rec, fr.palette, fr.useLUT,
		OptimizeFrames, nil, false, frame, delay, 0,
	)
}
