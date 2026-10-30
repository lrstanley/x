// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"
)

// kittyAPC returns a Kitty graphics command with control keys ctrl and
// payload, base64-encoded.
func kittyAPC(ctrl string, payload []byte) string {
	if payload == nil {
		return "\x1b_G" + ctrl + "\x1b\\"
	}
	return "\x1b_G" + ctrl + ";" + base64.StdEncoding.EncodeToString(payload) + "\x1b\\"
}

func writeTerm(t *testing.T, term *Terminal, s string) {
	t.Helper()
	if _, err := term.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
}

// solid returns w by h RGBA pixels of c.
func solid(w, h int, c color.NRGBA) []byte {
	return bytes.Repeat([]byte{c.R, c.G, c.B, c.A}, w*h)
}

func TestKittyDirectImage(t *testing.T) {
	t.Parallel()
	term := openTest(t, 10, 4)
	rgba := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	replies, err := term.WriteReplies([]byte(kittyAPC("a=T,i=1,f=32,s=2,v=2,c=2,r=1,z=-1", rgba)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(replies), "i=1;OK") {
		t.Errorf("replies = %q, want an OK for image 1", replies)
	}
	imgs := term.Images()
	if len(imgs) != 1 {
		t.Fatalf("got %d placements, want 1", len(imgs))
	}
	p := imgs[0]
	if p.Image.ID != 1 || !bytes.Equal(p.Image.Pixels.Pix, rgba) {
		t.Errorf("image = %d %v, want 1 %v", p.Image.ID, p.Image.Pixels.Pix, rgba)
	}
	// Two columns and a row of the default 10 by 20 cells.
	if want := image.Rect(0, 0, 20, 20); p.Rect != want || p.Source != image.Rect(0, 0, 2, 2) {
		t.Errorf("rect %v source %v, want %v from the whole image", p.Rect, p.Source, want)
	}
	if p.Z != -1 || !p.BelowText() || p.BelowBackground() {
		t.Errorf("z = %d, want -1, below the text", p.Z)
	}
	if cur := term.Cursor(); cur.X != 2 || cur.Y != 0 {
		t.Errorf("cursor at %d,%d after the image, want 2,0", cur.X, cur.Y)
	}
	scr := term.Screen()
	if len(scr.Images()) != 1 || scr.CellSize() != image.Pt(10, 20) {
		t.Errorf("screen images %v, cell size %v", scr.Images(), scr.CellSize())
	}

	writeTerm(t, term, kittyAPC("a=d,d=I,i=1", nil))
	if left := term.Images(); len(left) != 0 {
		t.Errorf("placements after delete: %+v", left)
	}
}

// TestKittyFormats checks that PNG, zlib, RGB, and gray payloads decode to
// the same pixels.
func TestKittyFormats(t *testing.T) {
	t.Parallel()
	red := color.NRGBA{R: 0xff, A: 0xff}
	src := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	src.SetNRGBA(0, 0, red)
	src.SetNRGBA(1, 0, red)
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, src); err != nil {
		t.Fatal(err)
	}
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	_, _ = zw.Write(solid(2, 1, red))
	_ = zw.Close()

	for name, tc := range map[string]struct {
		ctrl    string
		payload []byte
		want    color.NRGBA
	}{
		"png":  {"a=T,i=1,f=100", pngBuf.Bytes(), red},
		"zlib": {"a=T,i=1,f=32,o=z,s=2,v=1", z.Bytes(), red},
		"rgb":  {"a=T,i=1,f=24,s=2,v=1", []byte{0xff, 0, 0, 0xff, 0, 0}, red},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			term := openTest(t, 10, 2)
			writeTerm(t, term, kittyAPC(tc.ctrl, tc.payload))
			imgs := term.Images()
			if len(imgs) != 1 {
				t.Fatalf("got %d placements, want 1", len(imgs))
			}
			px := imgs[0].Image.Pixels
			if px.Rect != image.Rect(0, 0, 2, 1) || px.NRGBAAt(1, 0) != tc.want {
				t.Errorf("pixels %v at %v, want 2x1 of %v", px.Pix, px.Rect, tc.want)
			}
		})
	}
	if img := decodeKittyPixels([]byte{0x40, 0x80}, 3, 1, 1); img.NRGBAAt(0, 0) != (color.NRGBA{0x40, 0x40, 0x40, 0x80}) {
		t.Errorf("gray alpha = %v", img.NRGBAAt(0, 0))
	}
	if img := decodeKittyPixels([]byte{0x40}, 4, 1, 1); img.NRGBAAt(0, 0) != (color.NRGBA{0x40, 0x40, 0x40, 0xff}) {
		t.Errorf("gray = %v", img.NRGBAAt(0, 0))
	}
}

// TestKittyScrolls checks that a placement moves with the text it was
// placed on, and that the buffer generation changes when it does.
func TestKittyScrolls(t *testing.T) {
	t.Parallel()
	term := openTest(t, 10, 3)
	writeTerm(t, term, kittyAPC("a=T,i=1,f=32,s=1,v=1,c=1,r=2,C=1", solid(1, 1, color.NRGBA{A: 0xff})))
	before := term.Screen()
	if imgs := before.Images(); len(imgs) != 1 || imgs[0].Rect.Min != (image.Point{}) {
		t.Fatalf("placements %+v, want one at the origin", imgs)
	}
	writeTerm(t, term, "\n\n\n")
	after := term.Screen()
	if imgs := after.Images(); len(imgs) != 1 || imgs[0].Rect.Min.Y != -20 {
		t.Fatalf("placements %+v after one row of scroll, want one at y -20", imgs)
	}
	if before.Generation() == after.Generation() {
		t.Error("the generation did not change when the image moved")
	}
}

// TestKittyStorageLimit checks that a zero limit turns Kitty graphics off,
// and that a full reset restores the default.
func TestKittyStorageLimit(t *testing.T) {
	t.Parallel()
	term := openTest(t, 10, 2)
	if err := term.SetKittyImageStorageLimit(0); err != nil {
		t.Fatal(err)
	}
	seq := kittyAPC("a=T,i=1,f=32,s=1,v=1", solid(1, 1, color.NRGBA{A: 0xff}))
	writeTerm(t, term, seq)
	if imgs := term.Images(); len(imgs) != 0 {
		t.Fatalf("placements with Kitty graphics off: %+v", imgs)
	}
	writeTerm(t, term, "\x1bc"+seq)
	if imgs := term.Images(); len(imgs) != 1 {
		t.Fatalf("got %d placements after RIS, want 1", len(imgs))
	}
}

// TestKittyPlaceholders checks that Unicode placeholder cells draw the part
// of a virtual placement's image they number.
func TestKittyPlaceholders(t *testing.T) {
	t.Parallel()
	term := openTest(t, 10, 2)
	// A 20x20 image in a 4x1 box of 10x20 cells: it fits the height, and is
	// centered with 10 pixels on each side.
	writeTerm(t, term, kittyAPC("a=T,i=5,f=32,s=20,v=20,U=1,c=4,r=1,q=2", solid(20, 20, color.NRGBA{G: 0xff, A: 0xff})))
	if imgs := term.Images(); len(imgs) != 0 {
		t.Fatalf("a virtual placement drew %+v without placeholders", imgs)
	}
	// Image id 5 as the 256-color foreground; row 0, columns 0 and 1, which
	// show the padding and the left half of the image.
	cells := "\x1b[38;5;5m\U0010EEEE̅̅\U0010EEEE̅̍\x1b[m"
	writeTerm(t, term, cells)
	imgs := term.Images()
	if len(imgs) != 1 {
		t.Fatalf("got %d placements, want one run: %+v", len(imgs), imgs)
	}
	if p := imgs[0]; p.Rect != image.Rect(10, 0, 30, 20) || p.Source != image.Rect(0, 0, 20, 20) || p.Clip != image.Rect(0, 0, 20, 20) {
		t.Errorf("rect %v source %v clip %v, want the image centered, clipped to two cells", p.Rect, p.Source, p.Clip)
	}
	// A cell with no marks continues the run before it.
	writeTerm(t, term, "\r\n\x1b[38;5;5m\U0010EEEE̅̅\U0010EEEE\x1b[m")
	if both := term.Images(); len(both) != 2 || both[1].Rect != image.Rect(10, 20, 30, 40) || both[1].Clip != image.Rect(0, 20, 20, 40) {
		t.Errorf("placements %+v, want the second row to match the first", both)
	}
}

// TestKittyAnimation checks that an animation advances on the terminal's
// clock, and that AnimationDue says when.
func TestKittyAnimation(t *testing.T) {
	t.Parallel()
	now := time.Duration(0)
	term, err := testRuntime.Open(context.Background(), 10, 2, WithClock(func() time.Duration { return now }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = term.Close() })
	red, blue := color.NRGBA{R: 0xff, A: 0xff}, color.NRGBA{B: 0xff, A: 0xff}
	writeTerm(t, term, kittyAPC("a=T,i=1,f=32,s=1,v=1,q=2", solid(1, 1, red))+
		kittyAPC("a=f,i=1,f=32,s=1,v=1,z=100,q=2", solid(1, 1, blue))+
		// The root frame's gap, then run the animation.
		kittyAPC("a=a,i=1,r=1,z=100,q=2", nil)+
		kittyAPC("a=a,i=1,s=3,q=2", nil))
	first := term.Images()
	if len(first) != 1 || first[0].Image.Pixels.NRGBAAt(0, 0) != red {
		t.Fatalf("placements %+v, want the red root frame", first)
	}
	due, ok := term.AnimationDue()
	if !ok || due != 100*time.Millisecond {
		t.Fatalf("AnimationDue() = %v, %v, want 100ms", due, ok)
	}
	now = due
	next := term.Images()
	if len(next) != 1 || next[0].Image.Pixels.NRGBAAt(0, 0) != blue {
		t.Fatalf("placements %+v at %v, want the blue frame", next, now)
	}
	if next[0].Image.Generation == first[0].Image.Generation {
		t.Error("the image generation did not change with the frame")
	}
}

// TestKittyRedraw checks that a redraw puts the screen's placements back
// with their images: a plain one, one in a layer under the text with
// offsets and a source rectangle, one partly scrolled off the top, and a
// virtual one drawn by placeholder cells.
func TestKittyRedraw(t *testing.T) {
	t.Parallel()
	term := openTest(t, 12, 4)
	green := color.NRGBA{G: 0xff, A: 0xff}
	big := image.NewNRGBA(image.Rect(0, 0, 4, 40))
	for y := range 40 {
		for x := range 4 {
			big.SetNRGBA(x, y, color.NRGBA{R: uint8(y), A: 0xff}) //nolint:gosec // y < 40.
		}
	}
	writeTerm(t, term, kittyAPC("a=T,i=1,f=32,s=4,v=40,c=1,r=2,q=2", big.Pix)+"\r\n\n\n")
	writeTerm(t, term, kittyAPC("a=T,i=2,f=32,s=2,v=2,X=3,Y=4,x=1,w=1,z=-5,C=1,q=2", solid(2, 2, green)))
	writeTerm(t, term, "\x1b[4;5H"+kittyAPC("a=T,i=3,f=32,s=20,v=20,U=1,c=2,r=1,q=2", solid(20, 20, green))+
		"\x1b[38;5;3m\U0010EEEE̅̅\U0010EEEE̅̍\x1b[m")
	want := term.Images()
	if len(want) != 3 {
		t.Fatalf("got %d placements before the redraw, want 3: %+v", len(want), want)
	}
	if want[1].Rect.Min.Y >= 0 {
		t.Fatalf("image 1 at %v is not partly above the screen", want[1].Rect)
	}

	got := redrawInto(t, term).Images()
	if len(got) != len(want) {
		t.Fatalf("got %d placements after the redraw, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.Image.ID != w.Image.ID || g.Z != w.Z || !bytes.Equal(g.Image.Pixels.Pix, w.Image.Pixels.Pix) {
			t.Errorf("placement %d = image %d z %d, want image %d z %d", i, g.Image.ID, g.Z, w.Image.ID, w.Z)
		}
		// The cropped image shows the same rows of the source, from the top
		// of the screen down.
		visible := func(p Placement) (image.Rectangle, int) {
			if p.Rect.Min.Y >= 0 {
				return p.Rect, p.Source.Min.Y
			}
			cut := -p.Rect.Min.Y
			return image.Rect(p.Rect.Min.X, 0, p.Rect.Max.X, p.Rect.Max.Y), p.Source.Min.Y + cut*p.Source.Dy()/p.Rect.Dy()
		}
		gr, gy := visible(g)
		wr, wy := visible(w)
		if gr != wr || gy != wy {
			t.Errorf("placement %d shows %v from source y %d, want %v from %d", i, gr, gy, wr, wy)
		}
	}
}

// TestKittyPlaceholderRows checks that each row of placeholders shows its
// part of an image smaller than the box, without rounding it away.
func TestKittyPlaceholderRows(t *testing.T) {
	t.Parallel()
	term := openTest(t, 10, 2)
	writeTerm(t, term, kittyAPC("a=T,i=3,f=32,s=1,v=1,U=1,c=2,r=2,q=2", solid(1, 1, color.NRGBA{R: 0xff, A: 0xff}))+
		"\x1b[38;5;3m\U0010EEEE\u0305\u0305\U0010EEEE\r\n\U0010EEEE\u030D\u0305\U0010EEEE\x1b[m")
	imgs := term.Images()
	if len(imgs) != 2 {
		t.Fatalf("got %d placements, want a run on each row: %+v", len(imgs), imgs)
	}
	// The 1x1 image fills the 20x40 box's width, centered in its height.
	for i, p := range imgs {
		if p.Rect != image.Rect(0, 10, 20, 30) || p.Clip != image.Rect(0, 20*i, 20, 20*(i+1)) {
			t.Errorf("row %d: rect %v clip %v", i, p.Rect, p.Clip)
		}
	}
}

// TestKittyStorageOption checks that WithKittyStorage sets the limit a full
// reset restores, and that a terminal grown by images is not pooled.
func TestKittyStorageOption(t *testing.T) {
	t.Parallel()
	rt := NewRuntime(1)
	term, err := rt.Open(context.Background(), 10, 2, WithKittyStorage(0))
	if err != nil {
		t.Fatal(err)
	}
	seq := kittyAPC("a=T,i=1,f=32,s=1,v=1,q=2", solid(1, 1, color.NRGBA{A: 0xff}))
	writeTerm(t, term, seq+"\x1bc"+seq)
	if imgs := term.Images(); len(imgs) != 0 {
		t.Fatalf("placements with Kitty graphics off after RIS: %+v", imgs)
	}
	_ = term.Close()

	// The pooled instance takes the next terminal's options.
	term, err = rt.Open(context.Background(), 10, 2)
	if err != nil {
		t.Fatal(err)
	}
	big := 2048
	writeTerm(t, term, kittyAPC(fmt.Sprintf("a=T,i=1,f=32,s=%d,v=%d,q=2", big, big), solid(big, big, color.NRGBA{A: 0xff})))
	if imgs := term.Images(); len(imgs) != 1 {
		t.Fatalf("got %d placements with the default storage, want 1", len(imgs))
	}
	if len(term.mod.Memory()) <= maxPooledMemory {
		t.Fatalf("a %dx%d image left memory at %d bytes", big, big, len(term.mod.Memory()))
	}
	grown := term
	_ = term.Close()
	if term, err = rt.Open(context.Background(), 10, 2); err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	if term == grown {
		t.Error("a terminal past the pooled memory limit was reused")
	}
}
