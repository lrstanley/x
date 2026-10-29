// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package snapshot

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/lrstanley/x/term/reel"
)

func TestCompareScreens(t *testing.T) {
	t.Parallel()

	a := &reel.Screen{Rows: 1, Cols: 2, Title: "one", Cells: [][]*reel.ScreenCell{{nil, {Content: "x", Width: 1}}}}
	b := &reel.Screen{Rows: 1, Cols: 2, Title: "one", Cells: [][]*reel.ScreenCell{{nil, {Content: "x", Width: 1}}}}
	if !compareScreens(t, a, b) {
		t.Fatal("expected equal snapshots")
	}

	b.Title = "two"
	if compareScreens(t, a, b) {
		t.Fatal("expected snapshots to differ")
	}
}

func TestNewScreenFromText(t *testing.T) {
	t.Parallel()

	buf := AsScreenBuffer(t, "ab\ncd", WithDir(filepath.Join(t.TempDir(), "testdata")))
	ss := reel.NewScreen(buf)
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
	var got reel.Screen
	err = json.Unmarshal(data, &got)
	if err != nil {
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
	var got reel.Screen
	err = json.Unmarshal(data, &got)
	if err != nil {
		t.Fatal(err)
	}
	if !compareScreens(t, want, &got) {
		t.Fatal("written snapshot does not round-trip Compare with original")
	}
}

func screenSnapshotFromString(tb testing.TB, v, dir string) *reel.Screen {
	tb.Helper()
	return reel.NewScreen(AsScreenBuffer(tb, v, WithDir(dir)))
}

func writeScreenJSON(tb testing.TB, dir, name string, snap *reel.Screen) {
	tb.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		tb.Fatalf("mkdir: %v", err)
	}
	writeScreen(tb, snap, path)
}
