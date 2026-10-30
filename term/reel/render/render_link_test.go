// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"github.com/lrstanley/x/term/reel/vt"
)

func TestRendererLinkUnderline(t *testing.T) {
	t.Parallel()

	fg := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	bg := color.NRGBA{A: 0xff}
	palette := WithPalette(Palette{DefaultForeground: fg, DefaultBackground: bg})
	link := vt.Link{URL: "https://example.com"}

	// underlined reports whether every pixel of cell 0's underline row is fg.
	underlined := func(t *testing.T, d *Renderer, scr vt.Screen) bool {
		t.Helper()
		img := drawNRGBA(t, d, scr)
		ctx := d.layoutSnapshot(t, image.Point{}, scr)
		area := ctx.cellBounds(0, 0)
		y := area.Min.Y + ctx.Metrics().UnderlinePosition.Int()
		for x := area.Min.X; x < area.Max.X; x++ {
			if img.NRGBAAt(x, y) != fg {
				return false
			}
		}
		return true
	}
	linked := func(style vt.Style, l vt.Link) vt.Screen {
		scr := newTestScreen(2, 1)
		scr.SetCell(0, 0, &vt.Cell{Content: " ", Width: 1, Style: style, Link: l})
		return scr
	}

	if underlined(t, MustNew(palette), linked(vt.Style{}, link)) {
		t.Error("link underlined by default")
	}
	on := MustNew(palette, WithLinkUnderline(true))
	if !underlined(t, on, linked(vt.Style{}, link)) {
		t.Error("link not underlined with WithLinkUnderline")
	}
	if underlined(t, on, linked(vt.Style{}, vt.Link{})) {
		t.Error("cell without a link underlined")
	}

	// A link with its own underline keeps that style.
	dotted := vt.Style{Underline: vt.UnderlineDotted}
	got := drawNRGBA(t, on, linked(dotted, link))
	want := drawNRGBA(t, MustNew(palette), linked(dotted, vt.Link{}))
	if !bytes.Equal(got.Pix, want.Pix) {
		t.Error("link with a dotted underline drawn differently from the same cell without a link")
	}
}
