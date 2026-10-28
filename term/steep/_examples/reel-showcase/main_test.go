// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/effects"
	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/steep"
)

// The recording is only written when exporting is turned on, as with:
//
//	STEEP_EXPORT_DIR=testdata/ STEEP_EXPORT_MODE=on go test -v -count 1 ./reel-showcase/
//
// which writes testdata/showcase.gif and testdata/showcase.cast. Exporting
// the .cast again with the same profile writes the same GIF.

// showcaseProfile is what the showcase's recording writes: the steep logo,
// the keys pressed, a badged pause on the last frame, and a PASS or FAIL
// card after it, in a window with chrome.
var showcaseProfile = reel.Profile{
	Outputs: reel.MustFiles("showcase.gif", "showcase.cast"),
	Render: []render.Option{
		render.WithWindowChrome(true),
		render.WithPadding(14),
		render.WithMargin(36, lipgloss.Color("#1d212c")),
		render.WithBorderRadius(12),
	},
	Effects: []reel.Effect{
		steep.Logo(2 * time.Second),
		effects.Keys(1200 * time.Millisecond),
		effects.Pause(),
		steep.Outro(2 * time.Second),
	},
}

func TestShowcase(t *testing.T) {
	h := steep.NewHarness(
		t, newModel(),
		steep.WithWindowSize(110, 32),
		steep.WithTimeout(10*time.Second),
		// steep's terminal reports 256 colors by default; the gradients
		// need every bit.
		steep.WithColor(steep.ColorTrueColor),
		steep.WithRecording(showcaseProfile),
	)

	// Boot: the wordmark sweeps in while OSC 9;4 reports progress.
	h.Wait("all systems nominal").
		Wait("control room").
		Sleep(2500 * time.Millisecond)

	// Cycle every theme; each switch cross-fades the whole palette,
	// including the terminal background the window chrome is tinted from.
	for range themes {
		h.Key("t")
		h.Sleep(1400 * time.Millisecond)
	}

	// Deploy the selected service. OSC 7501 reports it as working (comets
	// around the window), then blocked on a question while it waits for
	// approval (a breathing outline), then done.
	h.Key("d").
		Wait("approval required").
		Sleep(1800 * time.Millisecond).
		Key("y").
		Wait("is live").
		Sleep(2200 * time.Millisecond)

	// Deploy a service whose tests fail, which reports an error.
	h.Key("j").
		Sleep(250 * time.Millisecond).
		Key("j").
		Sleep(400 * time.Millisecond).
		Key("d").
		Wait("deploy failed").
		Sleep(2200 * time.Millisecond)

	// The recording ends here, on the failed deploy: Close, which the
	// harness runs as a cleanup, quits the program, and leaves its exit out.
}
