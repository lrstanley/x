// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/lrstanley/x/term/steep"
	"github.com/lrstanley/x/term/still"
)

// STEEP_EXPORT_DIR=testdata/ STEEP_EXPORT_MODE=on STEEP_EXPORT_TYPE=gif go test -v -count 1 ./still-showcase/

func TestShowcase(t *testing.T) {
	h := steep.NewHarness(
		t, newModel(),
		steep.WithWindowSize(110, 32),
		steep.WithTimeout(10*time.Second),
		// steep's terminal reports 256 colors by default; the gradients
		// need every bit.
		steep.WithColor(steep.ColorTrueColor),
		steep.WithRenderer(
			still.WithWindowChrome(true),
			still.WithPadding(14),
			still.WithMargin(36, lipgloss.Color("#1d212c")),
			still.WithBorderRadius(12),
		),
		steep.WithExportName("showcase"),
		steep.WithExportKeys(1200*time.Millisecond),
		steep.WithExportIntro(2*time.Second),
		steep.WithExportOutro(2*time.Second),
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

	h.Key("q").
		WaitFinished()
}
