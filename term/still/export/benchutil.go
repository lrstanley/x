// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package export

import (
	"image"
	"image/color"
	"time"
)

// PalettizedRecorder captures palettized GIF frames using the production append path.
type PalettizedRecorder struct {
	rec     gifRecording
	palette color.Palette
	useLUT  bool
}

// NewPalettizedRecorder returns a recorder configured like default GIF capture.
func NewPalettizedRecorder() *PalettizedRecorder {
	adaptive := newAdaptivePalette(true)
	return &PalettizedRecorder{
		palette: adaptive.pal,
		rec: gifRecording{
			loopCount: 0,
			config:    image.Config{ColorModel: adaptive.pal},
			adaptive:  adaptive,
		},
	}
}

// AddFrame palettizes and appends one frame.
func (fr *PalettizedRecorder) AddFrame(frame image.Image, delay time.Duration) error {
	return appendRecordingFrame(
		&fr.rec, fr.palette, fr.useLUT,
		OptimizeFrames, nil, false, false, frame, delay,
	)
}
