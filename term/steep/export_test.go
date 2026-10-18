// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExportOnFailureWritesArtifacts(t *testing.T) {
	dir := t.ArtifactDir()
	h := NewHarness(t, rootTestModel{},
		WithExport(ExportOnFailure, ExportTypePNG|ExportTypeSnapshot),
		WithExportDir(dir),
		WithIgnoreEnv(true),
	)
	h.WaitSettle().Type("hi").WaitSettle()
	if h.export == nil {
		t.Fatal("expected export manager")
	}
	if h.export.lastPNG == nil {
		t.Fatal("expected buffered png before finalize")
	}
	if err := h.export.Finalize(true); err != nil {
		t.Fatal(err)
	}

	var foundPNG, foundSnap bool
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		base := filepath.Base(path)
		if strings.Contains(base, "_failed.png") {
			foundPNG = true
		}
		if strings.Contains(base, "_failed.snap") {
			foundSnap = true
		}
		return nil
	})
	if !foundPNG || !foundSnap {
		entries, _ := os.ReadDir(dir)
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("artifacts: png=%v snap=%v in %s (files: %v)", foundPNG, foundSnap, dir, names)
	}
}

func TestExportFilenameOverwrite(t *testing.T) {
	m := &exportManager{
		tb:   t,
		dir:  t.TempDir(),
		mode: ExportOn,
	}
	name1 := m.artifactName(HookInput.String(), ".png", false)
	time.Sleep(2 * time.Millisecond)
	name2 := m.artifactName(HookInput.String(), ".png", false)
	if name1 == name2 {
		t.Fatalf("expected distinct timestamps, got duplicate %q", name1)
	}
	if !strings.HasSuffix(name1, ".png") {
		t.Fatalf("bad suffix: %q", name1)
	}
}
