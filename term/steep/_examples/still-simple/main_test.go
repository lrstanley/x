// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"testing"
	"time"

	"github.com/lrstanley/x/term/steep"
	"github.com/lrstanley/x/term/still/fonts"
)

// Example of outputting GIFs from a given test:
//  STEEP_EXPORT_DIR=testdata/ STEEP_EXPORT_MODE=on STEEP_EXPORT_TYPE=gif STEEP_SLOW=10ms go test -v -count 1 ./...

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

// func TestGenerate(t *testing.T) {
// 	opts := []still.Option{
// 		still.WithFontSizePt(still.Pt(20)),
// 		still.WithBorderRadius(10),
// 		still.WithMargin(40, lipgloss.Color("#812cd1")),
// 		still.WithPadding(10),
// 	}

// 	h := steep.NewHarness(t, newRankingModel(), steep.WithRenderer(opts...))
// 	generateStill(h, "testdata/test.png", "testdata/test.gif")
// }

func TestGenerateFira(t *testing.T) {
	// fira := firaFontFamily(t)

	// opts := []still.Option{
	// 	still.WithFontFamily(fira),
	// 	still.WithCodepointMap(map[string]fonts.FontFamily{
	// 		"U+E000-U+F8FF": fira,
	// 	}),
	// 	still.WithFontSizePt(still.Pt(40)),
	// 	still.WithCellWidth(-0.1),
	// }

	h := steep.NewHarness(t, newRankingModel()) // steep.WithWindowSize(120, 30),
	// steep.WithRenderer(opts...),
	// steep.WithExportDir("testdata/"),
	// steep.WithExport(steep.ExportOn, steep.ExportTypeGIF),
	// steep.WithSlow(200*time.Millisecond), // should I get rid of this?
	// steep.WithExportFPS(20),
	// steep.WithExportSpeed(1),

	for range 10 {
		h.KeyDown()
	}
	for range 10 {
		h.KeyUp()
	}
}

// TestTitleCards records the ranking list between the animated steep logo and
// a PASS/FAIL outro card. Run it with STEEP_EXPORT_DIR set (or "go test
// -artifacts") to keep the GIF.
func TestTitleCards(t *testing.T) {
	h := steep.NewHarness(t, newRankingModel(),
		steep.WithExport(steep.ExportOn, steep.ExportTypeGIF),
		steep.WithExportName("title_cards"),
		steep.WithExportIntro(1500*time.Millisecond),
		steep.WithExportOutro(1500*time.Millisecond),
	)
	h.Wait("Tokyo", steep.WithANSI(false))
	for range 10 {
		h.KeyDown()
	}
	for range 10 {
		h.KeyUp()
	}
	time.Sleep(500 * time.Millisecond)
}

// generateStill writes committed testdata. For failure artifacts in CI, prefer
// steep.WithFailureGIF() or steep.WithExport(steep.ExportOnFailure, ...) so
// frames are captured via harness hooks into t.ArtifactDir().
// func generateStill(h *steep.Harness, pngPath, gifPath string) {
// 	h.Wait("Tokyo", steep.WithANSI(false))

// 	export.MustPNG(h.Image(), pngPath)

// 	gifCloser := export.MustGIF(gifPath, export.WithFrameRate(50, false, func() image.Image {
// 		return h.Image()
// 	}))
// 	defer gifCloser()

// 	for range 20 {
// 		h.KeyDown()
// 		time.Sleep(30 * time.Millisecond)
// 	}
// 	for range 20 {
// 		h.KeyUp()
// 		time.Sleep(30 * time.Millisecond)
// 	}
// 	time.Sleep(400 * time.Millisecond)
// }
