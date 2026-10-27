// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package sink

import (
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// TestNoTestOnlyDependencies checks that sink links no test-only packages
// (testing, Bubble Tea, steep).
func TestNoTestOnlyDependencies(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go binary not available")
	}

	out := goList(t, "-deps", ".")

	for dep := range strings.FieldsSeq(out) {
		if testOnlyDependency(dep) {
			t.Errorf("sink depends on %s", dep)
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
