// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/steep"
)

// The recording is only written when exporting is turned on, as with:
//
//	STEEP_EXPORT_MODE=on go test -v -count 1 ./reel-simple/
//
// which writes testdata/TestGenerate.gif, played once, and
// testdata/TestGenerate.mp4 (the MP4 needs ffmpeg). Each output's query
// parameters set its encoding options (see reel.ParseFile).

func TestGenerate(t *testing.T) {
	h := steep.NewHarness(
		t, newRankingModel(),
		steep.WithRecording(reel.Profile{
			Outputs: reel.MustFiles("{name}.gif?loop=once", "{name}.mp4?quality=90"),
			Render: []render.Option{
				render.WithWindowChrome(true),
				render.WithPadding(5),
				render.WithScale(1.5),
			},
		}),
		// steep.WithWindowSize(160, 40),
		steep.WithArtifacts("testdata"),
		steep.WithSlow(10*time.Millisecond),
	)

	for range 10 {
		h.KeyDown()
	}

	for range 10 {
		h.KeyUp()
	}
}
