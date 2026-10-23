// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"bytes"
	"image"
	"testing"
)

func newRenderer(t *testing.T, opts ...Option) *Renderer {
	t.Helper()
	d := MustNew(append([]Option{WithFontSizePt(10)}, opts...)...)
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestWindowChromeLayout(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(20, 5)
	plain := newRenderer(t, WithPadding(4), WithMargin(6, nil))
	d := newRenderer(t, WithWindowChrome(true), WithPadding(4), WithMargin(6, nil))

	bar := d.chromeHeight()
	if bar <= d.CellSize().Y {
		t.Fatalf("chromeHeight() = %d, want taller than a cell (%d)", bar, d.CellSize().Y)
	}
	if got, want := d.Size(scr), plain.Size(scr).Add(image.Pt(0, bar)); got != want {
		t.Fatalf("Size() = %v, want %v", got, want)
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
	a.UpdateEmulatorState(&EmulatorState{Title: "ignored"})
	b.UpdateEmulatorState(&EmulatorState{Title: "ignored"})

	if !bytes.Equal(drawNRGBA(t, a, scr).Pix, drawNRGBA(t, b, scr).Pix) {
		t.Fatal("WithWindowChrome(false) changed the output")
	}
	if got := a.Size(scr); got != image.Pt(10*a.CellSize().X, 3*a.CellSize().Y) {
		t.Fatalf("Size() = %v, want the bare grid", got)
	}
}

func TestWindowChromeTitleIsLive(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(30, 3)
	d := newRenderer(t, WithWindowChrome(true))

	d.UpdateEmulatorState(&EmulatorState{Title: "first"})
	first := bytes.Clone(drawNRGBA(t, d, scr).Pix)

	// Same buffer generation: only the title differs, and it must still repaint.
	d.UpdateEmulatorState(&EmulatorState{Title: "second"})
	second := bytes.Clone(drawNRGBA(t, d, scr).Pix)
	if bytes.Equal(first, second) {
		t.Fatal("changing the title did not change the next image")
	}

	d.UpdateEmulatorState(&EmulatorState{Title: "first"})
	if !bytes.Equal(first, drawNRGBA(t, d, scr).Pix) {
		t.Fatal("restoring the title did not restore the image")
	}
}

// TestWindowChromeTitleOnlyPaintsBar checks that the title stays in the bar
// and clips to it, however long it is.
func TestWindowChromeTitleOnlyPaintsBar(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(40, 3)
	d := newRenderer(t, WithWindowChrome(true))
	d.UpdateEmulatorState(&EmulatorState{})
	empty := bytes.Clone(drawNRGBA(t, d, scr).Pix)

	d.UpdateEmulatorState(&EmulatorState{Title: "a rather long window title  漢字 that cannot possibly fit"})
	img := drawNRGBA(t, d, scr)
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
		state *EmulatorState
	}{
		"no state":        {8, nil},
		"empty title":     {8, &EmulatorState{}},
		"escapes only":    {8, &EmulatorState{Title: "\x1b[31m\x07"}},
		"no room":         {2, &EmulatorState{Title: "title"}},
		"narrow, rounded": {1, &EmulatorState{Title: "漢字"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			scr := newTestScreen(tc.cols, 2)
			d := newRenderer(t, WithWindowChrome(true), WithBorderRadius(8))
			d.UpdateEmulatorState(tc.state)
			img := drawNRGBA(t, d, scr)

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
