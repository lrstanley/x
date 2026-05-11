// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package snapshot

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequireEqualCreatesSnapshot(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(envUpdateSnapshots, "true")

	RequireEqual(t, "hello\r\nworld\n")

	got := readSnapshot(t, "TestRequireEqualCreatesSnapshot.snap")
	if got != "hello\nworld\n" {
		t.Fatalf("snapshot = %q, want %q", got, "hello\nworld\n")
	}
}

func TestRequireEqualUsesExistingSnapshot(t *testing.T) {
	t.Chdir(t.TempDir())
	writeSnapshot(t, "TestRequireEqualUsesExistingSnapshot.snap", "hello\nworld\n")

	RequireEqual(t, []byte("hello\r\nworld\n"))
}

func TestRequireEqualNamedAppendsSuffix(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(envUpdateSnapshots, "true")

	RequireEqual(t, "hello\n", WithSuffix("case name"))

	got := readSnapshot(t, "TestRequireEqualNamedAppendsSuffix-case-name.snap")
	if got != "hello\n" {
		t.Fatalf("snapshot = %q, want %q", got, "hello\n")
	}
}

func TestRequireEqualLoopCounters(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(envUpdateSnapshots, "true")

	for i := range 3 {
		RequireEqual(t, fmt.Sprintf("snapshot %d\n", i))
	}

	tests := map[string]string{
		"TestRequireEqualLoopCounters.snap":    "snapshot 0\n",
		"TestRequireEqualLoopCounters.02.snap": "snapshot 1\n",
		"TestRequireEqualLoopCounters.03.snap": "snapshot 2\n",
	}
	for name, want := range tests {
		if got := readSnapshot(t, name); got != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestRequireEqualEscapesANSI(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(envUpdateSnapshots, "true")

	RequireEqual(t, "hello \x1b[31mred\x1b[0m\n")

	got := readSnapshot(t, "TestRequireEqualEscapesANSI.snap")
	if !strings.Contains(got, `\x1b[31mred\x1b[0m`) {
		t.Fatalf("snapshot does not contain escaped ANSI sequences: %q", got)
	}
}

func writeSnapshot(t *testing.T, name, content string) {
	t.Helper()

	path := filepath.Join("testdata", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("failed to create snapshot directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write snapshot: %v", err)
	}
}

func readSnapshot(t *testing.T, name string) string {
	t.Helper()

	bts, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("failed to read snapshot: %v", err)
	}
	return string(bts)
}
