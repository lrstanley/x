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

func TestTicker(t *testing.T) {
	t.Parallel()

	h := steep.NewHarness(t, newModel(), demo.Options("ticker", 56, 14)...)

	// Long names are cut off. The selected one waits a moment, then scrolls.
	h.Wait("markets").Wait("Globex International Holdings G…").Wait("Stark Industries Advanced Resea…")
	h.Wait("ACME Corp").Sleep(500 * time.Millisecond)

	// Moving the selection starts the next name over: cut off, then scrolling
	// until the end of it is in view.
	h.Key("down").Sleep(300 * time.Millisecond)
	h.Wait("Holdings Group")
	h.Key("down").Key("down").Wait("Umbrella Pharmaceuticals and Bi…").Wait("Biotechnology")

	// The banner slides in, holds, and slides out again.
	h.Key("b").Wait("Alert 1").Sleep(400 * time.Millisecond)
	h.WaitNot("Alert 1")

	// Paused, nothing moves, so the screen is the same every time.
	h.Key("space").Wait("NEWS  Central bank holds").Wait("Umbrella Pharmaceuticals and Bi…").Sleep(300 * time.Millisecond)
	h.AssertSnapshot()

	// A click on a row selects it, while paused too.
	h.LeftClick(10, 5).Sleep(200 * time.Millisecond)
	h.Key("space").Sleep(400 * time.Millisecond)

	h.Key("q")
	h.WaitFinished()
	if got := h.FinalModelAs[*model]().selected; got != 2 {
		t.Fatalf("selected = %d after clicking the third row, want 2", got)
	}
}
