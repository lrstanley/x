// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build unix

package pty

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const ghostty = "xterm-ghostty"

func TestEnv(t *testing.T) {
	t.Parallel()
	got := Env([]string{"A=1", "TERM=screen", "COLORTERM=no"}, true, ghostty)
	if got[0] != "A=1" || len(got) != 3 || !strings.HasPrefix(got[1], "TERM=") || got[1] == "TERM=screen" || got[2] != "COLORTERM=truecolor" {
		t.Fatalf("inherited env = %q, want the host's TERM and COLORTERM replaced", got)
	}
	if kept := Env([]string{"TERM=screen", "COLORTERM=no"}, false, ghostty); !slices.Equal(kept, []string{"TERM=screen", "COLORTERM=no"}) {
		t.Fatalf("env = %q, want TERM and COLORTERM kept", kept)
	}

	if hasTerminfo(terminfoPath([]string{"TERMINFO=" + t.TempDir()}), ghostty) {
		t.Fatal("found an entry in an empty directory")
	}
	for _, sub := range []string{"x", "78"} {
		dir, home := t.TempDir(), t.TempDir()
		fileTerminfo(t, dir, sub)
		fileTerminfo(t, filepath.Join(home, ".terminfo"), sub)
		for _, env := range [][]string{
			{"TERMINFO=" + dir},
			{"TERMINFO_DIRS=/nonexistent::" + dir},
			{"HOME=/nonexistent", "HOME=" + home},
		} {
			if !hasTerminfo(terminfoPath(env), ghostty) {
				t.Errorf("%s/%s: no entry found with %q", sub, ghostty, env)
			}
			if found := Env(env, false, ghostty); !slices.Contains(found, "TERM="+ghostty) {
				t.Errorf("%s/%s: Env(%q) = %q, want TERM=%s", sub, ghostty, env, found, ghostty)
			}
		}
	}
}

// fileTerminfo files an empty terminfo entry for xterm-ghostty in dir, under
// sub.
func fileTerminfo(t *testing.T, dir, sub string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, sub), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sub, ghostty), nil, 0o600); err != nil {
		t.Fatal(err)
	}
}
