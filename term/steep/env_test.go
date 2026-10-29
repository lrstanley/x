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

// stillPatterns maps each still's path to its MarkMatch patterns.
func stillPatterns(stills []reel.File) map[string][]string {
	out := make(map[string][]string, len(stills))
	for _, f := range stills {
		out[f.Path] = slices.Sorted(slices.Values(f.MarkMatch))
	}
	return out
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
	if cfg := collectOptions(t, WithRecording(reel.Profile{Outputs: reel.MustFiles("x.gif")})); cfg.policy != exportOff {
		t.Fatalf("got policy %v, want off: a profile alone exports nothing", cfg.policy)
	}
}

func TestWithIgnoreEnv(t *testing.T) {
	t.Setenv("STEEP_EXPORT_MODE", "on")
	t.Setenv("STEEP_EXPORT", "x.mp4")
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
// profile: timing, outputs, and stills replace the profile's, and render
// options are added after its own.
func TestApplyEnvProfile(t *testing.T) {
	t.Setenv("STEEP_EXPORT", "{name}_{status}.webm?quality=80,{name}_{status}_{mark}.png")
	t.Setenv("STEEP_EXPORT_DIR", "out")
	t.Setenv("STEEP_EXPORT_FPS", "10")
	t.Setenv("STEEP_EXPORT_SPEED", "0.5")
	t.Setenv("STEEP_RENDERER_SCALE", "2")
	t.Setenv("STEEP_RENDERER_FONT_FAMILY", "")

	user := reel.Profile{
		Outputs: reel.MustFiles("mine.gif"),
		Encode:  []sink.Option{sink.WithLoopCount(sink.LoopOnce)},
		FPS:     50,
		Vars:    map[string]string{"name": "fixed"},
	}
	cfg := collectOptions(t, WithRecording(user), WithStills(StillSnapshot, HookAssert))
	p := cfg.recording
	if got := outputPaths(p.Outputs); !slices.Equal(got, []string{"{name}_{status}.webm", "{name}_{status}_{mark}.png"}) {
		t.Errorf("outputs = %q, want the environment's", got)
	}
	if len(p.Outputs[0].Options) != 1 || len(p.Encode) != 1 {
		t.Errorf("%d output options and %d encode options, want the URL's and the profile's", len(p.Outputs[0].Options), len(p.Encode))
	}
	if cfg.stills != nil {
		t.Errorf("stills = %v, want those of WithStills replaced", cfg.stills)
	}
	if p.FPS != 10 || p.Speed != 0.5 {
		t.Errorf("fps %d speed %v, want 10 and 0.5", p.FPS, p.Speed)
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
	if again := collectOptions(t, WithRecording(user)); len(again.recording.Render) != 1 || len(user.Render) != 0 {
		t.Errorf("render options grew: %d, profile %d", len(again.recording.Render), len(user.Render))
	}
}

func TestWithStills(t *testing.T) {
	t.Parallel()
	cfg := collectOptions(t, WithIgnoreEnv(true), WithStills(StillPNG), WithStills(StillSnapshot|StillPNG, HookInput, HookAssert))
	want := map[string][]string{
		stillPath + ".png":  slices.Sorted(slices.Values(append([]string{"assert-[0-9]*"}, hookPatterns(captureEvents)...))),
		stillPath + ".snap": {"assert-[0-9]*", "input-[0-9]*"},
	}
	if got := stillPatterns(cfg.stills); !maps.EqualFunc(got, want, slices.Equal) {
		t.Fatalf("stills = %v, want %v", got, want)
	}
	// The default events are not grown by a later call.
	if !slices.Equal(captureEvents, []HookEvent{HookInput, HookTerminal, HookProgram, HookWait, HookReady, HookClose}) {
		t.Fatalf("captureEvents changed: %v", captureEvents)
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
