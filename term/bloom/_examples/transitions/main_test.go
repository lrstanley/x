// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"testing"
	"time"

	"github.com/lrstanley/x/term/bloom/_examples/internal/demo"
	"github.com/lrstanley/x/term/steep"
	"github.com/lrstanley/x/term/steep/snapshot"
)

func TestTransitions(t *testing.T) {
	t.Parallel()

	h := steep.NewHarness(t, newModel(), demo.Options("transitions", 124, 34)...)

	// One loop: in, held, and out again.
	h.Wait("Ease: Slide with OutBack").Wait("t = 1.00").Sleep(300 * time.Millisecond)
	h.Wait("t = 0.00").Sleep(300 * time.Millisecond)
	h.Wait("t = 1.00")

	// Paused, the gallery settles fully shown, the same every time.
	h.Key("space").Wait("t = 1.00").Sleep(500 * time.Millisecond)
	h.AssertSnapshot(snapshot.WithSuffix("gallery"))

	// The iris opens from the button that was used, then closes again.
	h.Key("d").Wait("Opened by an iris").Sleep(900 * time.Millisecond)
	h.AssertSnapshot(snapshot.WithSuffix("iris"))
	h.Key("esc").WaitNot("Opened by an iris").Sleep(700 * time.Millisecond)

	h.Key("u").Wait("Opened by unfolding").Sleep(900 * time.Millisecond)
	h.AssertSnapshot(snapshot.WithSuffix("unfold"))
	h.Key("esc").WaitNot("Opened by unfolding").Sleep(700 * time.Millisecond)

	h.Key("q")
	h.WaitFinished()
}
