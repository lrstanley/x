// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"image"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/lrstanley/x/charm/steep"
	"github.com/lrstanley/x/charm/still"
	"github.com/lrstanley/x/charm/still/export"
	"github.com/lrstanley/x/charm/still/fonts"
)

func firaFontFamily(t *testing.T) fonts.FontFamily {
	t.Helper()
	family, err := fonts.LoadFamily(fonts.FontFamilyNames{
		Regular: "FiraMonoNerdFontMono-Regular",
		Bold:    "FiraMonoNerdFontMono-Bold",
	})
	if err != nil {
		t.Skipf("fira mono nerd font mono not installed: %v", err)
	}
	return family
}

func TestGenerate(t *testing.T) {
	opts := []still.Option{
		still.WithFontSizePt(still.Pt(20)),
		still.WithBorderRadius(10),
		still.WithMargin(40, lipgloss.Color("#812cd1")),
		still.WithPadding(10),
	}

	h := steep.NewHarness(t, newRankingModel(), steep.WithImageRenderer(opts...))
	generateStill(h, "testdata/test.png", "testdata/test.gif")
}

func TestGenerateFira(t *testing.T) {
	fira := firaFontFamily(t)

	opts := []still.Option{
		still.WithFontFamily(fira),
		still.WithCodepointMap(map[string]fonts.FontFamily{
			"U+E000-U+F8FF": fira,
		}),
		still.WithFontSizePt(still.Pt(40)),
		still.WithCellWidth(-0.1),
	}

	h := steep.NewHarness(t, newRankingModel(), steep.WithWindowSize(120, 30), steep.WithImageRenderer(opts...))
	generateStill(h, "testdata/test-fira.png", "testdata/test-fira.gif")
}

func generateStill(h *steep.Harness, pngPath, gifPath string) {
	h.WaitString("Tokyo", steep.WithANSI(false))

	export.MustPNG(h.Image(), pngPath)

	gifCloser := export.MustGIF(gifPath, export.WithFrameRate(50, false, func() image.Image {
		return h.Image()
	}))
	defer gifCloser()

	for range 20 {
		h.KeyDown()
		time.Sleep(30 * time.Millisecond)
	}
	for range 20 {
		h.KeyUp()
		time.Sleep(30 * time.Millisecond)
	}
	time.Sleep(400 * time.Millisecond)
}
