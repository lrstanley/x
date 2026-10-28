// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/sink"
)

func outputPaths(files []reel.File) []string {
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}
	return paths
}

func TestParseExportTypes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		raw    string
		want   []string
		stills StillKind
	}{
		{"gif", []string{"{name}_{status}.gif"}, 0},
		{" GIF , mp4,webm, WEBP ,cast", []string{
			"{name}_{status}.gif", "{name}_{status}.mp4", "{name}_{status}.webm",
			"{name}_{status}.webp", "{name}_{status}.cast",
		}, 0},
		{"png", []string{}, StillPNG},
		{"snapshot,snap", []string{}, StillSnapshot},
		{"all", []string{"{name}_{status}.gif", "{name}_{status}.cast"}, StillPNG | StillSnapshot},
		{"gif,all,gif", []string{"{name}_{status}.gif", "{name}_{status}.cast"}, StillPNG | StillSnapshot},
		{"log,bogus", []string{}, 0},
	} {
		outputs, stills := parseExportTypes(tc.raw)
		if outputs == nil {
			t.Errorf("%q: outputs are nil, want an empty list that replaces the profile's", tc.raw)
		}
		if got := outputPaths(outputs); !slices.Equal(got, tc.want) || stills != tc.stills {
			t.Errorf("%q: outputs %q stills %b, want %q %b", tc.raw, got, stills, tc.want, tc.stills)
		}
	}
}

func TestParseExportMode(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]exportPolicy{
		"on": exportAlways, "Always": exportAlways, "true": exportAlways, "1": exportAlways,
		"failure": exportOnFailure, "on-failure": exportOnFailure,
		"off": exportOff, "false": exportOff, "0": exportOff,
	} {
		if got, ok := parseExportMode(raw); !ok || got != want {
			t.Errorf("%q: got %v %v, want %v", raw, got, ok, want)
		}
	}
	if _, ok := parseExportMode("sometimes"); ok {
		t.Error("unknown mode parsed")
	}
}

func TestApplyEnvExportMode(t *testing.T) {
	t.Setenv("STEEP_EXPORT_MODE", "failure")
	if cfg := collectOptions(t, WithExportAlways()); cfg.policy != exportOnFailure {
		t.Fatalf("got policy %v, want the environment's on failure", cfg.policy)
	}
	t.Setenv("STEEP_EXPORT_MODE", "nonsense")
	if cfg := collectOptions(t, WithExportAlways()); cfg.policy != exportAlways {
		t.Fatalf("got policy %v, want an unknown mode to leave the option alone", cfg.policy)
	}
}

func TestDefaultPolicyOff(t *testing.T) {
	t.Setenv("STEEP_EXPORT_MODE", "")
	if cfg := collectOptions(t, WithRecording(reel.Profile{Outputs: reel.Files("x.gif")})); cfg.policy != exportOff {
		t.Fatalf("got policy %v, want off: a profile alone exports nothing", cfg.policy)
	}
}

func TestWithIgnoreEnv(t *testing.T) {
	t.Setenv("STEEP_EXPORT_MODE", "on")
	t.Setenv("STEEP_EXPORT_TYPE", "mp4")
	t.Setenv("STEEP_EXPORT_FPS", "10")
	t.Setenv("STEEP_SLOW", "500ms")
	cfg := collectOptions(t, WithIgnoreEnv(true))
	if cfg.policy != exportOff || cfg.slow != 0 || cfg.recording.Outputs != nil || cfg.recording.FPS != 0 {
		t.Fatalf("environment applied: policy %v slow %v outputs %v fps %d", cfg.policy, cfg.slow, cfg.recording.Outputs, cfg.recording.FPS)
	}
}

func TestApplyEnvSlow(t *testing.T) {
	t.Setenv("STEEP_SLOW", "25ms")
	cfg := collectOptions(t)
	if cfg.slow != 25*time.Millisecond {
		t.Fatalf("got %v", cfg.slow)
	}
}

func TestApplyEnvTypingDelay(t *testing.T) {
	t.Setenv("STEEP_TYPING_DELAY", "50ms")
	if cfg := collectOptions(t); cfg.typingDelay != 50*time.Millisecond {
		t.Fatalf("got %v", cfg.typingDelay)
	}
	// As with the other STEEP_ variables, it overrides the option, even one
	// passed to a single call.
	if cfg := collectOptions(t, WithTypingDelay(0)); cfg.typingDelay != 50*time.Millisecond {
		t.Fatalf("with WithTypingDelay(0): got %v", cfg.typingDelay)
	}
}

func TestApplyEnvRecordMessages(t *testing.T) {
	t.Setenv("STEEP_RECORD_MESSAGES", "1")
	if cfg := collectOptions(t); !cfg.recordMessages {
		t.Fatal("STEEP_RECORD_MESSAGES=1 did not record messages")
	}
	t.Setenv("STEEP_RECORD_MESSAGES", "false")
	if cfg := collectOptions(t, WithRecordMessages(true)); cfg.recordMessages {
		t.Fatal("STEEP_RECORD_MESSAGES=false did not override the option")
	}
}

// TestApplyEnvProfile checks the environment's patch of the recording
// profile: timing and outputs replace the profile's, and encode and render
// options are added after its own.
func TestApplyEnvProfile(t *testing.T) {
	t.Setenv("STEEP_EXPORT_TYPE", "webm,png")
	t.Setenv("STEEP_EXPORT_DIR", "out")
	t.Setenv("STEEP_EXPORT_FPS", "10")
	t.Setenv("STEEP_EXPORT_SPEED", "0.5")
	t.Setenv("STEEP_EXPORT_QUALITY", "80")
	t.Setenv("STEEP_EXPORT_MAX_FRAMES", "5")
	t.Setenv("STEEP_EXPORT_MAX_BYTES", "")
	t.Setenv("STEEP_EXPORT_OPTIMIZE", "")
	t.Setenv("STEEP_RENDERER_SCALE", "2")
	t.Setenv("STEEP_RENDERER_FONT_FAMILY", "")

	user := reel.Profile{
		Outputs: reel.Files("mine.gif"),
		Encode:  []sink.Option{sink.WithLoopCount(sink.LoopOnce)},
		FPS:     50,
		Vars:    map[string]string{"name": "fixed"},
	}
	cfg := collectOptions(t, WithRecording(user), WithStills(StillSnapshot, HookAssert))
	p := cfg.recording
	if got := outputPaths(p.Outputs); !slices.Equal(got, []string{"{name}_{status}.webm"}) {
		t.Errorf("outputs = %q, want the environment's", got)
	}
	wantStills := make(map[HookEvent]StillKind)
	for _, ev := range captureEvents {
		wantStills[ev] = StillPNG
	}
	if !maps.Equal(cfg.stills, wantStills) {
		t.Errorf("stills = %v, want the environment's PNG at the capture events only", cfg.stills)
	}
	if p.FPS != 10 || p.Speed != 0.5 {
		t.Errorf("fps %d speed %v, want 10 and 0.5", p.FPS, p.Speed)
	}
	if len(p.Encode) != 3 {
		t.Errorf("%d encode options, want the profile's and two from the environment", len(p.Encode))
	}
	if len(p.Render) != 1 {
		t.Errorf("%d render options, want the scale", len(p.Render))
	}
	if cfg.artifacts != "out" {
		t.Errorf("artifacts = %q", cfg.artifacts)
	}
	if p.Vars["name"] != "fixed" {
		t.Errorf("vars = %v, want the profile's kept", p.Vars)
	}
	// Collecting the options again must not grow the profile's slices.
	if again := collectOptions(t, WithRecording(user)); len(again.recording.Encode) != 3 || len(user.Encode) != 1 {
		t.Errorf("encode options grew: %d, profile %d", len(again.recording.Encode), len(user.Encode))
	}
}

func TestWithStills(t *testing.T) {
	t.Parallel()
	cfg := collectOptions(t, WithIgnoreEnv(true), WithStills(StillPNG), WithStills(StillSnapshot, HookInput, HookAssert))
	want := make(map[HookEvent]StillKind)
	want[HookAssert] = StillSnapshot
	for _, ev := range captureEvents {
		want[ev] = StillPNG
	}
	want[HookInput] |= StillSnapshot
	if !maps.Equal(cfg.stills, want) {
		t.Fatalf("stills = %v, want %v", cfg.stills, want)
	}
}

func TestParseOptimizeFlags(t *testing.T) {
	t.Parallel()
	if got := parseOptimizeFlags("none"); got != sink.OptimizeNone {
		t.Fatalf("got %v", got)
	}
	if got := parseOptimizeFlags("frames,dirty"); got != (sink.OptimizeFrames | sink.OptimizeDirtyRects) {
		t.Fatalf("got %v", got)
	}
}

func TestNormalizeTestName(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"TestFoo/bar":           "TestFoo_bar",
		"TestFoo/a_b/../c d#01": "TestFoo_a_b____c_d_01",
		"///":                   "test",
		"TestÜber/日本":           "TestÜber_日本",
	} {
		if got := normalizeTestName(in); got != want {
			t.Errorf("normalizeTestName(%q) = %q, want %q", in, got, want)
		}
	}
}
