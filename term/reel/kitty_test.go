// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/reel/vt"
)

var (
	kittyRed  = color.NRGBA{R: 0xff, A: 0xff}
	kittyBlue = color.NRGBA{B: 0xff, A: 0xff}
)

// kittyCmd returns a Kitty graphics command with control keys ctrl and, if
// c is not zero, a w by h payload of c.
func kittyCmd(ctrl string, w, h int, c color.NRGBA) string {
	if c == (color.NRGBA{}) {
		return "\x1b_G" + ctrl + "\x1b\\"
	}
	px := bytes.Repeat([]byte{c.R, c.G, c.B, c.A}, w*h)
	return "\x1b_G" + ctrl + ";" + base64.StdEncoding.EncodeToString(px) + "\x1b\\"
}

// cellColor returns the color at the center of cell x, y of img, drawn by d.
func cellColor(d *render.Renderer, img image.Image, cols, rows, x, y int) color.Color {
	c := d.Layout(cols, rows).CellRect(x, y)
	mid := c.Min.Add(c.Size().Div(2))
	return img.At(mid.X, mid.Y)
}

// TestExportKittyPNG checks that a PNG export draws a Kitty image over the
// cells it was placed on.
func TestExportKittyPNG(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(Header{Cols: 6, Rows: 2},
		outAt(0, kittyCmd("a=T,i=1,f=32,s=1,v=1,c=2,r=1,q=2", 1, 1, kittyRed)+"hi"),
		Exit{event: at(100)},
	)
	d := render.MustNew()
	path := filepath.Join(t.TempDir(), "out.png")
	if err := Export(t.Context(), tl, Profile{Outputs: MustFiles(path), Rasterizer: d}); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	for x := range 2 {
		if got := color.NRGBAModel.Convert(cellColor(d, img, 6, 2, x, 0)); got != kittyRed {
			t.Errorf("cell %d,0 = %v, want the red image", x, got)
		}
	}
}

// TestCastKeepsKittyImages checks that images placed before a cut are
// still there when the cut is written as a .cast and read back.
func TestCastKeepsKittyImages(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(Header{Cols: 10, Rows: 4},
		outAt(0, kittyCmd("a=T,i=1,f=32,s=2,v=2,c=2,r=2,q=2", 2, 2, kittyRed)+"\r\n\n"),
		outAt(10, kittyCmd("a=T,i=2,f=32,s=1,v=1,X=2,z=-3,C=1,q=2", 1, 1, kittyBlue)),
		Mark{event: at(20), Label: "cut"},
		outAt(30, "more"),
	)
	cut, err := tl.Between("cut", "")
	if err != nil {
		t.Fatal(err)
	}
	images := func(h Header, events []Event) []vt.Placement {
		r, rerr := newReplay(t.Context(), replayRuntime, h, 0, 0)
		if rerr != nil {
			t.Fatal(rerr)
		}
		t.Cleanup(func() { _ = r.close() })
		for _, ev := range events {
			if err = r.apply(ev); err != nil {
				t.Fatal(err)
			}
		}
		return r.term.Images()
	}
	want := images(tl.header, tl.events)
	if len(want) != 2 {
		t.Fatalf("got %d placements from the source, want 2", len(want))
	}
	for _, v := range []CastVersion{CastV3, CastV2} {
		back := readCast(t, writeCast(t, cut, v))
		got := images(back.header, back.events)
		if len(got) != len(want) {
			t.Fatalf("v%d: got %d placements, want %d", v, len(got), len(want))
		}
		for i := range want {
			g, w := got[i], want[i]
			if g.Rect != w.Rect || g.Source != w.Source || g.Z != w.Z || !bytes.Equal(g.Image.Pixels.Pix, w.Image.Pixels.Pix) {
				t.Errorf("v%d: placement %d = %+v, want %+v", v, i, g, w)
			}
		}
	}
}

// TestExportKittyAnimation checks that a running animation makes a frame
// each time its frame changes, between and after the timeline's events.
func TestExportKittyAnimation(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(Header{Cols: 4, Rows: 1},
		outAt(0, kittyCmd("a=T,i=1,f=32,s=1,v=1,q=2", 1, 1, kittyRed)+
			kittyCmd("a=f,i=1,f=32,s=1,v=1,z=100,q=2", 1, 1, kittyBlue)+
			kittyCmd("a=a,i=1,r=1,z=100,q=2", 0, 0, color.NRGBA{})+
			kittyCmd("a=a,i=1,s=3,q=2", 0, 0, color.NRGBA{})),
		outAt(150, "x"),
		Exit{event: at(350)},
	)
	frames := exportFrames(t, tl, Profile{FPS: 100})
	type shown struct {
		at    time.Duration
		color color.NRGBA
	}
	var got []shown
	for _, f := range frames {
		imgs := f.Cells.Screen.Images()
		if len(imgs) != 1 {
			t.Fatalf("frame at %v has %d placements, want 1", f.At, len(imgs))
		}
		got = append(got, shown{f.At, imgs[0].Image.Pixels.NRGBAAt(0, 0)})
	}
	want := []shown{
		{0, kittyRed}, {100 * ms, kittyBlue}, {150 * ms, kittyBlue}, {200 * ms, kittyRed}, {300 * ms, kittyBlue},
	}
	if len(got) != len(want) {
		t.Fatalf("frames %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("frame %d = %v, want %v", i, got[i], want[i])
		}
	}
}

// TestCastCellSize checks that a session's cell size reaches the .cast
// header and the terminal a replay of it opens.
func TestCastCellSize(t *testing.T) {
	t.Parallel()
	s, err := Open(t.Context(), 4, 1, WithCellSize(8, 16))
	if err != nil {
		t.Fatal(err)
	}
	if w, h := s.Terminal().CellSize(); w != 8 || h != 16 {
		t.Errorf("session cell size %dx%d, want 8x16", w, h)
	}
	if _, err = s.Write([]byte("a")); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	data := writeCast(t, s.Timeline(), CastV3)
	if !bytes.Contains(data, []byte(`"cell_width":8,"cell_height":16`)) {
		t.Errorf("header %s has no cell size", bytes.SplitN(data, []byte("\n"), 2)[0])
	}
	back := readCast(t, data)
	if back.header.CellWidth != 8 || back.header.CellHeight != 16 {
		t.Errorf("read cell size %dx%d", back.header.CellWidth, back.header.CellHeight)
	}
	r, err := newReplay(t.Context(), replayRuntime, back.header, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()
	if w, h := r.term.CellSize(); w != 8 || h != 16 {
		t.Errorf("replay cell size %dx%d, want 8x16", w, h)
	}
}
