// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel"
)

func TestRun(t *testing.T) {
	dir := t.TempDir()
	files, err := run(t.Context(), dir, 48, 6, bytes.NewReader(demoTypescript), bytes.NewReader(demoTiming))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if fi, statErr := os.Stat(f); statErr != nil || fi.Size() == 0 {
			t.Errorf("%s: %v", f, statErr)
		}
	}

	// The recording plays for as long as the timing file says.
	f, err := os.Open(filepath.Join(dir, "attach.cast"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	tl, err := reel.ReadTimeline(f)
	if err != nil {
		t.Fatal(err)
	}
	if d := tl.Duration(); d != 1507*time.Millisecond {
		t.Fatalf("Duration = %v, want 1.507s", d)
	}
}
