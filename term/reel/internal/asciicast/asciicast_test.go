// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package asciicast

import (
	"bytes"
	"errors"
	"image/color"
	"io"
	"strings"
	"testing"
	"time"
)

func gray(v uint8) color.NRGBA { return color.NRGBA{R: v, G: v, B: v, A: 0xff} }

func TestCarryRune(t *testing.T) {
	t.Parallel()
	box := "─" // 3 bytes.
	tests := []struct {
		name       string
		tail, data string
		more       bool
		want, left string
	}{
		{"whole", "", "a" + box, true, "a" + box, ""},
		{"split", "", "a" + box[:2], true, "a", box[:2]},
		{"finished", box[:2], box[2:] + "b", true, box + "b", ""},
		{"only part", box[:1], box[1:2], true, "", box[:2]},
		{"last", "", "a" + box[:2], false, "a" + box[:2], ""},
		{"not utf-8", "", "a\xff", true, "a\xff", ""},
	}
	for _, tt := range tests {
		s, left := CarryRune([]byte(tt.tail), []byte(tt.data), tt.more)
		if s != tt.want || string(left) != tt.left {
			t.Errorf("%s: CarryRune = %q, %q, want %q, %q", tt.name, s, left, tt.want, tt.left)
		}
	}
}

func TestFormatTime(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		d    time.Duration
		want string
	}{
		{0, "0.000"},
		{time.Millisecond, "0.001"},
		{1500*time.Millisecond + 9, "1.500"},
		{12*time.Second + 345*time.Millisecond, "12.345"},
	} {
		if got := formatTime(tt.d); string(got) != tt.want {
			t.Errorf("formatTime(%v) = %s, want %s", tt.d, got, tt.want)
		}
	}
}

func TestWriter(t *testing.T) {
	t.Parallel()
	var p [16]color.Color
	for i := range p {
		p[i] = gray(uint8(i))
	}
	h := Header{
		Cols: 2, Rows: 1, Term: "xterm-test", Timestamp: time.Unix(1700000000, 0), Title: "t",
		Theme:        &Theme{Colors: Colors{Fg: gray(0xd0), Bg: gray(0x21), Cursor: gray(0xff)}, Palette: p},
		SkipPristine: true,
	}
	events := []Event{
		{Time: 0, Code: "o", Data: "a<b>"},
		{Time: 1001 * time.Millisecond, Code: "i", Data: "\r"},
		{Time: 1500 * time.Millisecond, Code: "m", Data: "m"},
	}
	const palette = "#000000:#010101:#020202:#030303:#040404:#050505:#060606:#070707:" +
		"#080808:#090909:#0a0a0a:#0b0b0b:#0c0c0c:#0d0d0d:#0e0e0e:#0f0f0f"
	for v, want := range map[int]string{
		3: `{"version":3,"term":{"cols":2,"rows":1,"type":"xterm-test","theme":` +
			`{"fg":"#d0d0d0","bg":"#212121","palette":"` + palette + `","cursor":"#ffffff"}},` +
			`"timestamp":1700000000,"title":"t","skip_pristine":true}
[0.000,"o","a<b>"]
[1.001,"i","\r"]
[0.499,"m","m"]
`,
		// v2 has offsets rather than intervals, and no place for the cursor
		// color or skip_pristine.
		2: `{"version":2,"width":2,"height":1,"timestamp":1700000000,"title":"t",` +
			`"env":{"TERM":"xterm-test"},"theme":{"fg":"#d0d0d0","bg":"#212121","palette":"` + palette + `"}}
[0.000,"o","a<b>"]
[1.001,"i","\r"]
[1.500,"m","m"]
`,
	} {
		h.Version = v
		var buf bytes.Buffer
		w, err := NewWriter(&buf, h)
		if err != nil {
			t.Fatal(err)
		}
		for _, ev := range events {
			if err = w.WriteEvent(ev); err != nil {
				t.Fatal(err)
			}
		}
		if got := buf.String(); got != want {
			t.Errorf("v%d:\n%s\nwant:\n%s", v, got, want)
		}
	}
	if _, err := NewWriter(io.Discard, Header{Version: 1}); err == nil {
		t.Error("NewWriter of v1 succeeded")
	}
}

func TestReader(t *testing.T) {
	t.Parallel()
	in := "\ufeff" + `{"version":3,"term":{"cols":3,"rows":2,"theme":{"fg":"#000000","bg":"#ffffff","palette":"` +
		`#000000:#010101:#020202:#030303:#040404:#050505:#060606:#070707"}},"timestamp":5,"x":[1]}` + "\r\n" +
		"# a comment\n" +
		`[0.5,"z",{"not":"a string"}]` + "\r\n" +
		"  \n" +
		`[0.25,"o","a"]` + "\n" +
		`[1e-3,"m","no newline at the end"]`
	r, err := NewReader(strings.NewReader(in), "o", "m")
	if err != nil {
		t.Fatal(err)
	}
	h := r.Header
	if h.Version != 3 || h.Cols != 3 || h.Rows != 2 || !h.Timestamp.Equal(time.Unix(5, 0)) || h.Theme == nil {
		t.Fatalf("Header = %+v", h)
	}
	if h.Theme.Fg != gray(0) || h.Theme.Bg != gray(0xff) || h.Theme.Cursor != nil ||
		h.Theme.Palette[7] != gray(7) || h.Theme.Palette[8] != nil {
		t.Fatalf("Theme = %+v", *h.Theme)
	}
	// The unknown event's interval counts.
	for _, want := range []struct {
		ev   Event
		line int
	}{
		{Event{Time: 750 * time.Millisecond, Code: "o", Data: "a"}, 5},
		{Event{Time: 751 * time.Millisecond, Code: "m", Data: "no newline at the end"}, 6},
	} {
		ev, nerr := r.Next()
		if nerr != nil || ev != want.ev || r.Line() != want.line {
			t.Fatalf("Next = %+v, %v at line %d, want %+v at line %d", ev, nerr, r.Line(), want.ev, want.line)
		}
	}
	if _, err = r.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("Next at the end = %v, want EOF", err)
	}

	// v2 times are offsets.
	r, err = NewReader(strings.NewReader(`{"version":2,"width":1,"height":1}`+"\n"+`[2,"o","a"]`+"\n"+`[1,"o","b"]`), "o")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []time.Duration{2 * time.Second, time.Second} {
		if ev, nerr := r.Next(); nerr != nil || ev.Time != want {
			t.Fatalf("v2 Next = %+v, %v, want time %v", ev, nerr, want)
		}
	}
}

func TestReaderV1(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"{\n  \"version\": 1,\n  \"width\": 80,\n  \"stdout\": [[0.1, \"a\"]]\n}\n",
		"{\n  \"width\": 80,\n  \"stdout\": [[0.1, \"a\"]],\n  \"version\": 1\n}\n",
		`{"version":1,"width":80,"height":24,"stdout":[]}`,
	} {
		if _, err := NewReader(strings.NewReader(in)); !errors.Is(err, errV1) {
			t.Errorf("NewReader(%q) = %v, want the v1 error", in, err)
		}
	}
	if _, err := NewReader(strings.NewReader("{\n  \"version\": 2,\n")); errors.Is(err, errV1) {
		t.Error("a broken v2 header read as v1")
	}
}

func TestColors(t *testing.T) {
	t.Parallel()
	c := Colors{Fg: gray(1), Cursor: gray(0xff)}
	data := FormatColors(c)
	if data != "fg=#010101,cursor=#ffffff" {
		t.Fatalf("FormatColors = %q", data)
	}
	if got, err := ParseColors(data); err != nil || got != c {
		t.Fatalf("ParseColors(%q) = %+v, %v", data, got, err)
	}
	if got, err := ParseColors("bg=,shadow=#ffffff,fg=#020202"); err != nil || got != (Colors{Fg: gray(2)}) {
		t.Fatalf("ParseColors ignoring keys = %+v, %v", got, err)
	}
	for data, want := range map[string]string{
		"fg":         `"fg" is not key=#rrggbb`,
		"bg=#12345g": `bg: color "#12345g" is not #rrggbb`,
		"cursor=red": `cursor: color "red" is not #rrggbb`,
	} {
		if _, err := ParseColors(data); err == nil || err.Error() != want {
			t.Errorf("ParseColors(%q) = %v, want %q", data, err, want)
		}
	}
}

func TestPalette(t *testing.T) {
	t.Parallel()
	var p [16]color.Color
	for i := range p {
		p[i] = color.NRGBA{R: uint8(i) * 0x10, A: 0x80}
	}
	// Alpha is dropped.
	s := formatPalette(p)
	if !strings.HasPrefix(s, "#000000:#100000:") || strings.Count(s, ":") != 15 {
		t.Fatalf("formatPalette = %q", s)
	}
	got, err := parsePalette(s)
	if err != nil || got[1] != (color.NRGBA{R: 0x10, A: 0xff}) {
		t.Fatalf("parsePalette = %v, %v", got, err)
	}
	if got, err = parsePalette(""); err != nil || got != ([16]color.Color{}) {
		t.Fatalf("parsePalette of none = %v, %v", got, err)
	}
	if _, err = parsePalette("#000000:#000000"); err == nil || err.Error() != "palette has 2 colors, want 8 or 16" {
		t.Fatalf("parsePalette of 2 = %v", err)
	}
}

func TestParseSize(t *testing.T) {
	t.Parallel()
	for s, want := range map[string]bool{
		"1": true, "80": true, "65535": true,
		"0": false, "-1": false, "+80": false, "65536": false, "8x": false, "": false,
	} {
		if _, ok := ParseSize(s); ok != want {
			t.Errorf("ParseSize(%q) ok = %v, want %v", s, ok, want)
		}
	}
}
