// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"errors"
	"os/exec"
	"path"
	"slices"
	"strings"
	"testing"
)

// TestNoTestOnlyDependencies checks that no package in the module, other than
// testutil helpers that only _test.go files import, links testing, Bubble Tea,
// or steep.
func TestNoTestOnlyDependencies(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go binary not available")
	}

	out := goList(t, "-f", "{{.ImportPath}}{{range .Deps}} {{.}}{{end}}", "./...")

	for line := range strings.Lines(out) {
		fields := strings.Fields(line)
		if len(fields) == 0 || path.Base(fields[0]) == "testutil" {
			continue
		}
		for _, dep := range fields[1:] {
			if testOnlyDependency(dep) {
				t.Errorf("%s depends on %s", fields[0], dep)
			}
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
