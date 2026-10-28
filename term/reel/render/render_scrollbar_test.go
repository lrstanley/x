// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"image"
	"testing"

	"github.com/lrstanley/x/term/reel/vt"
)

func TestRendererScrollbarThumbPinnedToBottom(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(1, 4)
	d := MustNew(WithScrollbar(true))
	img := drawAt(t, d, scr, vt.State{Focused: true, ScrollbackLen: 30}, 0)
	ctx := d.layoutSnapshot(t, image.Point{}, scr)
	sb := ctx.ScrollbarBounds()
	top := img.NRGBAAt(sb.Min.X, sb.Min.Y)
	bottom := img.NRGBAAt(sb.Min.X, sb.Max.Y-1)

	if top == bottom {
		t.Fatalf("scrollbar track and thumb matched: %#v", top)
	}
	if bottom.R <= top.R {
		t.Fatalf("scrollbar bottom thumb = %#v, want brighter than track %#v", bottom, top)
	}
}

func TestRendererScrollbarThumbFollowsOffset(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(1, 4)
	d := MustNew(WithScrollbar(true))
	ctx := d.layoutSnapshot(t, image.Point{}, scr)
	sb := ctx.ScrollbarBounds()

	thumbRows := func(offset int) (first, last int) {
		img := drawAt(t, d, scr, vt.State{Focused: true, ScrollbackLen: 30, ScrollbackOffset: offset}, 0)
		// The track is the dimmer of the two colors in the column.
		track := img.NRGBAAt(sb.Min.X, sb.Min.Y)
		for y := sb.Min.Y; y < sb.Max.Y; y++ {
			track.R = min(track.R, img.NRGBAAt(sb.Min.X, y).R)
		}
		first, last = -1, -1
		for y := sb.Min.Y; y < sb.Max.Y; y++ {
			if img.NRGBAAt(sb.Min.X, y).R > track.R {
				if first < 0 {
					first = y
				}
				last = y
			}
		}
		return first, last
	}

	bottomFirst, bottomLast := thumbRows(0)
	topFirst, topLast := thumbRows(30)
	midFirst, _ := thumbRows(15)

	if bottomLast != sb.Max.Y-1 || topFirst != sb.Min.Y {
		t.Fatalf("thumb at bottom ends at %d (track %v), at top starts at %d", bottomLast, sb, topFirst)
	}
	if bottomLast-bottomFirst != topLast-topFirst {
		t.Fatalf("thumb size changed: %d..%d vs %d..%d", bottomFirst, bottomLast, topFirst, topLast)
	}
	if midFirst <= topFirst || midFirst >= bottomFirst {
		t.Fatalf("thumb at the middle starts at %d, want between %d and %d", midFirst, topFirst, bottomFirst)
	}
}

func TestRendererScrollbarWidth(t *testing.T) {
	t.Parallel()

	if got := MustNew().scrollbarWidth(); got != 0 {
		t.Fatalf("width without a scrollbar = %d, want 0", got)
	}

	small := MustNew(WithScrollbar(true), WithScale(0.5))
	large := MustNew(WithScrollbar(true), WithScale(4))
	if got := small.scrollbarWidth(); got != minScrollbarWidthPx {
		t.Fatalf("width at a small scale = %d, want the minimum %d", got, minScrollbarWidthPx)
	}
	if got, want := large.scrollbarWidth(), large.metrics.CellSize().X/3; got != want || got <= small.scrollbarWidth() {
		t.Fatalf("width at a large scale = %d, want %d, wider than %d", got, want, small.scrollbarWidth())
	}

	d := MustNew(WithScrollbar(true), WithScrollbarWidth(7))
	if got := d.scrollbarWidth(); got != 7 {
		t.Fatalf("explicit width = %d, want 7", got)
	}
	scr := newTestScreen(2, 2)
	ctx := d.layoutSnapshot(t, image.Point{}, scr)
	if got := ctx.ScrollbarBounds().Dx(); got != 7 {
		t.Fatalf("scrollbar bounds width = %d, want 7", got)
	}
	if got, want := d.Layout(2, 2).Image.Dx(), 2*d.Layout(2, 2).Cell.X+7; got != want {
		t.Fatalf("image width = %d, want %d", got, want)
	}

	if _, err := New(WithScrollbarWidth(-1)); err == nil {
		t.Fatal("WithScrollbarWidth(-1) succeeded")
	}
}

func TestRendererScrollbarShownWithShortScrollback(t *testing.T) {
	t.Parallel()

	scr := newTestScreen(1, 8)
	d := MustNew(WithScrollbar(true))
	ctx := d.layoutSnapshot(t, image.Point{}, scr)
	sb := ctx.ScrollbarBounds()

	for _, tc := range []struct {
		name string
		st   vt.State
		want bool
	}{
		{"no scrollback", vt.State{Focused: true}, false},
		{"fewer rows than the screen", vt.State{Focused: true, ScrollbackLen: 3}, true},
		{"alternate screen", vt.State{Focused: true, ScrollbackLen: 3, AltScreen: true}, false},
	} {
		img := drawAt(t, d, scr, tc.st, 0)
		if got := img.NRGBAAt(sb.Min.X, sb.Min.Y) != img.NRGBAAt(sb.Min.X-sb.Dx(), sb.Min.Y); got != tc.want {
			t.Errorf("%s: scrollbar drawn = %v, want %v", tc.name, got, tc.want)
		}
	}
}
