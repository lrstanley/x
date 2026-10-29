// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestRun checks the files of a recording, and that exporting its
// asciicast file applies the effects it holds: the GIF is the recording's,
// and the asciicast file written again is the same.
func TestRun(t *testing.T) {
	dir := t.TempDir()
	files, err := run(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if fi, statErr := os.Stat(f); statErr != nil || fi.Size() == 0 {
			t.Errorf("%s: %v", f, statErr)
		}
	}
	if _, err = runCast(t.Context(), dir, filepath.Join(dir, "demo.cast")); err != nil {
		t.Fatal(err)
	}
	for _, ext := range []string{".gif", ".cast"} {
		var want, got []byte
		want, err = os.ReadFile(filepath.Join(dir, "demo"+ext))
		if err != nil {
			t.Fatal(err)
		}
		got, err = os.ReadFile(filepath.Join(dir, "cast"+ext))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("cast%s differs from demo%s", ext, ext)
		}
	}
}
