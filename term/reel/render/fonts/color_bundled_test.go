// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fonts

import (
	"image/color"
	"testing"
)

func TestBundledColorGlyph(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		cluster string
		want    bool
	}{
		{"smile", "😀", true},
		{"rocket", "🚀", true},
		{"heart vs16", "❤️", true},
		{"flag us", "🇺🇸", true},
		{"flag england", "🏴\U000E0067\U000E0062\U000E0065\U000E006E\U000E0067\U000E007F", true},
		{"keycap", "1️⃣", true},
		{"technologist zwj", "🧑‍💻", true},
		{"family zwj", "👨‍👩‍👧‍👦", true},
		{"rainbow flag zwj", "🏳️‍🌈", true},
		{"skin tone uses the default tone", "👋🏽", true},
		{"skin tone zwj uses the default tone", "👩🏽‍🚀", true},
		{"skin tone modifier alone", "🏽", false},
		{"letter", "A", false},
		{"two letters", "ab", false},
		{"cjk", "你", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g, ok := BundledColorGlyph(tc.cluster)
			if ok != tc.want || (g != nil) != tc.want {
				t.Fatalf("BundledColorGlyph(%+q) = %v, %v; want found = %v", tc.cluster, g, ok, tc.want)
			}
			if !ok {
				return
			}
			img := g.Image(48, 48)
			if img == nil {
				t.Fatal("no image")
			}
			if img.Bounds().Dx() != 48 || img.Bounds().Dy() != 48 {
				t.Fatalf("image is %v, want 48x48", img.Bounds())
			}
			if n := opaquePixels(img); n < 48*48/8 {
				t.Fatalf("only %d of %d pixels are opaque, want a drawn glyph", n, 48*48)
			}
		})
	}
}

// TestBundledColorGlyphColors makes sure the glyphs are color, not a mask.
func TestBundledColorGlyphColors(t *testing.T) {
	t.Parallel()

	g, ok := BundledColorGlyph("😀")
	if !ok {
		t.Fatal("no glyph")
	}
	img := g.Image(64, 64)
	c := img.RGBAAt(32, 12) // The face, near the top.
	if c.A != 255 || c.R < 200 || c.B > 100 {
		t.Fatalf("face pixel = %v, want an opaque yellow", c)
	}
	if got := img.RGBAAt(0, 0); got != (color.RGBA{}) {
		t.Fatalf("corner pixel = %v, want transparent", got)
	}
}

// TestBundledColorFontNotAFamily makes sure the emoji font does not show up as
// a family: it has no text glyphs to draw.
func TestBundledColorFontNotAFamily(t *testing.T) {
	t.Parallel()

	for key, faces := range embeddedFamilyIndex().faces {
		for _, f := range faces {
			if f.src == "embed:data/"+bundledColorFile {
				t.Fatalf("the bundled color emoji font is indexed as family %q", key)
			}
		}
	}
}
