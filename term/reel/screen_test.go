// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"bytes"
	json "encoding/json/v2"
	"errors"
	"image/color"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/reel/vt"
)

func TestSessionScreen(t *testing.T) {
	t.Parallel()
	s := openTest(t, 4, 2)
	write(t, s, "\x1b]0;hi\x07\x1b[31mab\x1b[0m\r\nc")
	got := s.Screen()
	if got.Title != "hi" || got.Rows != 2 || got.Cols != 4 || !got.Focused {
		t.Fatalf("Screen = %+v", got)
	}
	if got.Cursor.Position != (ScreenPosition{X: 1, Y: 1}) || !got.Cursor.Visible {
		t.Fatalf("Cursor = %+v", got.Cursor)
	}
	if c := got.Cells[0][0]; c == nil || c.Content != "a" || c.Style.Fg.Color == nil {
		t.Fatalf("cell 0,0 = %+v", c)
	}

	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var back Screen
	if err = json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	again, err := json.Marshal(&back)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(again, data) {
		t.Fatalf("round trip:\n%s\nwant\n%s", again, data)
	}
}

func TestExportScreens(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(Header{Cols: 6, Rows: 1},
		outAt(0, "a"),
		Mark{event: at(100), Label: "input-1"},
		outAt(200, "\rb"),
		Mark{event: at(300), Label: "wait-1"},
		outAt(400, "\rsecret"),
		Mark{event: at(500), Label: "input-2"},
	)
	dir := t.TempDir()
	p := Profile{
		Outputs: MustFiles(
			filepath.Join(dir, "{mark}.snap?match=input-*&match=nope-*"),
			filepath.Join(dir, "end.snap"),
			filepath.Join(dir, "end.json"),
		),
		// Screens show what effects conceal.
		Effects: []Effect{concealer{on: true}},
	}
	if err := Export(t.Context(), tl, p); err != nil {
		t.Fatal(err)
	}
	read := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	for name, want := range map[string]string{"input-1.snap": "a", "input-2.snap": "secret", "end.snap": "secret"} {
		if got := strings.TrimRight(read(name), " \n"); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "wait-1.snap")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("wait-1.snap was written: %v", err)
	}
	var scr Screen
	if err := json.Unmarshal([]byte(read("end.json")), &scr); err != nil {
		t.Fatal(err)
	}
	if scr.Cols != 6 || scr.Cells[0][0] == nil || scr.Cells[0][0].Content != "s" {
		t.Fatalf("end.json = %+v", scr)
	}
}

func TestExportScreenMissingMark(t *testing.T) {
	t.Parallel()
	tl := buildTimeline(Header{Cols: 4, Rows: 1}, outAt(0, "a"), Mark{event: at(100), Label: "here"})
	dir := t.TempDir()
	p := Profile{Outputs: MustFiles(
		filepath.Join(dir, "{mark}.snap?marks=here&marks=gone"),
	)}
	missing, err := MissingMarks(Export(t.Context(), tl, p))
	if err != nil || !slices.Equal(missing, []string{"gone"}) {
		t.Fatalf("Export: missing %v, err %v", missing, err)
	}
	if _, err = os.Stat(filepath.Join(dir, "here.snap")); err != nil {
		t.Fatal(err)
	}
}

func TestMarkMatchErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tl := buildTimeline(Header{Cols: 4, Rows: 1}, outAt(0, "a"), Mark{event: at(100), Label: "in/1"})
	err := Export(t.Context(), tl, Profile{Outputs: []File{{Path: filepath.Join(dir, "{mark}.snap"), MarkMatch: []string{"in/*"}}}})
	if err == nil || !strings.Contains(err.Error(), "not a file name") {
		t.Fatalf("Export with a label holding a slash = %v", err)
	}
	for name, f := range map[string]File{
		"bad pattern": {Path: filepath.Join(dir, "{mark}.png"), MarkMatch: []string{"["}},
		"no {mark}":   {Path: filepath.Join(dir, "x.png"), MarkMatch: []string{"*"}},
		"cast":        {Path: filepath.Join(dir, "{mark}.cast"), MarkMatch: []string{"*"}},
		"options":     {Path: filepath.Join(dir, "x.snap"), Options: MustFiles("x.gif?loop=once")[0].Options},
	} {
		if err = Export(t.Context(), tl, Profile{Outputs: []File{f}}); err == nil {
			t.Errorf("%s: Export succeeded", name)
		}
	}

	s := openTest(t, 4, 1)
	if _, err = Record(t.Context(), s, Profile{Outputs: []File{{Path: filepath.Join(dir, "{mark}.png"), MarkMatch: []string{"*"}}}}); err == nil {
		t.Error("Record with MarkMatch succeeded")
	}
}

func TestRecordScreens(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := openTest(t, 4, 1)
	rec, err := Record(t.Context(), s, Profile{Outputs: MustFiles(
		filepath.Join(dir, "{mark}.snap?marks=one"),
		filepath.Join(dir, "end.snap"),
	)})
	if err != nil {
		t.Fatal(err)
	}
	write(t, s, "a")
	if err = s.Mark("one"); err != nil {
		t.Fatal(err)
	}
	write(t, s, "b")
	time.Sleep(10 * time.Millisecond)
	if err = rec.Stop(); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"one.snap": "a", "end.snap": "ab"} {
		data, rerr := os.ReadFile(filepath.Join(dir, name))
		if got := strings.TrimRight(string(data), " "); rerr != nil || got != want {
			t.Errorf("%s = %q, %v, want %q", name, got, rerr, want)
		}
	}
}

func TestParseScreenFiles(t *testing.T) {
	t.Parallel()
	files, err := ParseFiles(" a.gif , {mark}.snap?match=input-*&marks=x,, end.json ")
	if err != nil {
		t.Fatal(err)
	}
	want := []File{
		{Path: "a.gif"},
		{Path: "{mark}.snap", Marks: []string{"x"}, MarkMatch: []string{"input-*"}},
		{Path: "end.json"},
	}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("ParseFiles = %+v, want %+v", files, want)
	}
	for _, raw := range []string{
		"a.snap?loop=once",
		"a.json?optimize=all",
		"a.snap?match=x",
		"{mark}.snap?match=[",
		"{mark}.cast?match=x",
	} {
		if _, err = ParseFile(raw); err == nil {
			t.Errorf("ParseFile(%q) succeeded", raw)
		}
	}
}

func TestScreenColorText(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]color.Color{
		"nil":     nil,
		"basic":   ansi.BasicColor(9),
		"indexed": ansi.IndexedColor(199),
		"vt":      vt.Indexed(201),
		"rgb":     color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff},
	} {
		b, err := ScreenColor{c}.MarshalText()
		if err != nil {
			t.Fatal(err)
		}
		var got ScreenColor
		if err = got.UnmarshalText(b); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if name == "vt" {
			if string(b) != "201" || got.Color != ansi.IndexedColor(201) {
				t.Errorf("vt: %q read back as %#v", b, got.Color)
			}
			continue
		}
		if name == "rgb" {
			if string(b) != "#123456" || nrgba(got.Color) != c {
				t.Errorf("rgb: %q read back as %v", b, got.Color)
			}
			continue
		}
		if got.Color != c {
			t.Errorf("%s: %q read back as %#v, want %#v", name, b, got.Color, c)
		}
	}
	var c ScreenColor
	if err := c.UnmarshalText([]byte("not-a-color%%%")); err == nil {
		t.Fatal("UnmarshalText of an invalid color succeeded")
	}
}

func TestNewScreenCell(t *testing.T) {
	t.Parallel()
	buf := vt.NewBuffer(2, 1)
	buf.SetCell(1, 0, &vt.Cell{
		Content: "Ω",
		Width:   1,
		Style: vt.Style{
			Fg:             ansi.BasicColor(3),
			Bg:             ansi.IndexedColor(42),
			UnderlineColor: color.NRGBA{R: 1, G: 2, B: 3, A: 255},
			Underline:      vt.UnderlineSingle,
			Attrs:          0x55,
		},
		Link: vt.Link{URL: "https://example.test", Params: "id=1"},
	})
	got := NewScreen(buf).Cells[0][1]
	want := &ScreenCell{
		Content: "Ω",
		Width:   1,
		Style: CellStyle{
			Fg: ScreenColor{ansi.BasicColor(3)}, Bg: ScreenColor{ansi.IndexedColor(42)},
			UnderlineColor: ScreenColor{color.NRGBA{R: 1, G: 2, B: 3, A: 255}},
			Underline:      vt.UnderlineSingle, Attrs: 0x55,
		},
		Link: CellLink{URL: "https://example.test", Params: "id=1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cell = %+v, want %+v", got, want)
	}
	if s := NewScreen(nil); s.Rows != 0 || s.Cells != nil {
		t.Fatalf("NewScreen(nil) = %+v", s)
	}
}
