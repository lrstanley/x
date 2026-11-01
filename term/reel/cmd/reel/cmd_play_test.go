// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"bufio"
	"bytes"
	json "encoding/json/v2"
	"strings"
	"testing"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/vt"
)

// testView returns a view of the recording cast, drawn into out as on a
// terminal of cols by lines.
func testView(t *testing.T, cast string, cols, lines int) (*view, *bytes.Buffer) {
	t.Helper()
	tl, err := reel.ReadTimeline(strings.NewReader(cast))
	if err != nil {
		t.Fatal(err)
	}
	p, err := reel.NewPlayer(t.Context(), tl)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	var out bytes.Buffer
	v := newView(bufio.NewWriter(&out), p, tl.Header())
	v.size = func() (int, int, error) { return cols, lines, nil }
	return v, &out
}

// cast returns an asciicast file of header and events, each an event's
// code and data, a millisecond apart.
func cast(header string, events ...[2]string) string {
	var b strings.Builder
	b.WriteString(header + "\n")
	for i, ev := range events {
		line, err := json.Marshal([]any{float64(i+1) / 1000, ev[0], ev[1]})
		if err != nil {
			panic(err)
		}
		b.Write(append(line, '\n'))
	}
	return b.String()
}

// show seeks to the first n events and draws.
func (v *view) show(t *testing.T, n int) {
	t.Helper()
	if err := v.p.Seek(n); err != nil {
		t.Fatal(err)
	}
	if err := v.draw(); err != nil {
		t.Fatal(err)
	}
}

// host returns the screen text of a cols by lines terminal that out was
// written to.
func host(t *testing.T, out []byte, cols, lines int) string {
	t.Helper()
	term, err := vt.NewRuntime(1).Open(t.Context(), cols, lines)
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	if _, err = term.Write(out); err != nil {
		t.Fatal(err)
	}
	return term.Text()
}

// TestViewIsolates checks that what a recording does to its terminal is
// drawn, not passed on: modes, colors, screen switches, and cursor saves
// never reach this terminal.
func TestViewIsolates(t *testing.T) {
	v, out := testView(t, cast(`{"version": 2, "width": 20, "height": 3}`,
		[2]string{"o", "\x1b]11;#ff0000\x07\x1b7\x1b[?1049h\x1b[?1000h\x1b[1;31mhi\x1b[0m \x1b]8;;https://x\x1b\\link\x1b]8;;\x1b\\"},
	), 80, 24)
	v.show(t, 1)
	got := out.String()
	for _, leak := range []string{"\x1b]11;", "\x1b7", "\x1b[?1049h", "\x1b[?1000h"} {
		if strings.Contains(got, leak) {
			t.Errorf("output passes on %q: %q", leak, got)
		}
	}
	for _, want := range []string{"\x1b[0;1;38;5;1;48;2;255;0;0mhi", "\x1b]8;;https://x\alink", "\x1b[?2026h", "\x1b[1;8H\x1b[?25h\x1b[?2026l"} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q: %q", want, got)
		}
	}
}

// TestViewRows checks what this terminal shows: a row as wide as it is
// whole, only changed rows written again, and the rows a recording no
// longer has cleared when it shrinks.
func TestViewRows(t *testing.T) {
	v, out := testView(t, cast(`{"version": 2, "width": 4, "height": 3}`,
		[2]string{"o", "abcd\r\nefgh\r\nijkl"},
		[2]string{"o", "\x1b[2;1HX"},
		[2]string{"r", "4x1"},
	), 4, 3)
	v.show(t, 1)
	if got := host(t, out.Bytes(), 4, 3); got != "abcd\nefgh\nijkl" {
		t.Errorf("full rows show %q", got)
	}
	n := out.Len()
	v.show(t, 2)
	if redraw := out.String()[n:]; !strings.Contains(redraw, "\x1b[2;1H") || strings.Contains(redraw, "\x1b[1;1H") || strings.Contains(redraw, "\x1b[3;1H") {
		t.Errorf("redraw = %q, want only row 2", redraw)
	}
	v.show(t, 3)
	if got := host(t, out.Bytes(), 4, 3); got != "ijkl\n    \n    " {
		t.Errorf("after shrinking to a row, this terminal shows %q", got)
	}
}

// TestViewResize checks that resizing this terminal draws every row again,
// cropped to it.
func TestViewResize(t *testing.T) {
	v, out := testView(t, cast(`{"version": 2, "width": 10, "height": 3}`, [2]string{"o", "abcdefghij\r\nb\r\nc"}), 80, 24)
	v.show(t, 1)
	v.size = func() (int, int, error) { return 5, 2, nil }
	n := out.Len()
	v.show(t, 1)
	if got := host(t, out.Bytes()[n:], 5, 2); got != "abcde\nb    " {
		t.Errorf("after a resize, this terminal shows %q", got)
	}
}

func TestRenderRowWide(t *testing.T) {
	term, err := vt.NewRuntime(1).Open(t.Context(), 4, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	_, _ = term.Write([]byte("a世b"))
	buf := term.Screen()
	plain := func(vt.Style) string { return "" }
	if got := renderRow(buf, 0, 4, plain); got != "a世b" {
		t.Errorf("row = %q", got)
	}
	if got := renderRow(buf, 0, 2, plain); got != "a " {
		t.Errorf("row cut in a wide character = %q, want it blanked", got)
	}
}

// TestViewColors checks that the recording's default colors fill what
// cells leave to the terminal, from its header or set by its program, and
// that its header's and program's palettes draw indexed colors.
func TestViewColors(t *testing.T) {
	v, out := testView(t, cast(`{"version": 2, "width": 10, "height": 2, "theme": {"fg": "#ffffff", "bg": "#ff0000", "palette": "#000000:#000000:#008000:#000000:#000000:#000000:#000000:#000000"}}`,
		[2]string{"o", "a\x1b[32mb\x1b[33mc"},
		[2]string{"o", "\x1b]4;3;#0000ff\x07"},
	), 80, 24)
	v.show(t, 1)
	got := out.String()
	for _, want := range []string{"\x1b[0;38;2;255;255;255;48;2;255;0;0m\x1b[2K", "\x1b[0;38;2;0;128;0;48;2;255;0;0mb", "\x1b[0;38;2;0;0;0;48;2;255;0;0mc"} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q: %q", want, got)
		}
	}
	n := out.Len()
	v.show(t, 2)
	if got = out.String()[n:]; !strings.Contains(got, "\x1b[0;38;2;0;0;255;48;2;255;0;0mc") {
		t.Errorf("after OSC 4 = %q, want c in the color the program set", got)
	}

	v, out = testView(t, cast(`{"version": 2, "width": 10, "height": 2}`, [2]string{"o", "a"}, [2]string{"o", "\x1b]11;#0000ff\x07"}), 80, 24)
	v.show(t, 1)
	if strings.Contains(out.String(), "48;2") {
		t.Errorf("output without recording colors sets a background: %q", out.String())
	}
	n = out.Len()
	v.show(t, 2)
	if got = out.String()[n:]; !strings.Contains(got, "\x1b[1;1H\x1b[0;48;2;0;0;255m\x1b[2K\x1b[0;48;2;0;0;255ma") {
		t.Errorf("after OSC 11 = %q, want every row drawn again on its background", got)
	}
}
