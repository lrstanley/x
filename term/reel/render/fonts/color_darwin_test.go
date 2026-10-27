// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build darwin

package fonts

import (
	"bytes"
	"testing"
)

// These tests draw with the Apple Color Emoji font every macOS has in
// /System/Library/Fonts, found the same way the renderer finds it. They are the
// only ones that read the real font, which can't be bundled, so they check what
// the synthetic fixtures can't: sbix strikes, "flip" records and the morx table
// that composes flags, skin tones and ZWJ sequences.

// appleGlyph returns the system color glyph of cluster, and fails the test if
// there is none.
func appleGlyph(t *testing.T, cluster string) *ColorGlyph {
	t.Helper()
	g, ok := SystemColorGlyph(cluster)
	if !ok {
		t.Fatalf("no color glyph for %+q: is Apple Color Emoji installed in the macOS font directories (%v)?", cluster, Dirs())
	}
	return g
}

func isSbix(src colorSource) bool {
	_, ok := src.(*sbixSource)
	return ok
}

func TestAppleColorEmoji(t *testing.T) {
	t.Parallel()

	if src := appleGlyph(t, "😀").layers[0].src; !isSbix(src) {
		t.Fatalf("an emoji is drawn from %T, want the sbix bitmaps of Apple Color Emoji", src)
	}

	for _, tc := range []struct {
		name    string
		cluster string
	}{
		{"smile", "😀"},
		{"thumbs up", "👍"},
		{"heart with selector", "❤️"},
		{"skin tone", "👋🏽"},
		{"flag", "🇺🇸"},
		{"another flag", "🇯🇵"},
		{"subdivision flag", "🏴\U000E0067\U000E0062\U000E0065\U000E006E\U000E0067\U000E007F"},
		{"rainbow flag", "🏳️‍🌈"},
		{"pirate flag", "🏴‍☠️"},
		{"technologist", "🧑‍💻"},
		{"family", "👨‍👩‍👧‍👦"},
		{"couple with heart", "👩‍❤️‍👨"},
		{"couple with heart and skin tones", "👩🏻‍❤️‍👨🏾"},
		{"kiss", "👩‍❤️‍💋‍👨"},
		{"handshake with skin tones", "🫱🏻‍🫲🏿"},
		{"astronaut with skin tone", "👩🏽‍🚀"},
		{"keycap", "1️⃣"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			img := appleGlyph(t, tc.cluster).Image(64, 64)
			if img == nil {
				t.Fatal("no image")
			}
			if n := opaquePixels(img); n < 64*64/8 {
				t.Fatalf("%d of %d pixels are opaque, want a drawn glyph", n, 64*64)
			}
		})
	}

	for _, cluster := range []string{"A", "ab", "你"} {
		if g, ok := SystemColorGlyph(cluster); ok {
			t.Errorf("SystemColorGlyph(%+q) = %v, want none", cluster, g)
		}
	}
}

// TestAppleColorEmojiSequencesAreComposed makes sure sequences are drawn as the
// glyph the font composes them into, not as one of their parts.
func TestAppleColorEmojiSequencesAreComposed(t *testing.T) {
	t.Parallel()

	same := func(a, b string) bool {
		return bytes.Equal(appleGlyph(t, a).Image(64, 64).Pix, appleGlyph(t, b).Image(64, 64).Pix)
	}
	for _, tc := range []struct{ name, sequence, part string }{
		{"family and its first member", "👨‍👩‍👧‍👦", "👨"},
		{"technologist and the person", "🧑‍💻", "🧑"},
		{"rainbow flag and white flag", "🏳️‍🌈", "🏳️"},
		{"pirate flag and black flag", "🏴‍☠️", "🏴"},
		{"subdivision flag and black flag", "🏴\U000E0067\U000E0062\U000E0065\U000E006E\U000E0067\U000E007F", "🏴"},
		{"skin tone and default tone", "👋🏽", "👋"},
	} {
		if same(tc.sequence, tc.part) {
			t.Errorf("%s: drawn the same, want the composed glyph", tc.name)
		}
	}
	if same("🇺🇸", "🇯🇵") {
		t.Error("two different flags are drawn the same")
	}
	if same("👋🏻", "👋🏿") {
		t.Error("two different skin tones are drawn the same")
	}
}

// TestAppleColorEmojiHalves makes sure the emoji Apple draws as two halves, the
// second a mark placed over the first, are drawn with both.
func TestAppleColorEmojiHalves(t *testing.T) {
	t.Parallel()

	for _, cluster := range []string{"👩‍❤️‍👨", "👩🏻‍❤️‍👨🏾", "👩‍❤️‍💋‍👨"} {
		g := appleGlyph(t, cluster)
		if len(g.layers) != 2 {
			t.Errorf("%+q has %d layers, want the two halves", cluster, len(g.layers))
			continue
		}
		whole, firstHalf := g.Image(64, 64), g.layers[0].src.render(g.layers[0].gid, 64, 64)
		if bytes.Equal(whole.Pix, firstHalf.Pix) {
			t.Errorf("%+q is drawn without its second half", cluster)
		}
		if opaquePixels(whole) <= opaquePixels(firstHalf) {
			t.Errorf("%+q: the halves together cover no more than the first half", cluster)
		}
	}

	// Two emoji next to each other are not a composed glyph.
	if g, ok := SystemColorGlyph("👨👩"); ok {
		t.Errorf("two separate emoji resolve to one glyph: %v", g)
	}
}

// TestAppleColorEmojiFlip makes sure emoji Apple stores as a mirrored copy of
// another glyph (the ones facing right, new in macOS 14.2) are mirrored.
func TestAppleColorEmojiFlip(t *testing.T) {
	t.Parallel()

	right, ok := SystemColorGlyph("🏃‍➡️")
	if !ok {
		t.Skip("this macOS has no right facing runner (added in 14.2)")
	}
	plain := appleGlyph(t, "🏃").Image(64, 64)
	flipped := *right.Image(64, 64)
	flipped.Pix = bytes.Clone(flipped.Pix)
	mirror(&flipped)
	if bytes.Equal(plain.Pix, right.Image(64, 64).Pix) {
		t.Error("the right facing runner is drawn facing the same way as the plain one")
	}
	if !bytes.Equal(plain.Pix, flipped.Pix) {
		t.Error("the right facing runner is not the plain runner mirrored")
	}
}
