// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build !observe_gen

package testdata_test

import (
	"bytes"
	"os/exec"
	"testing"

	// Pin sqlite for go mod tidy when ent/ (observe_gen tests) is not generated yet.
	_ "modernc.org/sqlite"
)

// TestSQLite generates the ent client then runs the observe_gen SQLite suite.
// Keeping the suite behind a build tag lets `go test ./...` succeed from a
// clean tree (ent/ is gitignored) without the parent module loading schemas.
func TestSQLite(t *testing.T) {
	run := func(name string, args ...string) {
		t.Helper()
		cmd := exec.Command(name, args...)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out.String())
		}
	}
	run("go", "run", "generate.go")
	run("go", "test", "-tags=observe_gen", "-count=1", ".")
}
