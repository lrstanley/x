// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"testing"
	"time"

	"github.com/lrstanley/x/term/bloom/_examples/internal/demo"
	"github.com/lrstanley/x/term/steep"
)

func TestTour(t *testing.T) {
	h := steep.NewHarness(t, newModel(), demo.Options("dashboard", 120, 30)...)
	demo.ResizeInBand(t, h)

	h.Wait("fleet").Sleep(600 * time.Millisecond)
	h.Key("down").Sleep(150 * time.Millisecond).Key("down").Sleep(150 * time.Millisecond).Key("down")
	h.Wait("cache").Sleep(400 * time.Millisecond)

	// A click selects a service: the header is row 0 and the sidebar's border
	// row 1, so the sixth service, ingest, is on row 7.
	h.LeftClick(4, 7).Wait("◆ fleet  ·  10 services  ·  ingest").Sleep(400 * time.Millisecond)

	// The dialog slides up over a dimmed, desaturated backdrop.
	h.Key("enter").Wait("Restart ingest?").Sleep(900 * time.Millisecond)
	h.Key("right").Sleep(400 * time.Millisecond).Key("left").Sleep(400 * time.Millisecond)
	h.Key("enter").WaitNot("Restart ingest?").Wait("restarting ingest").Sleep(500 * time.Millisecond)

	// Every effect, on a toast.
	for _, k := range []string{"1", "2", "3", "4", "5", "6"} {
		h.Key(k).Sleep(900 * time.Millisecond)
	}

	// The sidebar springs shut and open again.
	h.Key("ctrl+b").Sleep(700 * time.Millisecond).Key("ctrl+b").Sleep(700 * time.Millisecond)

	// Narrow terminals collapse the log panel first, then the sidebar. The
	// status bar stays one row, cutting the key hints rather than the timing.
	h.Resize(84, 30).Wait("of 120fps").Sleep(700 * time.Millisecond)
	h.Resize(60, 30).WaitNot("● api").Wait("of 120fps").Sleep(700 * time.Millisecond)
	h.Resize(120, 30).Wait("● api").Sleep(900 * time.Millisecond)

	h.Key("q")
	h.WaitFinished()
}
