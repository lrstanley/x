// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
)

func TestRun(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil || runtime.GOOS == "windows" {
		t.Skip("needs bash on a pseudo-terminal")
	}
	files, err := run(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if fi, err := os.Stat(f); err != nil || fi.Size() == 0 {
			t.Errorf("%s: %v", f, err)
		}
	}
}
