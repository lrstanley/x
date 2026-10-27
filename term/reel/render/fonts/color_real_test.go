// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fonts

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// realColorFonts indexes the color fonts in the directory named by
// REEL_COLOR_FONT_DIR (for example one holding Noto Color Emoji), or skips.
func realColorFonts(t *testing.T) []*colorFace {
	t.Helper()
	dir := os.Getenv("REEL_COLOR_FONT_DIR")
	if dir == "" {
		t.Skip("REEL_COLOR_FONT_DIR is not set")
	}
	ix := buildColorIndex(systemFontFiles([]string{dir}))
	if len(ix) == 0 {
		t.Skipf("no color fonts in %s", dir)
	}
	return ix
}

var realClusters = []struct {
	name    string
	cluster string
	ok      bool
	maybe   bool // Not every font has it: don't fail if it resolves or doesn't.
}{
	{"smile", "😀", true, false},
	{"thumbs", "👍", true, false},
	{"skin tone", "👋🏽", true, false},
	{"family zwj", "👨‍👩‍👧‍👦", true, false},
	{"technologist zwj", "🧑‍💻", true, false},
	{"flag us", "🇺🇸", true, false},
	{"flag england", "🏴\U000E0067\U000E0062\U000E0065\U000E006E\U000E0067\U000E007F", true, false},
	{"heart vs16", "❤️", true, false},
	{"keycap", "1️⃣", true, true},
	{"rainbow flag zwj", "🏳️‍🌈", true, false},
	{"letter", "A", false, false},
	{"two letters", "ab", false, false},
}

// TestRealColorFont resolves tricky clusters against an installed color font.
// With REEL_PREVIEW_DIR set it also writes a contact sheet of what resolved.
func TestRealColorFont(t *testing.T) {
	ix := realColorFonts(t)

	const cell = 64
	sheet := image.NewRGBA(image.Rect(0, 0, cell*len(realClusters), cell))
	draw.Draw(sheet, sheet.Bounds(), image.NewUniform(color.RGBA{0x30, 0x30, 0x38, 0xff}), image.Point{}, draw.Src)
	for i, tc := range realClusters {
		g, ok := lookupColor(ix, []rune(tc.cluster))
		if ok != tc.ok && !tc.maybe {
			t.Errorf("%s (%+q): resolved = %v, want %v", tc.name, tc.cluster, ok, tc.ok)
			continue
		}
		if !ok {
			continue
		}
		img := g.Image(cell, cell)
		if img == nil {
			t.Errorf("%s: no image", tc.name)
			continue
		}
		draw.Draw(sheet, image.Rect(i*cell, 0, (i+1)*cell, cell), img, image.Point{}, draw.Over)
	}

	if dir := os.Getenv("REEL_PREVIEW_DIR"); dir != "" {
		f, err := os.Create(filepath.Join(dir, os.Getenv("REEL_PREVIEW_NAME")))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err = png.Encode(f, sheet); err != nil {
			t.Fatal(err)
		}
	}
}
