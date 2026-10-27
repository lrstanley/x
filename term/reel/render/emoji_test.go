// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"image"
	"image/color"
	"testing"

	idraw "github.com/lrstanley/x/term/reel/render/internal/draw"
	"github.com/lrstanley/x/term/reel/render/internal/testutil"
	"github.com/lrstanley/x/term/reel/vt"
)

var emojiRed = color.NRGBA{R: 255, A: 255}

// withFakeEmoji makes "😀" the red glyph of an installed color font, and nothing
// else a color glyph.
func withFakeEmoji(d *Renderer) *Renderer {
	d.fonts.SetColorFallback(func(cluster string) idraw.ColorGlyph {
		if cluster == "😀" {
			return testutil.SolidColorGlyph{R: 255, A: 255}
		}
		return nil
	})
	return d
}

func TestColorEmojiDrawnAcrossTwoCells(t *testing.T) {
	t.Parallel()

	fg, bg, palette := blockTestPalette()
	d := withFakeEmoji(MustNew(WithScale(scaleFor(16)), palette))
	scr := newTestScreen(6, 1)
	putText(scr, 0, 0, "a😀b", vt.Style{Fg: fg, Bg: bg})
	img := drawNRGBA(t, d, scr)
	snap := d.layoutSnapshot(t, image.Point{}, scr)

	// The glyph fills both cells of the emoji, and nothing around them.
	both := snap.cellBounds(1, 0).Union(snap.cellBounds(2, 0))
	for y := both.Min.Y; y < both.Max.Y; y++ {
		for x := both.Min.X; x < both.Max.X; x++ {
			if got := img.NRGBAAt(x, y); got != emojiRed {
				t.Fatalf("emoji pixel (%d,%d) = %v, want %v", x, y, got, emojiRed)
			}
		}
	}
	for _, x := range []int{4, 5} {
		if _, ink := inkBounds(img, snap.cellBounds(x, 0), bg); ink {
			t.Errorf("cell %d has ink, want an empty cell", x)
		}
	}
	// "a" and "b" are text in the foreground, not tinted by the emoji.
	for _, x := range []int{0, 3} {
		cell := snap.cellBounds(x, 0)
		if _, ink := inkBounds(img, cell, bg); !ink {
			t.Errorf("text cell %d has no ink", x)
		}
		for yy := cell.Min.Y; yy < cell.Max.Y; yy++ {
			for xx := cell.Min.X; xx < cell.Max.X; xx++ {
				if img.NRGBAAt(xx, yy) == emojiRed {
					t.Fatalf("text cell %d has an emoji colored pixel at (%d,%d)", x, xx, yy)
				}
			}
		}
	}
}

func TestColorEmojiOnlyInWideCells(t *testing.T) {
	t.Parallel()

	fg, bg, palette := blockTestPalette()
	d := withFakeEmoji(MustNew(WithScale(scaleFor(16)), palette))

	hasRed := func(img *image.NRGBA) bool {
		for y := range img.Bounds().Dy() {
			for x := range img.Bounds().Dx() {
				if img.NRGBAAt(x, y) == emojiRed {
					return true
				}
			}
		}
		return false
	}

	// The same text in a one cell wide cell is a text presentation symbol, so it
	// is drawn as text, not from the color font, even if the font has it.
	scr := newTestScreen(4, 1)
	scr.SetCell(0, 0, &vt.Cell{Content: "😀", Width: 1, Style: vt.Style{Fg: fg, Bg: bg}})
	if hasRed(drawNRGBA(t, d, scr)) {
		t.Fatal("a one cell wide emoji was drawn in color")
	}

	// Wide text that no color font has is not drawn from one.
	scr = newTestScreen(4, 1)
	putText(scr, 0, 0, "😁", vt.Style{Fg: fg, Bg: bg})
	if hasRed(drawNRGBA(t, d, scr)) {
		t.Fatal("an emoji the color font lacks was drawn with the glyph of another")
	}
}

// TestColorEmojiBundledWithoutSystemFonts makes sure the bundled emoji font
// draws color emoji when installed fonts are not used, so the output does not
// depend on the machine.
func TestColorEmojiBundledWithoutSystemFonts(t *testing.T) {
	t.Parallel()

	fg, bg, palette := blockTestPalette()
	d := MustNew(WithScale(scaleFor(16)), WithSystemFontFallback(false), palette)
	scr := newTestScreen(6, 1)
	style := vt.Style{Fg: fg, Bg: bg}
	putText(scr, 0, 0, "😀", style)
	putText(scr, 2, 0, "👋🏽", style) // Drawn in the default tone: the bundled font has no skin tones.
	img := drawNRGBA(t, d, scr)
	snap := d.layoutSnapshot(t, image.Point{}, scr)

	for _, cluster := range []string{"😀", "👋🏽"} {
		if got := d.fonts.ColorGlyph(mustCluster(t, cluster)); got == nil {
			t.Fatalf("no bundled color glyph for %q", cluster)
		}
	}

	yellow := func(cell image.Rectangle) bool {
		for y := cell.Min.Y; y < cell.Max.Y; y++ {
			for x := cell.Min.X; x < cell.Max.X; x++ {
				if c := img.NRGBAAt(x, y); c.A == 255 && c.R > 200 && c.G > 150 && c.B < 100 {
					return true
				}
			}
		}
		return false
	}
	if !yellow(snap.cellBounds(0, 0).Union(snap.cellBounds(1, 0))) {
		t.Fatal("the emoji was not drawn in color")
	}
	if !yellow(snap.cellBounds(2, 0).Union(snap.cellBounds(3, 0))) {
		t.Fatal("the skin tone sequence was not drawn in the default tone")
	}
}

func mustCluster(t *testing.T, s string) idraw.Cluster {
	t.Helper()
	c, ok := idraw.NewCluster(s)
	if !ok {
		t.Fatalf("no cluster for %q", s)
	}
	return c
}

func TestColorEmojiFaint(t *testing.T) {
	t.Parallel()

	fg, bg, palette := blockTestPalette()
	d := withFakeEmoji(MustNew(WithScale(scaleFor(16)), palette, WithFaintFactor(0.5)))
	scr := newTestScreen(4, 1)
	style := vt.Style{Fg: fg, Bg: bg, Attrs: vt.AttrFaint}
	putText(scr, 0, 0, "😀", style)
	img := drawNRGBA(t, d, scr)
	snap := d.layoutSnapshot(t, image.Point{}, scr)
	got := sample(img, snap.cellBounds(0, 0))
	// Half way from the background to red.
	if got.R < 127-30+15 || got.R > 127+30+15 || got == emojiRed || got == bg {
		t.Fatalf("faint emoji pixel = %v, want about half way between %v and %v", got, bg, emojiRed)
	}
}

func TestColorEmojiConcealed(t *testing.T) {
	t.Parallel()

	fg, bg, palette := blockTestPalette()
	d := withFakeEmoji(MustNew(WithScale(scaleFor(16)), palette))
	scr := newTestScreen(4, 1)
	putText(scr, 0, 0, "😀", vt.Style{Fg: fg, Bg: bg, Attrs: vt.AttrConceal})
	img := drawNRGBA(t, d, scr)
	snap := d.layoutSnapshot(t, image.Point{}, scr)
	both := snap.cellBounds(0, 0).Union(snap.cellBounds(1, 0))
	if _, ink := inkBounds(img, both, bg); ink {
		t.Fatal("a concealed emoji was drawn")
	}
}

// TestColorEmojiRedrawsWhenReplaced makes sure the incremental draw repaints
// both cells of an emoji that is replaced, and draws one that is added.
func TestColorEmojiRedrawsWhenReplaced(t *testing.T) {
	t.Parallel()

	fg, bg, palette := blockTestPalette()
	d := withFakeEmoji(MustNew(WithScale(scaleFor(16)), palette))
	style := vt.Style{Fg: fg, Bg: bg}
	scr := newTestScreen(6, 2)
	putText(scr, 0, 0, "😀 x", style)
	putText(scr, 0, 1, "abcdef", style)

	hasRed := func(img *image.NRGBA, snap *renderFrame, x, y int) bool {
		cell := snap.cellBounds(x, y)
		return img.NRGBAAt(cell.Min.X+cell.Dx()/2, cell.Min.Y+cell.Dy()/2) == emojiRed
	}
	snap := d.layoutSnapshot(t, image.Point{}, scr)
	img := drawNRGBA(t, d, scr)
	if !hasRed(img, snap, 0, 0) || !hasRed(img, snap, 1, 0) {
		t.Fatal("first draw: both cells of the emoji should be red")
	}

	putText(scr, 0, 0, "ab", style)
	img = drawNRGBA(t, d, scr)
	if hasRed(img, snap, 0, 0) || hasRed(img, snap, 1, 0) {
		t.Fatal("after replacing the emoji with text: red is left behind")
	}

	putText(scr, 3, 0, "😀", style)
	img = drawNRGBA(t, d, scr)
	if !hasRed(img, snap, 3, 0) || !hasRed(img, snap, 4, 0) {
		t.Fatal("after adding an emoji: both cells should be red")
	}
}
