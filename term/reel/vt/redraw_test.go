// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// assertSameTerminal fails unless a and b show the same screen, scrollback,
// state, and modes.
func assertSameTerminal(t *testing.T, a, b *Terminal) {
	t.Helper()
	sa, sb := a.State(), b.State()
	sa.Generation, sb.Generation = 0, 0
	sa.ProgramStatusGeneration, sb.ProgramStatusGeneration = 0, 0
	// The redraw replays the prompt step, so it arrives later.
	sa.Prompt.At, sb.Prompt.At = 0, 0
	if !sa.Equal(sb) {
		t.Errorf("state:\n%+v\nwant:\n%+v", sb, sa)
	}
	ga, gb := a.Screen(), b.Screen()
	for y := range ga.Height() {
		for x := range ga.Width() {
			ca, cb := ga.CellAt(x, y), gb.CellAt(x, y)
			if ca.Content != cb.Content || ca.Width != cb.Width || ca.Style != cb.Style || ca.Link != cb.Link {
				t.Errorf("cell %d,%d = %+v, want %+v", x, y, *cb, *ca)
			}
		}
	}
	if fa, fb := fmt.Sprint(a.Scrollback()), fmt.Sprint(b.Scrollback()); fa != fb {
		t.Errorf("scrollback:\n%s\nwant:\n%s", fb, fa)
	}
	for _, m := range []Mode{
		{Number: 1049},
		{Number: 2004},
		{Number: 1000},
		{Number: 1006},
		{Number: 25},
		{Number: 7},
		{Number: 1004},
		{Number: 4, ANSI: true},
	} {
		ma, _ := a.Mode(m)
		mb, _ := b.Mode(m)
		if ma != mb {
			t.Errorf("mode %+v = %v, want %v", m, mb, ma)
		}
	}
	if a.WorkingDirectory() != b.WorkingDirectory() {
		t.Errorf("pwd = %q, want %q", b.WorkingDirectory(), a.WorkingDirectory())
	}
}

// redrawInto writes term's redraw into a fresh terminal of its size.
func redrawInto(t *testing.T, term *Terminal) *Terminal {
	t.Helper()
	data, err := term.Redraw()
	if err != nil {
		t.Fatal(err)
	}
	cols, rows := term.Size()
	fresh := openTest(t, cols, rows)
	if _, err = fresh.Write(data); err != nil {
		t.Fatal(err)
	}
	return fresh
}

func TestRedraw(t *testing.T) {
	t.Parallel()
	var history strings.Builder
	for i := range 12 {
		fmt.Fprintf(&history, "line %d\r\n", i)
	}
	for name, seq := range map[string]string{
		"empty": "",
		"styles and scrollback": history.String() +
			"\x1b[1;31mred\x1b[0m \x1b[4;38;5;33mblue\x1b[0m \x1b[48;2;10;20;30mrgb\x1b[0m wide 😀\r\n" +
			"\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\ \x1b[7mrev\x1b[3;9mpen",
		"cursor and modes": "abc\x1b[3;5H\x1b[?25l\x1b[5 q\x1b[?2004h\x1b[?1000h\x1b[?1006h\x1b[4h\x1b[?1004h",
		"alternate screen": "primary\x1b[?1049h\x1b[2J\x1b[Halt \x1b[32mscreen\x1b[0m",
		"region and wrap":  "\x1b[2;3r\x1b[1;1Hx\x1b[3;20Hy",
		"title, pwd, and colors": "\x1b]2;my title\x07\x1b]7;file:///tmp/dir\x07" +
			"\x1b]10;#112233\x07\x1b]11;#445566\x07\x1b]12;#778899\x07text",
		"palette":                "\x1b]4;1;#010203;200;#aabbcc\x07\x1b[31mred\x1b[38;5;200mpink",
		"reverse colors":         "\x1b[?5h\x1b[31mred",
		"trailing blank rows":    "a\r\nb\r\nc\r\nd\r\ne\r\nf\r\ng\r\n",
		"blank rows in history":  "a\r\n\r\n\r\nb\r\n\r\nc\r\nd\r\ne\r\n\r\n\r\n",
		"blank with history":     "\r\n\r\n\r\n\r\n\r\n\r\n",
		"bottom row":             history.String() + "\x1b[4;1Hlast",
		"progress":               "\x1b]9;4;3\x07busy",
		"program status":         "\x1b]7501;state=working:id=build:app=make:progress=40\x07\x1b]7501;state=blocked:kind=question\x07x",
		"program status cleared": "\x1b]9;4;1;50\x07\x1b]7501;state=working\x07\x1b]7501;state=clear\x07x",
		"prompt marks running": history.String() + promptCmd("ok", 0) + promptCmd("bad", 2) +
			"\x1b]133;A\x07$ \x1b]133;B\x07sleep\r\n\x1b]133;C;cmdline_url=sleep%201\x07",
		"prompt marks typing": promptCmd("x", 0) + "\x1b]133;A\x07$ \x1b]133;B\x07ty",
		"prompt marks ended":  promptCmd("x", 0) + "\x1b]133;A\x07$ \x1b]133;D\x07" + promptCmd("y", 127),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			term := openTest(t, 20, 4)
			if _, err := term.Write([]byte(seq)); err != nil {
				t.Fatal(err)
			}
			assertSameTerminal(t, term, redrawInto(t, term))
		})
	}
}

func TestRedrawHostColors(t *testing.T) {
	t.Parallel()
	// Colors the host set are not the program's, so they are not written.
	term := openTest(t, 4, 1)
	if err := term.SetDefaultForeground(Indexed(1)); err != nil {
		t.Fatal(err)
	}
	data, err := term.Redraw()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("\x1b]10;")) {
		t.Fatalf("redraw sets the host foreground: %q", data)
	}
}
