// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"bytes"
	"image"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel/vt"
)

// bellAt is when the BEL in bellState arrives.
const bellAt = 5 * time.Second

// bellState is a focused state with a title, rung at bellAt.
func bellState() (rung, quiet vt.State) {
	quiet = vt.State{Focused: true, Title: "title"}
	rung = quiet
	rung.Bell = vt.Bell{Count: 1, At: bellAt}
	return rung, quiet
}

func bellScreen() testScreen {
	scr := newTestScreen(30, 3)
	putText(scr, 0, 0, "ring", vt.Style{})
	return scr
}

func TestBellOffByDefault(t *testing.T) {
	t.Parallel()

	scr := bellScreen()
	d := newRenderer(t, WithWindowChrome(true))
	rung, quiet := bellState()
	if !bytes.Equal(drawFresh(d, scr, rung, bellAt).Pix, drawFresh(d, scr, quiet, bellAt).Pix) {
		t.Fatal("a BEL changed the image without WithBell")
	}
	if d.Animates(rung) {
		t.Fatal("Animates() = true for a BEL without WithBell")
	}
}

// TestBellFlash checks that the window flashes at the BEL, fades over
// bellFlash, and is gone after it.
func TestBellFlash(t *testing.T) {
	t.Parallel()

	scr := bellScreen()
	d := newRenderer(t, WithBell(true))
	rung, quiet := bellState()
	plain := drawFresh(d, scr, quiet, bellAt).Pix

	at := drawFresh(d, scr, rung, bellAt)
	half := drawFresh(d, scr, rung, bellAt+bellFlash/2)
	if bytes.Equal(at.Pix, plain) || bytes.Equal(half.Pix, plain) || bytes.Equal(at.Pix, half.Pix) {
		t.Fatal("the flash did not show, or did not fade")
	}
	if !bytes.Equal(drawFresh(d, scr, rung, bellAt+bellFlash).Pix, plain) {
		t.Fatal("the flash outlasted bellFlash")
	}
	if !bytes.Equal(drawFresh(d, scr, rung, bellAt-time.Millisecond).Pix, plain) {
		t.Fatal("the flash showed before the BEL")
	}

	// The flash blends toward the foreground, so the background lightens.
	win := d.layoutSnapshot(t, image.Point{}, scr).windowBounds
	p := win.Min.Add(image.Pt(win.Dx()/2, win.Dy()-1))
	if got, was := at.NRGBAAt(p.X, p.Y), drawFresh(d, scr, quiet, bellAt).NRGBAAt(p.X, p.Y); got.G <= was.G {
		t.Fatalf("flash pixel = %v, want lighter than %v", got, was)
	}
}

// TestBellTitle checks that with window chrome, the title bar shows the bell
// for WithBellDuration after the BEL, and the flash does not.
func TestBellTitle(t *testing.T) {
	t.Parallel()

	scr := bellScreen()
	d := newRenderer(t, WithBell(true), WithWindowChrome(true), WithBellDuration(time.Second))
	rung, quiet := bellState()
	plain := drawFresh(d, scr, quiet, bellAt).Pix

	shown := drawFresh(d, scr, rung, bellAt+bellFlash)
	if bytes.Equal(shown.Pix, plain) {
		t.Fatal("the title bar did not show the bell")
	}
	bar := d.layoutSnapshot(t, image.Point{}, scr).chromeBounds
	if !sameOutside(shown, drawFresh(d, scr, quiet, bellAt), bar) {
		t.Fatal("the bell in the title bar changed pixels outside it")
	}
	if !bytes.Equal(drawFresh(d, scr, rung, bellAt+time.Second).Pix, plain) {
		t.Fatal("the bell outlasted WithBellDuration")
	}
}

// TestBellRepaint checks that a frame drawn at the BEL and held repaints to
// what a full draw gives at each later offset, and that Changes and Animates
// follow the bell.
func TestBellRepaint(t *testing.T) {
	t.Parallel()

	scr := bellScreen()
	d := newRenderer(t, WithBell(true), WithWindowChrome(true), WithBellDuration(time.Second))
	rung, _ := bellState()
	if !d.Animates(rung) {
		t.Fatal("Animates() = false for a BEL")
	}
	base := drawAt(t, d, scr, rung, bellAt)
	for _, later := range []time.Duration{bellFlash / 3, bellFlash, time.Second, 3 * time.Second} {
		got := image.NewNRGBA(base.Rect)
		copy(got.Pix, base.Pix)
		d.Repaint(got, scr, rung, bellAt, bellAt+later)
		if !bytes.Equal(got.Pix, drawFresh(d, scr, rung, bellAt+later).Pix) {
			t.Errorf("repainted at +%v differs from a full draw", later)
		}
	}

	for _, tc := range []struct {
		from, to time.Duration
		want     bool
	}{
		{0, time.Millisecond, false},                         // one flash step
		{0, bellFlash / 2, true},                             // the flash faded
		{bellFlash, bellFlash + 100*time.Millisecond, false}, // only the title shows
		{bellFlash, time.Second, true},                       // the title cleared
		{time.Second, time.Hour, false},                      // nothing left
	} {
		if got := d.Changes(rung, bellAt+tc.from, bellAt+tc.to); got != tc.want {
			t.Errorf("Changes(+%v, +%v) = %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

// sameOutside reports whether a and b match everywhere outside r.
func sameOutside(a, b *image.NRGBA, r image.Rectangle) bool {
	for y := a.Rect.Min.Y; y < a.Rect.Max.Y; y++ {
		for x := a.Rect.Min.X; x < a.Rect.Max.X; x++ {
			if !image.Pt(x, y).In(r) && a.NRGBAAt(x, y) != b.NRGBAAt(x, y) {
				return false
			}
		}
	}
	return true
}
