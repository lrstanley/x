// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"bytes"
	"image"
	"strings"
	"testing"
	"unicode"
	"unsafe"

	"github.com/lrstanley/x/term/reel/vt"
)

func newRenderer(t *testing.T, opts ...Option) *Renderer {
	t.Helper()
	d := MustNew(append([]Option{WithScale(scaleFor(10))}, opts...)...)
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestWindowChromeLayout(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(20, 5)
	plain := newRenderer(t, WithPadding(4), WithMargin(6, nil))
	d := newRenderer(t, WithWindowChrome(true), WithPadding(4), WithMargin(6, nil))

	bar := d.chromeHeight()
	if cell := d.Layout(20, 5).Cell; bar <= cell.Y {
		t.Fatalf("chromeHeight() = %d, want taller than a cell (%d)", bar, cell.Y)
	}
	if got, want := d.Layout(20, 5).Image.Size(), plain.Layout(20, 5).Image.Size().Add(image.Pt(0, bar)); got != want {
		t.Fatalf("Layout().Image size = %v, want %v", got, want)
	}

	f := d.layoutSnapshot(t, image.Point{}, scr)
	if got := f.chromeBounds; got.Min != f.windowBounds.Min || got.Dx() != f.windowBounds.Dx() || got.Dy() != bar {
		t.Errorf("chromeBounds = %v, want top %dpx of window %v", got, bar, f.windowBounds)
	}
	if got, want := f.gridBounds.Min.Y, f.chromeBounds.Max.Y+4; got != want {
		t.Errorf("grid top = %d, want %d (below bar plus padding)", got, want)
	}
}

// TestWindowChromeOffMatchesDefault guards the "opt-in" requirement: the
// option's zero value must leave pixels as they were, whatever the title.
func TestWindowChromeOffMatchesDefault(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(10, 3)
	a, b := newRenderer(t), newRenderer(t, WithWindowChrome(false))
	st := vt.State{Title: "ignored"}

	if !bytes.Equal(drawAt(t, a, scr, st, 0).Pix, drawAt(t, b, scr, st, 0).Pix) {
		t.Fatal("WithWindowChrome(false) changed the output")
	}
	if l := a.Layout(10, 3); l.Image != l.Grid {
		t.Fatalf("Layout() = %+v, want the bare grid", l)
	}
}

func TestWindowChromeTitleIsLive(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(30, 3)
	d := newRenderer(t, WithWindowChrome(true))

	first := drawAt(t, d, scr, vt.State{Title: "first"}, 0).Pix

	// Same buffer generation: only the title differs, and it must still repaint.
	second := drawAt(t, d, scr, vt.State{Title: "second"}, 0).Pix
	if bytes.Equal(first, second) {
		t.Fatal("changing the title did not change the next image")
	}

	if !bytes.Equal(first, drawAt(t, d, scr, vt.State{Title: "first"}, 0).Pix) {
		t.Fatal("restoring the title did not restore the image")
	}
}

// TestWindowChromeTitleOnlyPaintsBar checks that the title stays in the bar
// and clips to it, however long it is.
func TestWindowChromeTitleOnlyPaintsBar(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(40, 3)
	d := newRenderer(t, WithWindowChrome(true))
	empty := drawAt(t, d, scr, vt.State{}, 0).Pix

	img := drawAt(t, d, scr, vt.State{Title: "a rather long window title  漢字 that cannot possibly fit"}, 0)
	bar := d.layoutSnapshot(t, image.Point{}, scr).chromeBounds

	below := bar.Max.Y * img.Stride
	if !bytes.Equal(img.Pix[below:], empty[below:]) {
		t.Fatal("title painted below the title bar")
	}
	if bytes.Equal(img.Pix, empty) {
		t.Fatal("title painted nothing")
	}
}

// TestWindowChromeNoTitle checks that the bar still lays out when there is
// no title to draw, or no room for it.
func TestWindowChromeNoTitle(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		cols  int
		state vt.State
	}{
		"empty title":     {8, vt.State{}},
		"escapes only":    {8, vt.State{Title: "\x1b[31m\x07"}},
		"no room":         {2, vt.State{Title: "title"}},
		"narrow, rounded": {1, vt.State{Title: "漢字"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			scr := newTestScreen(tc.cols, 2)
			d := newRenderer(t, WithWindowChrome(true), WithBorderRadius(8))
			img := drawAt(t, d, scr, tc.state, 0)

			bar := d.layoutSnapshot(t, image.Point{}, scr).chromeBounds
			if bar.Empty() {
				t.Fatal("bar collapsed")
			}
			x := bar.Max.X / 2
			if hairline, fill := img.NRGBAAt(x, bar.Max.Y-1), img.NRGBAAt(x, bar.Min.Y+2); hairline == fill {
				t.Errorf("hairline %v matches bar fill %v", hairline, fill)
			}
		})
	}
}

// TestWindowChromeFonts checks that the smaller title bar faces are loaded
// only with window chrome, and closed with the renderer.
func TestWindowChromeFonts(t *testing.T) {
	t.Parallel()

	if d := newRenderer(t); d.chromeCtx.fonts != nil {
		t.Fatal("chrome fonts loaded without window chrome")
	}
	d := MustNew(WithScale(scaleFor(10)), WithWindowChrome(true))
	if d.chromeCtx.fonts == nil {
		t.Fatal("chrome fonts not loaded with window chrome")
	}
	if cw, _ := d.chromeCtx.cell(); cw >= d.Layout(1, 1).Cell.X {
		t.Errorf("chrome cell width %d, want narrower than the grid's %d", cw, d.Layout(1, 1).Cell.X)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if d.chromeCtx.fonts != nil {
		t.Fatal("Close kept the chrome fonts")
	}
}

// TestWindowChromeTitleCentered checks that the title is smaller than grid
// text and that its cap height is centered on the window buttons.
func TestWindowChromeTitleCentered(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(30, 2)
	scr.SetCell(0, 0, &vt.Cell{Content: "H", Width: 1})
	d := newRenderer(t, WithWindowChrome(true), WithScale(scaleFor(16)))
	empty := drawAt(t, d, scr, vt.State{}, 0).Pix
	img := drawAt(t, d, scr, vt.State{Title: "HHHH"}, 0)
	f := d.layoutSnapshot(t, image.Point{}, scr)

	bar := f.chromeBounds
	ink := image.Rectangle{Min: bar.Max, Max: bar.Min}
	for y := bar.Min.Y; y < bar.Max.Y-1; y++ {
		for x := bar.Min.X + bar.Dx()/4; x < bar.Max.X-bar.Dx()/4; x++ {
			if i := img.PixOffset(x, y); !bytes.Equal(img.Pix[i:i+4], empty[i:i+4]) {
				ink = ink.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	if ink.Empty() {
		t.Fatal("title drew nothing")
	}
	body := bar.Dy() - 1
	diameter := body / 3
	mid := 2*(bar.Min.Y+(body-diameter)/2) + diameter // twice the button centerline
	if got := ink.Min.Y + ink.Max.Y; got < mid-2 || got > mid+2 {
		t.Errorf("title ink rows %d..%d, want centered on the buttons at %.1f", ink.Min.Y, ink.Max.Y, float64(mid)/2)
	}
	grid, ok := inkBounds(img, f.cellBounds(0, 0), sample(img, f.cellBounds(1, 1)))
	if !ok {
		t.Fatal("grid H drew nothing")
	}
	if ink.Dy() >= grid.Dy() {
		t.Errorf("title cap height %d, want smaller than the grid's %d", ink.Dy(), grid.Dy())
	}
}

func TestPlainText(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"plain title", "plain title"},
		{"漢字 ✔ ok", "漢字 ✔ ok"},
		{"\u202egnp.exe\u202c", "gnp.exe"},                         // RLO, PDF
		{"a\u202ab\u202bc\u202dd", "abcd"},                         // LRE, RLE, LRO
		{"\u2066invoice\u2069\u2067x\u2068y", "invoicexy"},         // isolates
		{"zero\u200bwidth\u200c\u200d\u200e\u200f!", "zerowidth!"}, // ZWSP, ZWNJ, ZWJ, LRM, RLM
		{"word\u2060joiner\u2061\u2062\u2063\u2064", "wordjoiner"}, // word joiner, invisible operators
		{"\ufeffbom\u00adsoft\u061calm\u180e", "bomsoftalm"},       // BOM, soft hyphen, ALM, MVS
		{"tag\U000e0041\U000e007f", "tag"},                         // tag characters
		{"cgj\u034fhf\u115f\u1160\u3164\uffa0", "cgjhf"},           // CGJ, Hangul fillers
		{"kv\u17b4\u17b5x\u2065y\ufff0z\U000e0fff", "kvxyz"},       // Khmer vowels, reserved ignorables
		{"a\x00b\x07c\x1bd\x7fe\u0085f\u009bg", "a b c d e f g"},   // C0, DEL, C1
		{"\x1b[31mred\x1b[0m", "[31mred [0m"},                      // escapes are not interpreted
		{"line\nbreak\r\nand\ttab\u2028sep\u2029para", "line break and tab sep para"},
		{"  lead  and   trail  ", "lead and trail"},
		{"trail ", "trail"},
		{"\u00a0nbsp\u3000ideographic\u2003em", "nbsp ideographic em"},
		{"a \u200b b", "a b"},
		{"a\u200b b", "a b"},
		{"bad\xffutf8\xc3", "badutf8"},
		{"non\ufffechar\U0001ffff\ufdd0", "nonchar"},
		{"<b>markup</b> &amp;", "<b>markup</b> &amp;"},
		{"\u202e\u200b\x00 \t", ""},
		{strings.Repeat("ab ", chromeTextMax), strings.TrimSpace(strings.Repeat("ab ", chromeTextMax)[:chromeTextMax])},
		{strings.Repeat("\u200b", 2*chromeTextMax) + "x", ""},
	} {
		got := plainText(tc.in)
		if got != tc.want {
			t.Errorf("plainText(%q) = %q, want %q", tc.in, got, tc.want)
		}
		for _, r := range got {
			if unicode.Is(unicode.Cf, r) || unicode.IsControl(r) || (r != ' ' && unicode.IsSpace(r)) {
				t.Errorf("plainText(%q) kept %U", tc.in, r)
			}
		}
		if strings.Contains(got, "  ") || strings.TrimSpace(got) != got {
			t.Errorf("plainText(%q) = %q has stray spaces", tc.in, got)
		}
		if got == tc.in && got != "" && unsafe.StringData(got) != unsafe.StringData(tc.in) {
			t.Errorf("plainText(%q) copied text it returns as is", tc.in)
		}
	}
}

// TestWindowChromeTitlePlain checks that the window title, which is as
// untrusted as program status text, draws like its visible text.
func TestWindowChromeTitlePlain(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(30, 2)
	d := newRenderer(t, WithWindowChrome(true))
	draw := func(title string) []byte {
		return drawFresh(d, scr, vt.State{Title: title}, 0).Pix
	}
	if !bytes.Equal(draw("Run \u202egnp.exe\u202c\x1b[0m now"), draw("Run gnp.exe [0m now")) {
		t.Fatal("a hostile window title draws differently from its visible text")
	}
}

func TestChromeDirectory(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"file:///tmp", "/tmp"},
		{"file://host/home/me/my%20dir", "/home/me/my dir"},
		{"file://host/a?q=1#frag", "/a"},
		{"file://host/bad%zz", "/bad%zz"},
		{"file://host", ""},
		{"kitty-shell-cwd://host/x y/100%", "/x y/100%"},
		{"/plain/path", "/plain/path"},
		{"relative/path", ""},
		{"https://example.com/x", ""},
	} {
		if got := chromeDirectory(tc.in); got != tc.want {
			t.Errorf("chromeDirectory(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFitTextLeft(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		in    string
		cells int
		want  string
	}{
		{"/a/b", 4, "/a/b"},
		{"/home/me/src", 6, "…e/src"},
		{"/漢字/x", 4, "…/x"},
		{"/abc", 1, ""},
		{"/abc", 0, ""},
		{"/abc", -2, ""},
	} {
		got, w := fitTextLeft(tc.in, tc.cells)
		if got != tc.want || w > max(tc.cells, 0) {
			t.Errorf("fitTextLeft(%q, %d) = %q (%d cells), want %q", tc.in, tc.cells, got, w, tc.want)
		}
	}
}

// TestWindowChromeTitleLabel checks how the title and working directory
// share the title bar.
func TestWindowChromeTitleLabel(t *testing.T) {
	t.Parallel()

	d := newRenderer(t, WithWindowChrome(true))
	f := d.layoutSnapshot(t, image.Point{}, newTestScreen(4, 2))
	const dir = "file://host/home/me/src/project"
	for _, tc := range []struct {
		name  string
		st    vt.State
		cells int
		want  string
	}{
		{"title only", vt.State{Title: "vim"}, 40, "vim"},
		{"directory only", vt.State{WorkingDirectory: dir}, 40, "/home/me/src/project"},
		{"directory only, trimmed", vt.State{WorkingDirectory: dir}, 10, "…c/project"},
		{"both", vt.State{Title: "vim", WorkingDirectory: dir}, 40, "vim — /home/me/src/project"},
		{"both, trimmed", vt.State{Title: "vim", WorkingDirectory: dir}, 18, "vim — …src/project"},
		{"both, no room", vt.State{Title: "vim", WorkingDirectory: dir}, 14, "vim"},
		{"hostile directory", vt.State{Title: "t", WorkingDirectory: "file:///a%1b%5b31mb%e2%80%aec"}, 40, "t — /a [31mbc"},
	} {
		f.st = tc.st
		spans, cells := f.titleLabel(nil, tc.cells)
		var got strings.Builder
		for _, s := range spans {
			got.WriteString(s.text)
		}
		if got.String() != tc.want || cells > tc.cells {
			t.Errorf("%s: titleLabel(%d) = %q (%d cells), want %q", tc.name, tc.cells, got.String(), cells, tc.want)
		}
	}
}

// TestWindowChromeWorkingDirectoryIsLive checks that a new working
// directory repaints the title bar of a screen that did not change.
func TestWindowChromeWorkingDirectoryIsLive(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(40, 3)
	d := newRenderer(t, WithWindowChrome(true))

	first := drawAt(t, d, scr, vt.State{Title: "t", WorkingDirectory: "file:///first"}, 0).Pix
	if bytes.Equal(first, drawAt(t, d, scr, vt.State{Title: "t", WorkingDirectory: "file:///second"}, 0).Pix) {
		t.Fatal("changing the working directory did not change the next image")
	}
	if !bytes.Equal(first, drawAt(t, d, scr, vt.State{Title: "t", WorkingDirectory: "file:///first"}, 0).Pix) {
		t.Fatal("restoring the working directory did not restore the image")
	}
	if !bytes.Equal(drawFresh(d, scr, vt.State{Title: "t", WorkingDirectory: "file:///first"}, 0).Pix, first) {
		t.Fatal("an incremental working directory change differs from a full draw")
	}
}
