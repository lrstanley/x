// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"testing"
)

func TestHarnessRequirePlainSnapshotUsesCurrentOutput(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("UPDATE_SNAPSHOTS", "true")

	h := NewHarness(t, rootTestModel{text: "\x1b[31mred\x1b[0m"})

	h.WaitStrings([]string{"size=80x24", "red"})
	h.RequireViewSnapshotNoANSI()

	got := readSteepSnapshot(t, "TestHarnessRequirePlainSnapshotUsesCurrentOutput.snap")
	if got != "size=80x24\ntext=red" {
		t.Fatalf("snapshot = %q, want current plain output", got)
	}
}
