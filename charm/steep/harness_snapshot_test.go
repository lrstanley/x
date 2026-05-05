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

	h := NewHarness(t, rootTestModel{text: "\x1b[31mred\x1b[0m"}, WithWindowSize(80, 3))

	h.WaitStrings([]string{"size=80x3", "red"})
	h.RequireViewSnapshotNoANSI()

	got := readSteepSnapshot(t, "TestHarnessRequirePlainSnapshotUsesCurrentOutput.snap")
	if got != "size=80x3\n         text=red\n" {
		t.Fatalf("snapshot = %q, want current plain output", got)
	}
}

func TestHarnessAssertViewSnapshot(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("UPDATE_SNAPSHOTS", "true")

	h := NewHarness(t, rootTestModel{text: "assert-snap"}, WithWindowSize(80, 3))

	h.WaitStrings([]string{"size=80x3", "text=assert-snap"})
	h.AssertViewSnapshot()

	got := readSteepSnapshot(t, "TestHarnessAssertViewSnapshot.snap")
	if got != "size=80x3\n         text=assert-snap\n" {
		t.Fatalf("snapshot = %q, want plain view text", got)
	}
}

func TestHarnessAssertViewSnapshotNoANSIWrapper(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("UPDATE_SNAPSHOTS", "true")

	h := NewHarness(t, rootTestModel{text: "\x1b[32mgreen\x1b[0m"}, WithWindowSize(80, 3))

	h.WaitStrings([]string{"size=80x3", "green"})
	h.AssertViewSnapshotNoANSI()

	got := readSteepSnapshot(t, "TestHarnessAssertViewSnapshotNoANSIWrapper.snap")
	if got != "size=80x3\n         text=green\n" {
		t.Fatalf("snapshot = %q, want ansi stripped", got)
	}
}
