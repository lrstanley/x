// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"testing"

	"github.com/lrstanley/x/term/steep"
	"github.com/lrstanley/x/term/still"
)

// STEEP_EXPORT_DIR=testdata/ STEEP_EXPORT_MODE=on STEEP_EXPORT_TYPE=gif STEEP_SLOW=10ms go test -v -count 1 ./...

func TestGenerate(t *testing.T) {
	h := steep.NewHarness(
		t, newRankingModel(),
		steep.WithRenderer(
			still.WithWindowChrome(true),
			still.WithPadding(5),
			// still.WithFontSize(20),
		),
		// steep.WithWindowSize(160, 40),
	)

	for range 10 {
		h.KeyDown()
	}

	for range 10 {
		h.KeyUp()
	}
}
