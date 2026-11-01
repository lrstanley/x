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

func TestReflow(t *testing.T) {
	t.Parallel()
	h := steep.NewHarness(t, newModel(), demo.Options("reflow", 100, 26)...)
	h.Wait("automatic layout animations").Wait("selected: Atlas")
	h.Key("b").Wait("sidebar: false").Sleep(2 * time.Second)
	h.Key("s").Wait("reverse: true").Sleep(time.Second)
	h.Key("r").Wait("ratio: 50").Sleep(time.Second)
	h.Key("r").Wait("ratio: 70").Sleep(time.Second)
	h.Key("d").Wait("details: false").Sleep(time.Second)
	h.Key("down").Wait("selected: Birch").Sleep(2 * time.Second)
	h.Key("b").Wait("sidebar: true").Sleep(2 * time.Second)
	h.Key("d").Wait("details: true").Wait("Selected: Birch").Sleep(time.Second)
	h.AssertSnapshot()
	h.Key("q").WaitFinished()
}
