// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"bytes"
	"context"
	"encoding/base64"
	"image/color"
	"testing"

	"github.com/lrstanley/x/term/reel/vt"
)

// kittyScreen writes s to a fresh cols by rows terminal and returns its
// screen and state.
func kittyScreen(t *testing.T, cols, rows int, s string) (*vt.Buffer, vt.State) {
	t.Helper()
	term, err := vt.NewRuntime(-1).Open(context.Background(), cols, rows)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = term.Close() })
	if _, err = term.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	st := term.State()
	st.Focused = true
	return term.Screen(), st
}

func kittyImage(ctrl string, w, h int, c color.NRGBA) string {
	px := bytes.Repeat([]byte{c.R, c.G, c.B, c.A}, w*h)
	return "\x1b_G" + ctrl + ";" + base64.StdEncoding.EncodeToString(px) + "\x1b\\"
}

// TestDrawKittyImages checks that placements are drawn over the cells they
// cover, scaled from the terminal's cells to the renderer's, in their layer.
func TestDrawKittyImages(t *testing.T) {
	t.Parallel()
	red := color.NRGBA{R: 0xff, A: 0xff}
	d := MustNew()
	// A 2x1-cell red image at the origin, and text drawn over a layer under
	// it at column 4.
	scr, st := kittyScreen(t, 8, 2,
		kittyImage("a=T,i=1,f=32,s=1,v=1,c=2,r=1,q=2", 1, 1, red)+
			"\x1b[1;5H"+kittyImage("a=T,i=2,f=32,s=1,v=1,c=1,r=1,z=-1,C=1,q=2", 1, 1, red)+"█")
	img := drawAt(t, d, scr, st, 0)
	l := d.Layout(8, 2)
	for _, x := range []int{0, 1} {
		c := l.CellRect(x, 0)
		mid := c.Min.Add(c.Size().Div(2))
		if got := img.NRGBAAt(mid.X, mid.Y); got != red {
			t.Errorf("cell %d,0 center = %v, want the red image", x, got)
		}
	}
	if c := l.CellRect(2, 0); img.NRGBAAt(c.Min.X+c.Dx()/2, c.Min.Y+c.Dy()/2) == red {
		t.Error("the image reaches past its two cells")
	}
	under := l.CellRect(4, 0)
	if got := img.NRGBAAt(under.Min.X+under.Dx()/2, under.Min.Y+under.Dy()/2); got == red {
		t.Errorf("the text over a layer under it is hidden: %v", got)
	}
}

// TestDrawKittyPlaceholder checks that placeholder cells draw their image,
// not their text.
func TestDrawKittyPlaceholder(t *testing.T) {
	t.Parallel()
	blue := color.NRGBA{B: 0xff, A: 0xff}
	d := MustNew()
	scr, st := kittyScreen(t, 4, 1,
		kittyImage("a=T,i=7,f=32,s=1,v=1,U=1,c=1,r=1,q=2", 1, 1, blue)+
			"\x1b[38;5;7m\U0010EEEE̅̅\x1b[m")
	img := drawAt(t, d, scr, st, 0)
	c := d.Layout(4, 1).CellRect(0, 0)
	// The square image fits the cell's width, centered between bands of
	// padding above and below.
	if mid := c.Min.Add(c.Size().Div(2)); img.NRGBAAt(mid.X, mid.Y) != blue {
		t.Errorf("placeholder center = %v, want the blue image", img.NRGBAAt(mid.X, mid.Y))
	}
	if got := img.NRGBAAt(c.Min.X, c.Min.Y); got == blue {
		t.Errorf("placeholder top-left = %v, want padding", got)
	}
}
