// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"bytes"
	"slices"
	"testing"
	"time"
)

// TestMouse checks that a cast with mouse input draws the mouse by
// default, moving for mouseSettle after its last event and then holding
// still, and that the player's image is left alone.
func TestMouse(t *testing.T) {
	cast := `{"version":2,"width":20,"height":4}
[0.1, "o", "\u001b[?1000h\u001b[?1006hclick me"]
[0.5, "i", "\u001b[<0;5;2M"]
[0.6, "i", "\u001b[<0;5;2m"]
[3.0, "o", " done"]
`
	x, err := load(t.Context(), []byte(cast), 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = x.player.Close() })
	if !x.displayFor.Mouse || x.displayReason["mouse"] == "" {
		t.Errorf("mouse = %v (%q), want on for the mouse input", x.displayFor.Mouse, x.displayReason["mouse"])
	}
	if want := [][2]float64{{500, 600 + ms(mouseSettle)}}; !slices.Equal(x.mouseSpans, want) {
		t.Errorf("mouse spans = %v, want %v", x.mouseSpans, want)
	}
	if err = x.seek(2); err != nil {
		t.Fatal(err)
	}
	img, err := x.player.Image()
	if err != nil {
		t.Fatal(err)
	}
	bare := bytes.Clone(img.Pix)
	release := 600 * time.Millisecond
	for _, tc := range []struct {
		at    time.Duration
		drawn bool
	}{
		{release, true},
		{release + mouseSettle - 50*time.Millisecond, true},
		{release + mouseSettle, false},
		{release + mouseSettle + time.Second, false},
	} {
		if got := !bytes.Equal(x.withMouse(img, tc.at).Pix, bare); got != tc.drawn {
			t.Errorf("mouse drawn at %v = %v, want %v", tc.at, got, tc.drawn)
		}
	}
	if !bytes.Equal(img.Pix, bare) {
		t.Error("drawing the mouse changed the player's image")
	}
	if err = x.setDisplay(display{}); err != nil {
		t.Fatal(err)
	}
	if img, err = x.player.Image(); err != nil {
		t.Fatal(err)
	}
	if x.withMouse(img, release) != img {
		t.Error("mouse drawn with the display's mouse off")
	}
}
