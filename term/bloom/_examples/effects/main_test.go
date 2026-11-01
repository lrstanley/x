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

func TestEffects(t *testing.T) {
	t.Parallel()

	h := steep.NewHarness(t, newModel(), demo.Options("effects", 120, 32)...)

	// One full loop: in, held, and out again.
	h.Wait("Custom: sweep").Wait("t = 1.00").Sleep(900 * time.Millisecond)
	h.Wait("t = 0.00").Sleep(400 * time.Millisecond)

	// Each backdrop in turn over the gallery, then none.
	for _, name := range []string{"Dim", "Desaturate", "Tint", "Recolor"} {
		h.Key("b").Wait("Backdrop: " + name).Sleep(900 * time.Millisecond)
	}
	h.Key("b").Wait("backdrop (none)").WaitNot("Backdrop:").Sleep(400 * time.Millisecond)

	h.Key("space").Sleep(300 * time.Millisecond)
	h.Key("q")
	h.WaitFinished()
}
