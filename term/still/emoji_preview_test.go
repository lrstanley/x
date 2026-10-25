// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lrstanley/x/term/still/export"
	"github.com/lrstanley/x/term/vt/screen"
)

// TestColorEmojiGIF writes an animation of the kinds of emoji drawn through the
// renderer, one row at a time, to STILL_PREVIEW_GIF when it is set.
func TestColorEmojiGIF(t *testing.T) {
	path := os.Getenv("STILL_PREVIEW_GIF")
	if path == "" {
		t.Skip("STILL_PREVIEW_GIF is not set")
	}
	fg, bg, palette := blockTestPalette()
	d := MustNew(WithScale(scaleFor(24)), palette)
	style := screen.Style{Fg: fg, Bg: bg}
	dim := style
	dim.Fg = color.NRGBA{R: 166, G: 173, B: 200, A: 255}
	faint := style
	faint.Attrs |= screen.AttrFaint
	onBlue := screen.Style{Fg: fg, Bg: color.NRGBA{R: 49, G: 50, B: 90, A: 255}}

	rows := []struct {
		label, text string
		style       screen.Style
	}{
		{"single", "😀 🚀 🔥 ✅ 🎉 🦄 🍎 🐙 🌈", style},
		{"flags", "🇺🇸 🇯🇵 🇧🇷 🇩🇪 🇰🇷 🏴\U000E0067\U000E0062\U000E0065\U000E006E\U000E0067\U000E007F 🏳️‍🌈 🏴‍☠️", style},
		{"zwj", "🧑‍💻 👩‍🚀 👨‍👩‍👧‍👦 👩‍❤️‍👨 🧑‍🍳 🐻‍❄️", style},
		{"skin tones", "👋🏽 👍🏿 🙏🏻 💪🏾 (default tone)", style},
		{"vs16/keycap", "❤️ ⭐ ☀️ 1️⃣ #️⃣ ✌️", style},
		{"text stays", "❤ ✓ ★ ☀ and CJK 你好", style},
		{"faint", "😀 🚀 🇺🇸 🦄", faint},
		{"on a bg", "😀 🚀 🇺🇸 🦄", onBlue},
	}

	const labelW = 13
	scr := newTestScreen(labelW+34, len(rows)+2)
	putText(scr, 0, 0, "color emoji, bundled Noto subset", style)

	ch := make(chan export.Frame)
	rec, err := export.GIF(export.WithChannel(ch), export.WithLoopCount(0))
	if err != nil {
		t.Fatal(err)
	}
	ch <- export.Frame{Image: drawIntoNRGBA(d, scr), Delay: 700 * time.Millisecond}
	for i, row := range rows {
		putText(scr, 0, i+2, row.label, dim)
		putText(scr, labelW, i+2, row.text, row.style)
		delay := 900 * time.Millisecond
		if i == len(rows)-1 {
			delay = 4 * time.Second
		}
		ch <- export.Frame{Image: drawIntoNRGBA(d, scr), Delay: delay}
	}
	close(ch)
	if err = rec.Close(); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = rec.EncodeAll(f); err != nil {
		t.Fatal(err)
	}
}

// TestColorEmojiPreview writes a picture of emoji drawn through the renderer,
// when STILL_PREVIEW_DIR and STILL_PREVIEW_NAME are set. It needs a color font
// to be installed (see STILL_COLOR_FONT_DIR in the fonts package tests).
func TestColorEmojiPreview(t *testing.T) {
	dir, name := os.Getenv("STILL_PREVIEW_DIR"), os.Getenv("STILL_PREVIEW_NAME")
	if dir == "" || name == "" {
		t.Skip("STILL_PREVIEW_DIR and STILL_PREVIEW_NAME are not set")
	}
	fg, bg, palette := blockTestPalette()
	d := MustNew(WithScale(scaleFor(18)), palette)
	scr := newTestScreen(34, 5)
	style := screen.Style{Fg: fg, Bg: bg}
	putText(scr, 0, 0, "plain text then 😀 👍 👋🏽 done", style)
	putText(scr, 0, 1, "🇺🇸 🏴\U000E0067\U000E0062\U000E0065\U000E006E\U000E0067\U000E007F ❤️ 1️⃣ 🧑‍💻 👨‍👩‍👧‍👦 🏳️‍🌈", style)
	putText(scr, 0, 2, "text ❤ and ✓ stay text, CJK 你好 too", style)
	putText(scr, 0, 3, "faint: ", style)
	faint := style
	faint.Attrs |= screen.AttrFaint
	putText(scr, 7, 3, "😀 👍 🇺🇸", faint)
	putText(scr, 0, 4, "😀😀😀😀😀😀😀😀😀😀😀😀😀😀😀😀😀", style)

	img := drawNRGBA(t, d, scr)
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
