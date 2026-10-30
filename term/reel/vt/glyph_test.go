// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

// glyphTriangle is a glyf record with one contour of three on-curve points,
// from libghostty-vt's own tests.
const glyphTriangle = "AAEAZABkA4QDhAACAAABAQEB9P5wAyADhPzgAAA="

var glyphTrianglePoints = [][]GlyphPoint{{
	{X: 500, Y: 900, OnCurve: true},
	{X: 100, Y: 100, OnCurve: true},
	{X: 900, Y: 100, OnCurve: true},
}}

func TestStateGlyphs(t *testing.T) {
	t.Parallel()

	term := openTest(t, 8, 2)
	if g := term.State().Glyphs; g != nil {
		t.Fatalf("new terminal glyphs = %v, want nil", g)
	}
	register := "\x1b_25a1;r;cp=e000;upm=2048;aw=1024;lh=1536;width=2;size=stretch;align=end,baseline;pad=0.1,0.2,0.3,0.4;" + glyphTriangle + "\x1b\\"
	reply, err := term.WriteReplies([]byte(register))
	if err != nil {
		t.Fatal(err)
	}
	if want := "\x1b_25a1;r;cp=e000;status=0\x1b\\"; string(reply) != want {
		t.Fatalf("register reply = %q, want %q", reply, want)
	}
	g := term.State().Glyphs
	gl := g.Lookup(0xe000)
	if gl == nil || len(g.order) != 1 {
		t.Fatalf("Lookup(U+E000) = %v with %d glyphs", gl, len(g.order))
	}
	want := &Glyph{
		Rune:       0xe000,
		Contours:   glyphTrianglePoints,
		UnitsPerEm: 2048,
		Advance:    1024,
		LineHeight: 1536,
		Cells:      2,
		Size:       GlyphSizeStretch,
		AlignX:     GlyphAlignEnd,
		AlignY:     GlyphAlignBaseline,
		Pad:        [4]float64{0.1, 0.2, 0.3, 0.4},
		raw:        gl.raw,
	}
	if !reflect.DeepEqual(gl, want) {
		t.Fatalf("glyph = %+v, want %+v", gl, want)
	}

	if _, err = term.Write([]byte("text")); err != nil {
		t.Fatal(err)
	}
	if again := term.State().Glyphs; again != g {
		t.Error("output that registered nothing replaced the glyphs")
	}

	// Registering the same glyph again changes nothing either, and another
	// glyph leaves this one as it was.
	if _, err = term.WriteReplies([]byte(register)); err != nil {
		t.Fatal(err)
	}
	if again := term.State().Glyphs; again != g {
		t.Error("registering an identical glyph replaced the glyphs")
	}
	if _, err = term.Write([]byte("\x1b_25a1;r;cp=e001;reply=0;" + glyphTriangle + "\x1b\\")); err != nil {
		t.Fatal(err)
	}
	if more := term.State().Glyphs; more == g || more.Lookup(0xe000) != gl || more.Lookup(0xe001) == nil {
		t.Error("registering another glyph did not keep the first")
	}
	if _, err = term.Write([]byte("\x1b_25a1;c;cp=e001\x1b\\")); err != nil {
		t.Fatal(err)
	}
	g = term.State().Glyphs

	// A rejected registration changes nothing.
	if _, err = term.Write([]byte("\x1b_25a1;r;cp=41;" + glyphTriangle + "\x1b\\")); err != nil {
		t.Fatal(err)
	}
	if again := term.State().Glyphs; again != g {
		t.Error("a rejected registration replaced the glyphs")
	}

	if _, err = term.Write([]byte("\x1b_25a1;c;cp=e000\x1b\\")); err != nil {
		t.Fatal(err)
	}
	if cleared := term.State().Glyphs; cleared != nil {
		t.Fatalf("glyphs after clearing the only one = %v, want nil", cleared)
	}

	if _, err = term.Write([]byte("\x1b_25a1;r;cp=e001;reply=0;" + glyphTriangle + "\x1b\\\x1bc")); err != nil {
		t.Fatal(err)
	}
	if reset := term.State().Glyphs; reset != nil {
		t.Fatalf("glyphs after a full reset = %v, want nil", reset)
	}
}

func TestStateGlyphDefaults(t *testing.T) {
	t.Parallel()

	term := openTest(t, 8, 2)
	if _, err := term.Write([]byte("\x1b_25a1;r;cp=100000;reply=0;" + glyphTriangle + "\x1b\\")); err != nil {
		t.Fatal(err)
	}
	gl := term.State().Glyphs.Lookup(0x100000)
	if gl == nil {
		t.Fatal("glyph not registered")
	}
	if gl.UnitsPerEm != 1000 || gl.Advance != 1000 || gl.LineHeight != 1000 || gl.Cells != 1 ||
		gl.Size != GlyphSizeHeight || gl.AlignX != GlyphAlignCenter ||
		gl.AlignY != GlyphAlignCenter || gl.Pad != [4]float64{} {
		t.Fatalf("glyph = %+v, want the protocol defaults", gl)
	}
}

func TestRedrawGlyphs(t *testing.T) {
	t.Parallel()

	src := openTest(t, 8, 2)
	in := "\x1b_25a1;r;cp=e001;reply=2;upm=500;" + glyphTriangle + "\x1b\\" +
		"\x1b_25a1;r;cp=e000;size=contain;" + glyphTriangle + "\x1b\\" +
		""
	if _, err := src.WriteReplies([]byte(in)); err != nil {
		t.Fatal(err)
	}
	vt, err := src.Redraw()
	if err != nil {
		t.Fatal(err)
	}
	dst := openTest(t, 8, 2)
	reply, err := dst.WriteReplies(vt)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(vt, []byte("reply=2")) {
		t.Errorf("redraw kept the program's reply option: %q", vt)
	}
	if bytes.Contains(reply, []byte("25a1")) {
		t.Errorf("redraw asked for glyph replies: %q", reply)
	}
	a, b := src.State().Glyphs, dst.State().Glyphs
	if got, want := glyphsVT(b), glyphsVT(a); got != want {
		t.Fatalf("redrawn glyphs = %q, want %q", got, want)
	}
	if a.order[0].Rune != 0xe001 {
		t.Errorf("oldest glyph = %U, want U+E001", a.order[0].Rune)
	}
	if got := dst.Screen().Text(); !strings.Contains(got, "") {
		t.Errorf("redrawn text = %q", got)
	}
}

func TestDecodeGlyf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data []byte
		want [][]GlyphPoint
	}{
		{
			name: "short deltas and same flags",
			data: []byte{
				0, 1, 0, 0, 0, 0, 0, 0, 0, 0, // one contour, bounding box
				0, 2, 0, 0, // last point 2, no instructions
				0x31, 0x32, 0x35, // on (0,0); off +10,0; on 0,+10
				10, // x of point 1
				10, // y of point 2
			},
			want: [][]GlyphPoint{{
				{X: 0, Y: 0, OnCurve: true},
				{X: 10, Y: 0},
				{X: 10, Y: 10, OnCurve: true},
			}},
		},
		{
			name: "repeated flags and two contours",
			data: []byte{
				0, 2, 0, 0, 0, 0, 0, 0, 0, 0,
				0, 0, 0, 2, 0, 0, // ends 0 and 2
				0x2b, 2, // on, x short negative, y same, repeated twice more
				5, 5, 5, // x: -5 each
			},
			want: [][]GlyphPoint{
				{{X: -5, OnCurve: true}},
				{{X: -10, OnCurve: true}, {X: -15, OnCurve: true}},
			},
		},
		{
			name: "no contours",
			data: []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
			want: [][]GlyphPoint{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := decodeGlyf(tt.data)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("decodeGlyf = %+v, want %+v", got, tt.want)
			}
		})
	}

	for _, bad := range [][]byte{
		nil,
		{0xff, 0xff, 0, 0, 0, 0, 0, 0, 0, 0}, // composite
		{0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 0, 0, 0x01}, // too few flags
	} {
		if _, err := decodeGlyf(bad); err == nil {
			t.Errorf("decodeGlyf(%x) succeeded", bad)
		}
	}
}
