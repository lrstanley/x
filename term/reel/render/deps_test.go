// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// TestNoTestOnlyDependencies checks that the renderer links neither test-only
// packages (testing, Bubble Tea, steep) nor the vt emulator and its Wasm
// runtime. vt/screen, the cell buffer, is the only vt package it may link.
func TestNoTestOnlyDependencies(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go binary not available")
	}

	out := goList(t, "-deps", "-f", "{{.ImportPath}} {{with .Module}}{{.Path}}{{end}}", ".")

	// Each line is "<import path> <module path>", and -deps lists this package
	// last.
	lines := strings.Split(strings.TrimSpace(out), "\n")
	_, module, _ := strings.Cut(lines[len(lines)-1], " ")
	if module == "" {
		t.Fatalf("go list reported no module for render: %q", lines[len(lines)-1])
	}
	vt := module + "/vt"

	for _, line := range lines[:len(lines)-1] {
		dep, _, _ := strings.Cut(line, " ")
		if testOnlyDependency(dep) {
			t.Errorf("render depends on %s", dep)
		}
		if (dep == vt || strings.HasPrefix(dep, vt+"/")) && dep != vt+"/screen" {
			t.Errorf("render depends on %s; vt/screen is the only vt package it may link", dep)
		}
	}
}

// testOnlyDependency reports whether dep is a package only tests should link.
func testOnlyDependency(dep string) bool {
	for _, prefix := range []string{"testing", "charm.land/bubbletea", "github.com/charmbracelet/bubbletea"} {
		if dep == prefix || strings.HasPrefix(dep, prefix+"/") {
			return true
		}
	}
	return slices.Contains(strings.Split(dep, "/"), "steep")
}

// goList runs go list with args in the package directory and returns its
// output, failing the test with go's stderr if it fails.
func goList(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "go", append([]string{"list"}, args...)...).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("go list: %v\n%s", err, exitErr.Stderr)
		}
		t.Fatalf("go list: %v", err)
	}
	return string(out)
}
