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

func TestHarnessAssertViewSnapshot(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("UPDATE_SNAPSHOTS", "true")

	h := NewHarness(t, rootTestModel{text: "assert-snap"})

	h.WaitStrings([]string{"size=80x24", "text=assert-snap"})
	h.AssertViewSnapshot()

	got := readSteepSnapshot(t, "TestHarnessAssertViewSnapshot.snap")
	if got != "size=80x24\ntext=assert-snap" {
		t.Fatalf("snapshot = %q, want plain view text", got)
	}
}

func TestHarnessAssertViewSnapshotNoANSIWrapper(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("UPDATE_SNAPSHOTS", "true")

	h := NewHarness(t, rootTestModel{text: "\x1b[32mgreen\x1b[0m"})

	h.WaitStrings([]string{"size=80x24", "green"})
	h.AssertViewSnapshotNoANSI()

	got := readSteepSnapshot(t, "TestHarnessAssertViewSnapshotNoANSIWrapper.snap")
	if got != "size=80x24\ntext=green" {
		t.Fatalf("snapshot = %q, want ansi stripped", got)
	}
}
