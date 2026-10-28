// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"
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

// stillEvents maps each still's path to its events.
func stillEvents(stills []hookStill) map[string][]HookEvent {
	out := make(map[string][]HookEvent, len(stills))
	for _, s := range stills {
		out[s.file.Path] = slices.Sorted(slices.Values(s.events))
	}
	return out
}

func TestParseExport(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		raw     string
		outputs []string
		stills  map[string][]HookEvent
	}{
		{"{name}.gif", []string{"{name}.gif"}, map[string][]HookEvent{}},
		{
			" ./artifacts/{name}.gif?max-bytes=5MB&loop=once , file://./artifacts/{name}.cast?version=3,,",
			[]string{"./artifacts/{name}.gif", "./artifacts/{name}.cast"},
			map[string][]HookEvent{},
		},
		{"x-{mark}.png?marks=a&marks=b", []string{"x-{mark}.png"}, map[string][]HookEvent{}},
		{"", []string{}, map[string][]HookEvent{}},
		{"{mark}.png?ev%65nts=wait&%6Fptimize=none", []string{}, map[string][]HookEvent{"{mark}.png": {HookWait}}},
		{
			"{mark}.snap?events=wait,./{mark}.snap?events=close,{mark}.gif?events=input,{mark}.gif",
			[]string{},
			map[string][]HookEvent{
				"{mark}.snap": {HookWait, HookClose},
				"{mark}.gif":  slices.Sorted(slices.Values(captureEvents)),
			},
		},
		{"file:///C:/out/{mark}.snap", []string{}, map[string][]HookEvent{"C:/out/{mark}.snap": slices.Sorted(slices.Values(captureEvents))}},
		{
			"{name}_{mark}.png?optimize=none,{name}_{mark}.snap?events=wait&events=close&events=wait,file://s/{mark}.snap",
			[]string{},
			map[string][]HookEvent{
				"{name}_{mark}.png":  slices.Sorted(slices.Values(captureEvents)),
				"{name}_{mark}.snap": {HookWait, HookClose},
				"s/{mark}.snap":      slices.Sorted(slices.Values(captureEvents)),
			},
		},
		{
			"{mark}.gif?events=assert&loop=once&events=input",
			[]string{},
			map[string][]HookEvent{"{mark}.gif": {HookInput, HookAssert}},
		},
	} {
		outputs, stills, err := parseExport(tc.raw)
		if err != nil {
			t.Errorf("%q: %v", tc.raw, err)
			continue
		}
		if outputs == nil {
			t.Errorf("%q: outputs are nil, want an empty list that replaces the profile's", tc.raw)
		}
		if got := outputPaths(outputs); !slices.Equal(got, tc.outputs) {
			t.Errorf("%q: outputs %q, want %q", tc.raw, got, tc.outputs)
		}
		if got := stillEvents(stills); !maps.EqualFunc(got, tc.stills, slices.Equal) {
			t.Errorf("%q: stills %v, want %v", tc.raw, got, tc.stills)
		}
	}
}

func TestParseExportErrors(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]string{
		"gif":                               "unknown format",
		"x.gif?fps=20":                      `unknown parameter "fps"`,
		"x.gif?events=input":                "events parameters need a path holding {mark}",
		"x-{mark}.png?marks=a&events=input": "events parameters need",
		"{mark}.png?events=typing":          `unknown hook event "typing"`,
		"{mark}.png?events=%zz":             "invalid URL escape",
		"x.snap":                            "a .snap path must hold {mark}",
		"{mark}.gif?events=input,{mark}.gif?events=wait&loop=once": `two outputs write "{mark}.gif"`,
		"{mark}.gif?loop=once,./{mark}.gif":                        `two outputs write "./{mark}.gif"`,
		"x.gif?ev%zznts=1":                                         "invalid URL escape",
		"{mark}.snap?loop=once":                                    "takes only events",
		"s3://b/{mark}.snap":                                       `unsupported scheme "s3"`,
		"file:///C:/{mark}.snap?loop=once":                         "takes only events",
		"{mark}.cast":                                              "a .cast path cannot hold {mark}",
	} {
		_, _, err := parseExport(raw)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want an error containing %q", raw, err, want)
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
	if got := outputPaths(p.Outputs); !slices.Equal(got, []string{"{name}_{status}.webm"}) {
		t.Errorf("outputs = %q, want the environment's", got)
	}
	if len(p.Outputs[0].Options) != 1 || len(p.Encode) != 1 {
		t.Errorf("%d output options and %d encode options, want the URL's and the profile's", len(p.Outputs[0].Options), len(p.Encode))
	}
	want := map[string][]HookEvent{"{name}_{status}_{mark}.png": slices.Sorted(slices.Values(captureEvents))}
	if got := stillEvents(cfg.stills); !maps.EqualFunc(got, want, slices.Equal) {
		t.Errorf("stills = %v, want the environment's PNG at the capture events only", got)
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

func TestPlanStills(t *testing.T) {
	t.Parallel()
	marks := []hookMark{{label: "ready-1", event: HookReady, snap: "a"}, {label: "input-1", event: HookInput, snap: "b"}}
	vars := map[string]string{"name": "TestFoo"}
	resolve := func(path string) string { return filepath.Join("dir", path) }
	stills := []hookStill{
		{file: reel.File{Path: "{name}_{mark}.png"}, events: []HookEvent{HookReady, HookInput}},
		{file: reel.File{Path: "{name}_{mark}.snap"}, events: []HookEvent{HookInput}},
		{file: reel.File{Path: "{mark}.gif"}, events: []HookEvent{HookClose}},
	}
	files, snaps, err := planStills(stills, marks, vars, resolve)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "{name}_{mark}.png" || !slices.Equal(files[0].Marks, []string{"ready-1", "input-1"}) {
		t.Errorf("files = %+v", files)
	}
	if want := []snapFile{{path: filepath.Join("dir", "TestFoo_input-1.snap"), text: "b"}}; !slices.Equal(snaps, want) {
		t.Errorf("snapshots = %+v, want %+v", snaps, want)
	}
	if vars["mark"] != "" {
		t.Error("planStills changed vars")
	}

	// Templates that differ can still expand to one path.
	stills = append(stills, hookStill{file: reel.File{Path: "TestFoo_{mark}.snap"}, events: []HookEvent{HookInput}})
	if _, _, err = planStills(stills, marks, vars, resolve); err == nil || !strings.Contains(err.Error(), "two outputs write") {
		t.Errorf("got %v, want a duplicate path error", err)
	}
}

func TestWithStills(t *testing.T) {
	t.Parallel()
	cfg := collectOptions(t, WithIgnoreEnv(true), WithStills(StillPNG), WithStills(StillSnapshot|StillPNG, HookInput, HookAssert))
	want := map[string][]HookEvent{
		stillPath + ".png":  slices.Sorted(slices.Values(append([]HookEvent{HookAssert}, captureEvents...))),
		stillPath + ".snap": {HookInput, HookAssert},
	}
	if got := stillEvents(cfg.stills); !maps.EqualFunc(got, want, slices.Equal) {
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
