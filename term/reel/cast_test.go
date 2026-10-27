// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"bytes"
	"errors"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel/vt"
)

var (
	castFg     = color.NRGBA{R: 0xd0, G: 0xd0, B: 0xd0, A: 0xff}
	castBg     = color.NRGBA{R: 0x21, G: 0x21, B: 0x21, A: 0xff}
	castCursor = color.NRGBA{R: 0xff, G: 0x80, B: 0x00, A: 0xff}
)

// writeCast writes tl as version v, checking the count WriteCast returns.
func writeCast(t *testing.T, tl *Timeline, v CastVersion) []byte {
	t.Helper()
	var buf bytes.Buffer
	n, err := tl.WriteCast(&buf, v)
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(buf.Len()) {
		t.Fatalf("WriteCast returned %d, wrote %d bytes", n, buf.Len())
	}
	return buf.Bytes()
}

func readCast(t *testing.T, data []byte) *Timeline {
	t.Helper()
	tl, err := ReadTimeline(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("ReadTimeline: %v\n%s", err, data)
	}
	return tl
}

func readCastFile(t *testing.T, name string) *Timeline {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return readCast(t, data)
}

// assertSameTimes fails unless got's events are at exactly the offsets of
// want's.
func assertSameTimes(t *testing.T, got, want *Timeline) {
	t.Helper()
	g, w := slices.Collect(got.Events()), slices.Collect(want.Events())
	if len(g) != len(w) {
		t.Fatalf("%d events, want %d", len(g), len(w))
	}
	for i := range g {
		if g[i].At() != w[i].At() {
			t.Fatalf("event %d at %v, want %v", i, g[i].At(), w[i].At())
		}
	}
}

// withDefaultPalette returns h with the palette a file without one of its
// own is written with, which it reads back with.
func withDefaultPalette(h Header) Header {
	for i := range h.Palette {
		if h.Palette[i] == nil {
			h.Palette[i] = color.NRGBAModel.Convert(vt.Indexed(i))
		}
	}
	return h
}

func TestCastRoundTrip(t *testing.T) {
	t.Parallel()
	tl := newTimeline(Header{
		Cols: 80, Rows: 24, Title: "demo <&>",
		Theme:        Colors{Fg: castFg, Bg: castBg, Cursor: castCursor},
		SkipPristine: true,
	})
	tl.start = time.Unix(1700000000, 0)
	for _, ev := range []Event{
		Output{event: at(0), Data: []byte("\x1b[1;32mhello\x1b[0m wörld ─│ 😀 <&>\r\n")},
		Mark{event: at(5), Label: "start"},
		Input{event: at(10), Data: []byte("\x1b[A\x1b[200~pasted\x1b[201~\x1b[<0;3;4M")},
		Output{event: at(10), Data: []byte("\x00\x07\x7f\"\\")},
		Focus{event: at(250), On: false},
		Theme{event: at(250), Colors: Colors{Fg: castBg, Bg: castFg}},
		Focus{event: at(1500), On: true},
		Theme{event: at(1500)},
		Resize{event: at(61_001), Cols: 132, Rows: 43},
		Mark{event: at(61_001), Label: ""},
		Theme{event: at(61_002), Colors: Colors{Cursor: castCursor}},
		Mark{event: at(61_003), Label: "end"},
		Exit{event: at(3_600_000), Status: -1},
	} {
		tl.appendAt(ev, ev.At())
	}

	got := readCast(t, writeCast(t, tl, CastV3))
	if want := withDefaultPalette(tl.Header()); got.Header() != want {
		t.Fatalf("Header = %+v, want %+v", got.Header(), want)
	}
	if !got.start.Equal(tl.start) {
		t.Fatalf("start = %v, want %v", got.start, tl.start)
	}
	assertEvents(t, got, describe(slices.Collect(tl.Events()))...)

	// The timeline read back works like any other.
	part, err := got.Between("start", "end")
	if err != nil {
		t.Fatal(err)
	}
	if from, to := part.Window(); from != 5*time.Millisecond || to != 61_003*time.Millisecond {
		t.Fatalf("Window = %v, %v", from, to)
	}
	for in := range got.Inputs() {
		if n := len(vt.DecodeInput(in.Data)); n != 3 {
			t.Fatalf("Strokes = %d, want 3: %+v", n, vt.DecodeInput(in.Data))
		}
	}
}

func TestCastFormat(t *testing.T) {
	t.Parallel()
	tl := newTimeline(Header{Cols: 20, Rows: 4, Title: "t", Theme: Colors{Fg: castFg, Bg: castBg, Cursor: castCursor}})
	tl.start = time.Unix(1700000000, 0)
	for _, ev := range []Event{
		Output{event: at(0), Data: []byte("a<b>")},
		Input{event: at(1001), Data: []byte("\r")},
		Resize{event: at(1001), Cols: 30, Rows: 5},
		Mark{event: at(1500), Label: "m"},
		Focus{event: at(1600)},
		Theme{event: at(1700), Colors: Colors{Fg: castFg, Cursor: castCursor}},
		Exit{event: at(12_345), Status: 3},
	} {
		tl.appendAt(ev, ev.At())
	}
	const palette = "#000000:#cd0000:#00cd00:#cdcd00:#0000ee:#cd00cd:#00cdcd:#e5e5e5:" +
		"#7f7f7f:#ff0000:#00ff00:#ffff00:#5c5cff:#ff00ff:#00ffff:#ffffff"

	v3 := `{"version":3,"term":{"cols":20,"rows":4,"type":"xterm-ghostty","theme":` +
		`{"fg":"#d0d0d0","bg":"#212121","palette":"` + palette + `","cursor":"#ff8000"}},` +
		`"timestamp":1700000000,"title":"t"}
[0.000,"o","a<b>"]
[1.001,"i","\r"]
[0.000,"r","30x5"]
[0.499,"m","m"]
[0.100,"f","0"]
[0.100,"t","fg=#d0d0d0,cursor=#ff8000"]
[10.645,"x","3"]
`
	if got := string(writeCast(t, tl, CastV3)); got != v3 {
		t.Fatalf("v3:\n%s\nwant:\n%s", got, v3)
	}
	var buf bytes.Buffer
	if _, err := tl.WriteTo(&buf); err != nil || buf.String() != v3 {
		t.Fatalf("WriteTo = %v:\n%s", err, buf.String())
	}

	// v2 has absolute times, and no focus, theme, or exit events.
	v2 := `{"version":2,"width":20,"height":4,"timestamp":1700000000,"title":"t",` +
		`"env":{"TERM":"xterm-ghostty"},"theme":{"fg":"#d0d0d0","bg":"#212121","palette":"` + palette + `"}}
[0.000,"o","a<b>"]
[1.001,"i","\r"]
[1.001,"r","30x5"]
[1.500,"m","m"]
`
	if got := string(writeCast(t, tl, CastV2)); got != v2 {
		t.Fatalf("v2:\n%s\nwant:\n%s", got, v2)
	}

	if _, err := tl.WriteCast(&buf, 1); err == nil {
		t.Fatal("WriteCast(1) succeeded")
	}
}

func TestCastNoTheme(t *testing.T) {
	t.Parallel()
	// Without both Fg and Bg there is no theme to write, and the header
	// reads back without colors.
	tl := newTimeline(Header{Cols: 2, Rows: 1, Theme: Colors{Fg: castFg}})
	for _, v := range []CastVersion{CastV3, CastV2} {
		data := writeCast(t, tl, v)
		if bytes.Contains(data, []byte("theme")) || bytes.Contains(data, []byte("timestamp")) {
			t.Fatalf("v%d header has a theme or timestamp: %s", v, data)
		}
		if h := readCast(t, data).Header(); h != (Header{Cols: 2, Rows: 1}) {
			t.Fatalf("v%d Header = %+v", v, h)
		}
	}
}

func TestCastV2RoundTrip(t *testing.T) {
	t.Parallel()
	tl := newTimeline(Header{
		Cols: 80, Rows: 24, Title: "v2",
		Theme:        Colors{Fg: castFg, Bg: castBg, Cursor: castCursor},
		SkipPristine: true,
	})
	for _, ev := range []Event{
		Output{event: at(0), Data: []byte("hi")},
		Focus{event: at(3)},
		Input{event: at(7), Data: []byte("q")},
		Theme{event: at(8), Colors: Colors{Fg: castFg}},
		Resize{event: at(1234), Cols: 100, Rows: 30},
		Mark{event: at(5000), Label: "end"},
		Exit{event: at(5001)},
	} {
		tl.appendAt(ev, ev.At())
	}
	got := readCast(t, writeCast(t, tl, CastV2))
	// v2 has no slot for the cursor color, and reel writes its own header
	// fields only in v3.
	if want := withDefaultPalette(Header{Cols: 80, Rows: 24, Title: "v2", Theme: Colors{Fg: castFg, Bg: castBg}}); got.Header() != want {
		t.Fatalf("Header = %+v, want %+v", got.Header(), want)
	}
	assertEvents(t, got,
		`0 output "hi"`,
		`7 input "q"`,
		`1234 resize 100x30`,
		`5000 mark end`,
	)
}

func TestCastIntervalsDoNotDrift(t *testing.T) {
	t.Parallel()
	// Offsets a third of a millisecond apart would drift by a third of a
	// millisecond per event if each interval were rounded on its own.
	tl := newTimeline(Header{Cols: 80, Rows: 24})
	const n = 30_000
	for i := range n {
		tl.appendAt(Output{Data: []byte{'a' + byte(i%26)}}, time.Duration(i)*time.Second/3000)
	}
	got := readCast(t, writeCast(t, tl, CastV3))
	assertSameTimes(t, got, tl)
	if d := got.Duration(); d != 10*time.Second {
		t.Fatalf("Duration = %v, want 10s", d)
	}
}

func TestCastSplitCharacters(t *testing.T) {
	t.Parallel()
	box, smile := "─", "😀" // 3 and 4 bytes.
	tl := testTimeline(t,
		Output{event: at(0), Data: []byte("a" + box[:1])},
		Input{event: at(1), Data: []byte(smile[:2])},
		Output{event: at(2), Data: []byte(box[1:2])},
		// An event holding only part of a character passes it on, and is
		// kept empty.
		Output{event: at(3), Data: []byte(box[2:] + smile[:3])},
		Input{event: at(4), Data: []byte(smile[2:] + "x")},
		Output{event: at(5), Data: []byte(smile[3:] + "\xffb\xe2")},
	)
	assertEvents(t, readCast(t, writeCast(t, tl, CastV3)),
		`0 output "a"`,
		`1 input ""`,
		`2 output ""`,
		`3 output "─"`,
		`4 input "😀x"`,
		// Bytes that are not UTF-8, including a character the last event
		// leaves unfinished, are U+FFFD.
		"5 output \"😀\uFFFDb\uFFFD\"",
	)

	// A character split at the end of the last event of a kind is not held.
	tl = testTimeline(t,
		Output{event: at(0), Data: []byte(box[:2])},
		Input{event: at(1), Data: []byte("x")},
	)
	assertEvents(t, readCast(t, writeCast(t, tl, CastV3)),
		"0 output \"\uFFFD\uFFFD\"",
		`1 input "x"`,
	)
}

func TestCastSession(t *testing.T) {
	t.Parallel()
	s := openTest(t, 20, 4, WithTitle("session"))
	write(t, s, "hello ─\r\n")
	for _, step := range []func() error{
		func() error { return s.Type("ok") },
		func() error { return s.Mark("a") },
		func() error { return s.Resize(30, 6) },
		func() error { return s.Focus(false) },
		func() error { return s.SetCursorColor(castCursor) },
		func() error { return s.SetBackground(nil) },
	} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	write(t, s, "\x1b[?1004h")

	tl := s.Timeline()
	got := readCast(t, writeCast(t, tl, CastV3))
	if want := withDefaultPalette(tl.Header()); got.Header() != want {
		t.Fatalf("Header = %+v, want %+v", got.Header(), want)
	}
	assertEvents(t, got, describe(slices.Collect(tl.Events()))...)
	assertSameTimes(t, got, tl)
	if got.start.Unix() != tl.start.Unix() {
		t.Fatalf("start = %v, want %v", got.start, tl.start)
	}
}

func TestReadTimelineAsciinema(t *testing.T) {
	t.Parallel()
	// Recorded by asciinema 3.2.1, and converted to v2 by it. v2's times are
	// absolute, so the same events come out of both.
	want := []string{
		`2 output "\x1b[1;32mhello\x1b[0m wörld ─│\r\r\n"`,
		`303 output "\x1b[31mred\x1b[0m\r\r\n"`,
		`505 exit 3`,
	}
	for _, name := range []string{"asciinema-v3.cast", "asciinema-v2.cast"} {
		tl := readCastFile(t, name)
		if h := tl.Header(); h != (Header{Cols: 40, Rows: 6, Title: "fixture"}) {
			t.Fatalf("%s: Header = %+v", name, h)
		}
		if tl.start.Unix() != 1792962137 {
			t.Fatalf("%s: start = %v", name, tl.start)
		}
		assertEvents(t, tl, want...)
	}
}

// hexNRGBA returns the opaque color 0xrrggbb.
func hexNRGBA(rgb uint32) color.NRGBA {
	return color.NRGBA{R: uint8(rgb >> 16), G: uint8(rgb >> 8), B: uint8(rgb), A: 0xff}
}

func TestReadTimelineSpec(t *testing.T) {
	t.Parallel()
	theme := Colors{Fg: castFg, Bg: castBg}

	// Written by hand from the v3 spec: comments, blank lines, extra header
	// fields, unknown codes, and intervals in microseconds.
	tl := readCastFile(t, "spec-v3.cast")
	want := Header{Cols: 80, Rows: 24, Title: "Demo", Theme: theme}
	want.Palette[0], want.Palette[7], want.Palette[15] = hexNRGBA(0x151515), hexNRGBA(0xd0d0d0), hexNRGBA(0xf5f5f5)
	if h := tl.Header(); h.Palette[0] != want.Palette[0] || h.Palette[7] != want.Palette[7] || h.Palette[15] != want.Palette[15] {
		t.Fatalf("v3 palette = %v", h.Palette)
	}
	for i, rgb := range []uint32{
		0x151515, 0xac4142, 0x7e8e50, 0xe5b567, 0x6c99bb, 0x9f4e85, 0x7dd6cf, 0xd0d0d0,
		0x505050, 0xac4142, 0x7e8e50, 0xe5b567, 0x6c99bb, 0x9f4e85, 0x7dd6cf, 0xf5f5f5,
	} {
		want.Palette[i] = hexNRGBA(rgb)
	}
	if h := tl.Header(); h != want {
		t.Fatalf("v3 Header = %+v", h)
	}
	// The file's intervals are in microseconds. Their sums are rounded to
	// the millisecond, so they do not drift: the last is 6.643958s.
	assertEvents(t, tl,
		`249 output "\x1b[1;31mHello \x1b[32mWorld!\x1b[0m\n"`,
		`1250 output "That was ok\rThis is better."`,
		`1750 mark `,
		`1894 input "ls\r"`,
		`2094 output "file1  file2\r\n"`,
		// The unknown events' intervals still count.
		`5144 resize 90x30`,
		`6144 mark chapter 2`,
		`6644 exit 0`,
	)

	// And from the v2 spec, with an 8 color palette and an event out of
	// order, which is raised to the one before.
	tl = readCastFile(t, "spec-v2.cast")
	want = Header{Cols: 80, Rows: 24, Title: "Demo", Theme: theme}
	copy(want.Palette[:8], []color.Color{
		hexNRGBA(0x151515), hexNRGBA(0xac4142), hexNRGBA(0x7e8e50), hexNRGBA(0xe5b567),
		hexNRGBA(0x6c99bb), hexNRGBA(0x9f4e85), hexNRGBA(0x7dd6cf), hexNRGBA(0xd0d0d0),
	})
	if h := tl.Header(); h != want {
		t.Fatalf("v2 Header = %+v", h)
	}
	assertEvents(t, tl,
		`249 output "\x1b[1;31mHello \x1b[32mWorld!\x1b[0m\n"`,
		`1250 output "That was ok\rThis is better."`,
		`1750 mark intro`,
		`1894 input "ls\r"`,
		`1894 output "out of order"`,
		`3000 resize 90x30`,
	)
}

func TestReadTimelineLenient(t *testing.T) {
	t.Parallel()
	in := "\ufeff" + `{"version":3,"term":{"cols":3,"rows":2,"theme":{"fg":"#000000","bg":"#ffffff","palette":""}},"env":{"N":1},"x":[1]}` + "\r\n" +
		`[0.5,"z",{"not":"a string"}]` + "\r\n" +
		`[0.5,"t","fg=#010203,bg=,shadow=#ffffff"]` + "\r\n" +
		`  ` + "\n" +
		`[1e-3,"o","no newline at the end"]`
	tl := readCast(t, []byte(in))
	if h := tl.Header(); h.Cols != 3 || h.Rows != 2 || h.Theme.Fg != (color.NRGBA{A: 0xff}) {
		t.Fatalf("Header = %+v", h)
	}
	assertEvents(t, tl,
		`1000 theme {1 2 3 255} <nil> <nil>`,
		`1001 output "no newline at the end"`,
	)
	if !tl.start.IsZero() {
		t.Fatalf("start = %v without a timestamp", tl.start)
	}
}

func TestReadTimelineErrors(t *testing.T) {
	t.Parallel()
	const header = `{"version":3,"term":{"cols":80,"rows":24}}` + "\n"
	tests := []struct {
		name, in, want string
	}{
		{"empty", "", "line 1: no header"},
		{"v1", "{\n  \"version\": 1,\n  \"width\": 80,\n  \"stdout\": [[0.1, \"a\"]]\n}\n", "asciicast v1 is not supported"},
		{"v1 version last", "{\n  \"width\": 80,\n  \"stdout\": [[0.1, \"a\"]],\n  \"version\": 1\n}\n", "asciicast v1 is not supported"},
		{"v1 on one line", `{"version":1,"width":80,"height":24,"stdout":[]}`, "asciicast v1 is not supported"},
		{"v4", `{"version":4}`, "unsupported asciicast version 4"},
		{"no version", `{"term":{"cols":80,"rows":24}}`, "unsupported asciicast version 0"},
		{"not json", "hello\n", "line 1: header"},
		{"no size", `{"version":3}`, "line 1: header: terminal size 0x0"},
		{"v2 no size", `{"version":2,"term":{"cols":80,"rows":24}}`, "terminal size 0x0"},
		{"bad theme", `{"version":3,"term":{"cols":1,"rows":1,"theme":{"fg":"red","bg":"#000000","palette":""}}}`, `theme fg: color "red"`},
		{"bad json", header + "[0.1, \"o\", \"a\"]\n[0.1, \"o\"\n", "line 3: jsontext: unexpected EOF"},
		{"object", header + `{"time": 1}`, "line 2: event is not a JSON array"},
		{"two fields", header + `[0.1, "o"]`, "line 2: event has 2 fields"},
		{"four fields", header + `[0.1, "o", "a", "b"]`, "line 2: event has 4 fields"},
		{"string time", header + `["0.1", "o", "a"]`, `line 2: event time "0.1" is not a number`},
		{"null time", header + `[null, "o", "a"]`, "line 2: event time null is not a number"},
		{"negative time", header + `[-1, "o", "a"]`, "line 2: event time -1 is out of range"},
		{"huge time", header + `[1e300, "o", "a"]`, "out of range"},
		{"over a year", header + `[31536001, "o", "a"]`, "line 2: event time 3.1536001e+07 is out of range"},
		{"no code", header + `[1, "", "a"]`, "line 2: event has no code"},
		{"number code", header + `[1, 5, "a"]`, "line 2: event code"},
		{"number data", header + `[1, "o", 5]`, `line 2: "o" event: data 5 is not a string`},
		{"null data", header + `[1, "m", null]`, `line 2: "m" event: data null is not a string`},
		{"time overflow", header + `[3e7, "o", "a"]` + "\n" + `[3e7, "o", "a"]` + "\n", "line 3: event time is out of range"},
		{"huge size", `{"version":3,"term":{"cols":65536,"rows":24}}`, "terminal size 65536x24"},
		{"signed resize", header + `[1, "r", "+80x24"]`, "is not COLSxROWS"},
		{"huge resize", header + `[1, "r", "80x65536"]`, "is not COLSxROWS"},
		{"bad palette", `{"version":3,"term":{"cols":1,"rows":1,"theme":{"fg":"#000000","bg":"#000000","palette":"bad"}}}`, `theme palette has 1 colors`},
		{"bad palette color", `{"version":3,"term":{"cols":1,"rows":1,"theme":{"fg":"#000000","bg":"#000000","palette":"#000000:#000000:#000000:#000000:#000000:#000000:#000000:red"}}}`, `color "red"`},
		{"bad theme bg", `{"version":3,"term":{"cols":1,"rows":1,"theme":{"fg":"#000000","bg":"","palette":""}}}`, `theme bg: color ""`},
		{"bad theme cursor", `{"version":3,"term":{"cols":1,"rows":1,"theme":{"fg":"#000000","bg":"#000000","palette":"","cursor":"#00"}}}`, `theme cursor: color "#00"`},
		{"bad resize", header + "# comment\n\n" + `[1, "r", "80"]`, `line 4: "r" event: size "80" is not COLSxROWS`},
		{"zero resize", header + `[1, "r", "0x24"]`, "is not COLSxROWS"},
		{"bad exit", header + `[1, "x", "ok"]`, `"x" event: status "ok" is not a number`},
		{"bad focus", header + `[1, "f", "yes"]`, `"f" event: focus "yes" is not 0 or 1`},
		{"bad theme event", header + `[1, "t", "fg"]`, `"t" event: "fg" is not key=#rrggbb`},
		{"bad theme color", header + `[1, "t", "bg=#12345g"]`, `"t" event: bg: color "#12345g"`},
	}
	for _, tt := range tests {
		_, err := ReadTimeline(strings.NewReader(tt.in))
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want %q", tt.name, err, tt.want)
		}
	}
}

type failingIO struct{}

func (failingIO) Read([]byte) (int, error)  { return 0, errors.New("boom") }
func (failingIO) Write([]byte) (int, error) { return 0, errors.New("boom") }

func TestCastIOErrors(t *testing.T) {
	t.Parallel()
	if _, err := ReadTimeline(failingIO{}); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("ReadTimeline err = %v, want the reader's", err)
	}
	if n, err := testTimeline(t).WriteTo(failingIO{}); err == nil || n != 0 {
		t.Fatalf("WriteTo = %d, %v, want the writer's error", n, err)
	}
}

func TestCastBetween(t *testing.T) {
	t.Parallel()
	tl := newTimeline(Header{Cols: 80, Rows: 24})
	tl.start = time.Unix(1700000000, 0)
	tl.appendAt(Output{Data: []byte("setup")}, 0)
	tl.appendAt(Mark{Label: "a"}, time.Second)
	tl.appendAt(Output{Data: []byte("shown")}, 2*time.Second)
	part, err := tl.Between("a", "")
	if err != nil {
		t.Fatal(err)
	}
	// The cut starts at its window, with a redraw of what came before.
	got := readCast(t, writeCast(t, part, CastV3))
	if got.start.Unix() != 1700000001 {
		t.Fatalf("start = %v, want the window's start", got.start)
	}
	events := slices.Collect(got.Events())
	if len(events) != 3 {
		t.Fatalf("events = %v, want the redraw and the window", describe(events))
	}
	if o, ok := events[0].(Output); !ok || o.At() != 0 || !bytes.Contains(o.Data, []byte("setup")) {
		t.Fatalf("first event = %v, want a redraw of the setup at 0", describe(events[:1]))
	}
	assertEvents(t, got.fixed(got.header, -1).cutAt(1), `0 mark a`, `1000 output "shown"`)

	// A cut from the first event has nothing to redraw.
	part, err = tl.Between("", "a")
	if err != nil {
		t.Fatal(err)
	}
	assertEvents(t, readCast(t, writeCast(t, part, CastV2)), `0 output "setup"`, `1000 mark a`)
}

// cutAt returns t without its first n events, for comparing the rest.
func (t *Timeline) cutAt(n int) *Timeline {
	return &Timeline{header: t.header, events: t.events[n:], from: -1}
}

// TestCastBetweenReplays checks that a cut written as a file replays to the
// terminal the cut shows: at its start, from the redraw, and at its end.
func TestCastBetweenReplays(t *testing.T) {
	t.Parallel()
	tl := newTimeline(Header{Cols: 30, Rows: 5, Theme: Colors{Fg: castFg, Bg: castBg}})
	for _, ev := range []Event{
		Output{event: at(0), Data: []byte("\x1b]2;title\x07\x1b[1;31mred\x1b[0m line\r\n\x1b[4munder\x1b[0m")},
		Resize{event: at(10), Cols: 32, Rows: 6},
		Theme{event: at(20), Colors: Colors{Bg: castCursor}},
		Output{event: at(30), Data: []byte("\r\n\x1b]9;4;3\x07\x1b[?2004h\x1b[?25l\x1b[3 q\x1b[3;7Hcursor")},
		Focus{event: at(40)},
		Mark{event: at(50), Label: "cut"},
		Output{event: at(60), Data: []byte("\x1b[?25h more\x1b]9;4;0\x07")},
		Focus{event: at(70), On: true},
		// Clearing the host colors in the window falls back to the
		// header's, not to those the cut starts with.
		Theme{event: at(75)},
		Output{event: at(80), Data: []byte("\x1b[?1049h\x1b[Halt")},
	} {
		tl.appendAt(ev, ev.At())
	}
	cut, err := tl.Between("cut", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []CastVersion{CastV3, CastV2} {
		back := readCast(t, writeCast(t, cut, v))
		// The redraw, then in v3 the theme and focus events. v2 has neither,
		// so the source is compared without them.
		redraw, src := 3, tl.events
		if v == CastV2 {
			redraw = 1
			src = slices.DeleteFunc(slices.Clone(src), func(ev Event) bool {
				switch ev.(type) {
				case Focus, Theme:
					return true
				}
				return false
			})
		}
		// At the start, the source replays the events before the window,
		// and the file its redraw.
		before := slices.IndexFunc(src, func(ev Event) bool { m, ok := ev.(Mark); return ok && m.Label == "cut" })
		assertSameReplay(t, tl.header, src[:before], back.header, back.events[:redraw])
		assertSameReplay(t, tl.header, src, back.header, back.events)
	}
}

// assertSameReplay fails unless replaying a and b leaves the same terminal.
func assertSameReplay(t *testing.T, ha Header, a []Event, hb Header, b []Event) {
	t.Helper()
	replayAll := func(h Header, events []Event) (vt.State, string) {
		r, err := newReplay(t.Context(), replayRuntime, h, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer r.close()
		for _, ev := range events {
			if err = r.apply(ev); err != nil {
				t.Fatal(err)
			}
		}
		st := r.state()
		st.Generation, st.ProgramStatusGeneration = 0, 0
		var cells strings.Builder
		scr := r.term.Screen()
		for y := range scr.Height() {
			for x := range scr.Width() {
				c := scr.CellAt(x, y)
				fmt.Fprintf(&cells, "%q%+v|", c.Content, c.Style)
			}
			cells.WriteByte('\n')
		}
		return st, cells.String()
	}
	sa, ca := replayAll(ha, a)
	sb, cb := replayAll(hb, b)
	if !sa.Equal(sb) {
		t.Errorf("state:\n%+v\nwant:\n%+v", sb, sa)
	}
	if ca != cb {
		t.Errorf("screen:\n%s\nwant:\n%s", cb, ca)
	}
}

func FuzzReadTimeline(f *testing.F) {
	names, err := filepath.Glob("testdata/*.cast")
	if err != nil {
		f.Fatal(err)
	}
	for _, name := range names {
		data, rerr := os.ReadFile(name)
		if rerr != nil {
			f.Fatal(rerr)
		}
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		tl, rerr := ReadTimeline(bytes.NewReader(data))
		if rerr != nil {
			return
		}
		// Whatever reads, writes a file that reads back as itself.
		for _, v := range []CastVersion{CastV3, CastV2} {
			first := writeCast(t, tl, v)
			if again := writeCast(t, readCast(t, first), v); !bytes.Equal(again, first) {
				t.Fatalf("v%d rewrite differs:\n%s\nthen:\n%s", v, first, again)
			}
		}
	})
}

func TestCastPalette(t *testing.T) {
	t.Parallel()
	h := Header{Cols: 2, Rows: 1, Theme: Colors{Fg: castFg, Bg: castBg}}
	h.Palette[1] = hexNRGBA(0xff0000)
	h.Palette[12] = hexNRGBA(0x0000aa)
	tl := newTimeline(h)
	for _, v := range []CastVersion{CastV3, CastV2} {
		data := writeCast(t, tl, v)
		// Nil entries are written as the defaults.
		if !bytes.Contains(data, []byte(`"palette":"#000000:#ff0000:#00cd00:`)) ||
			!bytes.Contains(data, []byte(`:#ffff00:#0000aa:#ff00ff:`)) {
			t.Fatalf("v%d header: %s", v, data)
		}
		got := readCast(t, data).Header()
		if want := withDefaultPalette(h); got != want {
			t.Fatalf("v%d Header = %+v, want %+v", v, got, want)
		}
	}
}
