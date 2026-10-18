// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"os"
	"testing"
	"time"

	"github.com/lrstanley/x/term/still/export"
)

func TestParseExportTypes(t *testing.T) {
	t.Parallel()
	if got := parseExportTypes("png,gif,all"); got != ExportTypeAll {
		t.Fatalf("got %v, want ExportTypeAll", got)
	}
	if got := parseExportTypes("log,snapshot"); got != (ExportTypeLog | ExportTypeSnapshot) {
		t.Fatalf("got %v", got)
	}
}

func TestApplyEnvExportMode(t *testing.T) {
	t.Setenv("STEEP_EXPORT_MODE", "on-failure")
	cfg := collectOptions(t)
	if cfg.exportMode != ExportOnFailure {
		t.Fatalf("got mode %v, want ExportOnFailure", cfg.exportMode)
	}
}

func TestWithIgnoreEnv(t *testing.T) {
	t.Setenv("STEEP_EXPORT_MODE", "on")
	t.Setenv("STEEP_SLOW", "500ms")
	cfg := collectOptions(t, WithIgnoreEnv(true))
	if cfg.exportMode != ExportOff {
		t.Fatalf("export mode should stay off, got %v", cfg.exportMode)
	}
	if cfg.slow != 0 {
		t.Fatalf("slow should stay zero, got %v", cfg.slow)
	}
}

func TestApplyEnvSlow(t *testing.T) {
	t.Setenv("STEEP_SLOW", "25ms")
	cfg := collectOptions(t)
	if cfg.slow != 25*time.Millisecond {
		t.Fatalf("got %v", cfg.slow)
	}
}

func TestParseOptimizeFlags(t *testing.T) {
	t.Parallel()
	if got := parseOptimizeFlags("none"); got != export.OptimizeNone {
		t.Fatalf("got %v", got)
	}
	if got := parseOptimizeFlags("frames,dirty"); got != (export.OptimizeFrames | export.OptimizeDirtyRects) {
		t.Fatalf("got %v", got)
	}
}

func TestNormalizeTestName(t *testing.T) {
	t.Parallel()
	if got := normalizeTestName("TestFoo/bar"); got != "TestFoo_bar" {
		t.Fatalf("got %q", got)
	}
}

func TestArtifactDirUsesTB(t *testing.T) {
	dir := artifactDir(t)
	if dir == "" {
		t.Fatal("empty artifact dir")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	}
}
