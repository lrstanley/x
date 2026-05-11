// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package snapshot

import (
	"encoding/json"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func TestColorMarshalUnmarshalText(t *testing.T) {
	t.Parallel()

	t.Run("nil", func(t *testing.T) {
		t.Parallel()
		var c Color
		b, err := c.MarshalText()
		if err != nil {
			t.Fatal(err)
		}
		if len(b) != 0 {
			t.Fatalf("MarshalText(nil) = %q, want empty", b)
		}
		var round Color
		if err := round.UnmarshalText(b); err != nil {
			t.Fatal(err)
		}
		if round.Color != nil {
			t.Fatalf("Unmarshal empty: Color = %v, want nil", round.Color)
		}
	})

	t.Run("basic", func(t *testing.T) {
		t.Parallel()
		c := Color{Color: ansi.BasicColor(9)}
		b, err := c.MarshalText()
		if err != nil {
			t.Fatal(err)
		}
		var got Color
		if err := got.UnmarshalText(b); err != nil {
			t.Fatal(err)
		}
		if got.Color != ansi.BasicColor(9) {
			t.Fatalf("got %v, want basic 9", got.Color)
		}
	})

	t.Run("indexed", func(t *testing.T) {
		t.Parallel()
		c := Color{Color: ansi.IndexedColor(199)}
		b, err := c.MarshalText()
		if err != nil {
			t.Fatal(err)
		}
		var got Color
		if err := got.UnmarshalText(b); err != nil {
			t.Fatal(err)
		}
		if got.Color != ansi.IndexedColor(199) {
			t.Fatalf("got %v, want indexed 199", got.Color)
		}
	})

	t.Run("rgb", func(t *testing.T) {
		t.Parallel()
		c := Color{Color: color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff}}
		b, err := c.MarshalText()
		if err != nil {
			t.Fatal(err)
		}
		const want = "#123456"
		if string(b) != want {
			t.Fatalf("MarshalText = %q, want %q", b, want)
		}
		var got Color
		if err := got.UnmarshalText(b); err != nil {
			t.Fatal(err)
		}
		r1, g1, b1, _ := c.Color.RGBA()
		r2, g2, b2, _ := got.Color.RGBA()
		if r1 != r2 || g1 != g2 || b1 != b2 {
			t.Fatalf("round-trip RGB drift: got %v", got.Color)
		}
	})
}

func TestColorUnmarshalTextInvalid(t *testing.T) {
	t.Parallel()

	var c Color
	err := c.UnmarshalText([]byte("not-a-color%%%"))
	if err == nil {
		t.Fatal("expected error for invalid color text")
	}
}

func TestCellFromUV(t *testing.T) {
	t.Parallel()

	t.Run("nil_cell", func(t *testing.T) {
		t.Parallel()
		if got := (&Cell{}).FromUV(nil); got != nil {
			t.Fatalf("FromUV(nil) = %v, want nil", got)
		}
	})

	t.Run("populated", func(t *testing.T) {
		t.Parallel()
		in := &uv.Cell{
			Content: "Ω",
			Width:   1,
			Style: uv.Style{
				Fg:             ansi.BasicColor(3),
				Bg:             ansi.IndexedColor(42),
				UnderlineColor: color.NRGBA{R: 1, G: 2, B: 3, A: 255},
				Underline:      uv.UnderlineSingle,
				Attrs:          0x55,
			},
			Link: uv.Link{URL: "https://example.test", Params: "id=1"},
		}
		got := (&Cell{}).FromUV(in)
		if got.Content != "Ω" || got.Width != 1 {
			t.Fatalf("Content/Width = %q / %d", got.Content, got.Width)
		}
		if got.Link.URL != "https://example.test" || got.Link.Params != "id=1" {
			t.Fatalf("Link = %+v", got.Link)
		}
		if got.Style.Underline != uv.UnderlineSingle || got.Style.Attrs != 0x55 {
			t.Fatalf("Style underline/attrs = %v / %x", got.Style.Underline, got.Style.Attrs)
		}
		if fg, ok := got.Style.Fg.Color.(ansi.BasicColor); !ok || fg != 3 {
			t.Fatalf("Fg = %T %v", got.Style.Fg.Color, got.Style.Fg.Color)
		}
		if bg, ok := got.Style.Bg.Color.(ansi.IndexedColor); !ok || bg != 42 {
			t.Fatalf("Bg = %T %v", got.Style.Bg.Color, got.Style.Bg.Color)
		}
	})
}

func TestScreenSnapshotCompare(t *testing.T) {
	t.Parallel()

	a := &ScreenSnapshot{Rows: 1, Cols: 2, Title: "one", Cells: [][]*Cell{{nil, {Content: "x", Width: 1}}}}
	b := &ScreenSnapshot{Rows: 1, Cols: 2, Title: "one", Cells: [][]*Cell{{nil, {Content: "x", Width: 1}}}}
	if !a.Compare(t, b) {
		t.Fatal("expected equal snapshots")
	}

	b.Title = "two"
	if a.Compare(t, b) {
		t.Fatal("expected snapshots to differ")
	}
}

func TestWithScreenBuffer(t *testing.T) {
	t.Parallel()

	buf := AsScreenBuffer(t, "ab\ncd", WithDir(filepath.Join(t.TempDir(), "testdata")))
	ss := (&ScreenSnapshot{}).WithScreenBuffer(t, buf)
	if ss.Rows != 2 || ss.Cols != 2 {
		t.Fatalf("dimensions = %dx%d, want 2x2", ss.Cols, ss.Rows)
	}
	want := [][]string{{"a", "b"}, {"c", "d"}}
	for y := range 2 {
		for x := range 2 {
			cell := ss.Cells[y][x]
			if cell == nil {
				t.Fatalf("cells[%d][%d] is nil", y, x)
			}
			if cell.Content != want[y][x] {
				t.Fatalf("cells[%d][%d].Content = %q, want %q", y, x, cell.Content, want[y][x])
			}
		}
	}
}

func TestAsScreenBufferDimensions(t *testing.T) {
	t.Parallel()

	buf := AsScreenBuffer(t, "hello")
	if buf.Width() != 5 || buf.Height() != 1 {
		t.Fatalf("Bounds = %dx%d, want 5x1", buf.Width(), buf.Height())
	}
}

func TestRequireScreenEqualUsesExistingSnapshot(t *testing.T) {
	t.Parallel()

	snapDir := filepath.Join(t.TempDir(), "testdata")
	snap := screenSnapshotFromString(t, "yo\n", snapDir)
	writeScreenJSON(t, snapDir, "TestRequireScreenEqualUsesExistingSnapshot.json", snap)

	RequireScreenEqual(t, screenSnapshotFromString(t, "yo\n", snapDir), WithDir(snapDir))
}

func TestRequireScreenEqualCreatesSnapshot(t *testing.T) {
	t.Parallel()

	snapDir := filepath.Join(t.TempDir(), "testdata")
	RequireScreenEqual(t, screenSnapshotFromString(t, "line\n", snapDir), WithDir(snapDir), WithUpdate(true))

	path := filepath.Join(snapDir, "TestRequireScreenEqualCreatesSnapshot.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	var got ScreenSnapshot
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Rows < 1 || len(got.Cells) != got.Rows {
		t.Fatalf("invalid snapshot layout: %+v", got)
	}
}

func TestWriteScreenExplicitPath(t *testing.T) {
	t.Parallel()

	snapDir := filepath.Join(t.TempDir(), "testdata")
	if err := os.MkdirAll(snapDir, 0o750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(snapDir, "out.json")
	want := screenSnapshotFromString(t, "z\n", snapDir)
	WriteScreen(t, want, path, WithUpdate(true))

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got ScreenSnapshot
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !want.Compare(t, &got) {
		t.Fatal("written snapshot does not round-trip Compare with original")
	}
}

func screenSnapshotFromString(tb testing.TB, v string, dir string) *ScreenSnapshot {
	tb.Helper()
	return (&ScreenSnapshot{}).WithScreenBuffer(tb, AsScreenBuffer(tb, v, WithDir(dir)))
}

func writeScreenJSON(tb testing.TB, dir, name string, snap *ScreenSnapshot) {
	tb.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		tb.Fatalf("mkdir: %v", err)
	}
	writeScreen(tb, snap, path)
}
