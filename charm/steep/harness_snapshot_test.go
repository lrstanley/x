// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"path/filepath"
	"testing"

	"github.com/lrstanley/x/charm/steep/snapshot"
)

func TestHarnessAssertViewSnapshot(t *testing.T) {
	t.Parallel()

	snapDir := filepath.Join(t.TempDir(), "testdata")

	h := NewHarness(t, rootTestModel{text: "assert-snap"}, WithWindowSize(80, 3))

	h.WaitStrings([]string{"size=80x3", "text=assert-snap"})
	h.AssertSnapshot(snapshot.WithDir(snapDir), snapshot.WithUpdate(true))

	got := readSteepSnapshot(t, filepath.Join(snapDir, "TestHarnessAssertViewSnapshot.snap"))
	if got != "size=80x3\ntext=assert-snap\n" {
		t.Fatalf("snapshot = %q, want plain view text", got)
	}
}
