// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package demo holds what the examples' tests share: the steep options for
// a tour that is recorded when exporting is turned on, as with:
//
//	STEEP_EXPORT_DIR=/tmp/previews STEEP_EXPORT_MODE=on go test -count 1 ./...
package demo

import (
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/effects"
	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/steep"
)

// Options returns the steep options for a w×h true color tour, recorded to
// name.gif when exporting is turned on.
func Options(name string, w, h int) []steep.Option {
	return []steep.Option{
		steep.WithWindowSize(w, h),
		steep.WithTimeout(20 * time.Second),
		steep.WithColor(steep.ColorTrueColor),
		steep.WithRecording(reel.Profile{
			Outputs: reel.MustFiles(name + ".gif"),
			Render: []render.Option{
				render.WithWindowChrome(true),
				render.WithPadding(10),
				render.WithMargin(24, lipgloss.Color("#16161e")),
				render.WithBorderRadius(10),
			},
			Effects: []reel.Effect{effects.Keys(time.Second), effects.Pause()},
		}),
	}
}

// ResizeInBand lets h.Resize reach the program: the harness has no
// pseudo-terminal to signal a resize with, so it is reported in band.
func ResizeInBand(t *testing.T, h *steep.Harness) {
	t.Helper()
	if _, err := h.Session().Write([]byte(ansi.SetModeInBandResize)); err != nil {
		t.Fatal(err)
	}
}
