// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"image"
	"testing"
)

// TestSymbolGlyphsRenderFromFonts covers symbols that sit in the default
// symbol font's ranges without being in that font. Each must be drawn from a
// face that has it (the primary or the bundled fallback), so every glyph gets
// its own ink rather than a shared .notdef box.
func TestSymbolGlyphsRenderFromFonts(t *testing.T) {
	t.Parallel()

	fg, bg, palette := blockTestPalette()
	glyphs := []string{
		"■", "●", "▶", "✓", "⌘", "⚠", // Primary font.
		"★", "✔", "☀", "♪", "⚙", "◐", "◢", "⇄", "➤", "☐", // Bundled fallback.
		"\ufffd",
	}
	d := MustNew(WithScale(scaleFor(16)), WithSystemFontFallback(false), palette)
	seen := map[string]string{}
	for _, glyph := range glyphs {
		scr := blockRow(glyph, 1, fg, bg)
		img := drawNRGBA(t, d, scr)
		cell := d.layoutSnapshot(t, image.Point{}, scr).cellBounds(0, 0)
		if _, ok := inkBounds(img, cell, bg); !ok {
			t.Errorf("%q: no ink", glyph)
			continue
		}
		sig := cellInkSignature(img, cell, bg)
		if other, dup := seen[sig]; dup {
			t.Errorf("%q renders identically to %q (a .notdef box?)", glyph, other)
		}
		seen[sig] = glyph
	}
}

func TestFilledSquareIsSolid(t *testing.T) {
	t.Parallel()

	fg, bg, palette := blockTestPalette()
	d := MustNew(WithScale(scaleFor(16)), WithSystemFontFallback(false), palette)
	scr := blockRow("■", 1, fg, bg)
	img := drawNRGBA(t, d, scr)
	cell := d.layoutSnapshot(t, image.Point{}, scr).cellBounds(0, 0)
	ink, ok := inkBounds(img, cell, bg)
	if !ok {
		t.Fatal("■ has no ink")
	}
	filled := 0
	for y := ink.Min.Y; y < ink.Max.Y; y++ {
		for x := ink.Min.X; x < ink.Max.X; x++ {
			if img.NRGBAAt(x, y) == fg {
				filled++
			}
		}
	}
	if area := ink.Dx() * ink.Dy(); filled*10 < area*7 {
		t.Fatalf("■ fills %d of %d ink pixels, want a solid square, not a hollow box", filled, area)
	}
}
