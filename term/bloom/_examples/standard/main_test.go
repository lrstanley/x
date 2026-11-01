// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"testing"
	"time"

	"github.com/lrstanley/x/term/steep"
)

func TestStandard(t *testing.T) {
	t.Parallel()

	h := steep.NewHarness(t, newModel(), steep.WithWindowSize(70, 12), steep.WithTimeout(5*time.Second))
	h.Wait("# Home")
	h.LeftClick(3, 2).Wait("# Settings").Wait("opened Settings")
	h.AssertSnapshot()
	h.Key("q")
	h.WaitFinished()
}
