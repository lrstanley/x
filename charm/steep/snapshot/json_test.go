// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package snapshot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRequireJSONEqualCreatesSnapshot(t *testing.T) {
	t.Parallel()

	snapDir := filepath.Join(t.TempDir(), "testdata")
	RequireJSONEqual(t, "hello\r\nworld\n", WithDir(snapDir), WithUpdate(true))

	data, err := os.ReadFile(filepath.Join(snapDir, "TestRequireJSONEqualCreatesSnapshot.json"))
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	var snap ScreenSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if snap.Rows < 2 || snap.Cols < 1 {
		t.Fatalf("unexpected dimensions rows=%d cols=%d", snap.Rows, snap.Cols)
	}
}

func TestRequireJSONEqualUsesExistingSnapshot(t *testing.T) {
	t.Parallel()

	snapDir := filepath.Join(t.TempDir(), "testdata")
	snap := screenSnapshotFromString(t, "ping\n", snapDir)
	writeScreenJSON(t, snapDir, "TestRequireJSONEqualUsesExistingSnapshot.json", snap)
	RequireJSONEqual(t, "ping\n", WithDir(snapDir))
}

func TestAssertJSONEqual(t *testing.T) {
	t.Parallel()

	snapDir := filepath.Join(t.TempDir(), "testdata")
	snap := screenSnapshotFromString(t, "ok\n", snapDir)
	writeScreenJSON(t, snapDir, "TestAssertJSONEqual.json", snap)
	if !AssertJSONEqual(t, "ok\n", WithDir(snapDir)) {
		t.Fatal("AssertJSONEqual should match existing snapshot")
	}
}

func TestAssertJSONEqualWithUpdate(t *testing.T) {
	t.Parallel()

	snapDir := filepath.Join(t.TempDir(), "testdata")
	if !AssertJSONEqual(t, "fresh\n", WithDir(snapDir), WithUpdate(true)) {
		t.Fatal("AssertJSONEqual with update should return true")
	}
	path := filepath.Join(snapDir, "TestAssertJSONEqualWithUpdate.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected snapshot file: %v", err)
	}
}

func TestWriteJSONDefaultPathSnapExtension(t *testing.T) {
	t.Parallel()

	snapDir := filepath.Join(t.TempDir(), "testdata")
	WriteJSON(t, "snap-check\n", "", WithDir(snapDir), WithUpdate(true))

	path := filepath.Join(snapDir, "TestWriteJSONDefaultPathSnapExtension.snap")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	var snap ScreenSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("content is not valid ScreenSnapshot JSON: %v", err)
	}
	if snap.Rows < 1 {
		t.Fatalf("expected at least one row")
	}
}

func TestWriteJSONExplicitPath(t *testing.T) {
	t.Parallel()

	snapDir := filepath.Join(t.TempDir(), "testdata")
	if err := os.MkdirAll(snapDir, 0o750); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(snapDir, "custom.json")
	WriteJSON(t, "explicit\n", out, WithUpdate(true))

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var snap ScreenSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Rows < 1 {
		t.Fatal("empty snapshot")
	}
}
