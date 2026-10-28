// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/render"
)

func applyEnv(tb testing.TB, cfg *options) {
	if cfg.ignoreEnv {
		return
	}

	if v := os.Getenv("STEEP_LOG"); v != "" {
		if v == "true" || v == "1" {
			cfg.hooks = append(cfg.hooks, LoggerHook(tb, nil))
		}
	}

	if v := os.Getenv("STEEP_RECORD_MESSAGES"); v != "" {
		if on, err := strconv.ParseBool(v); err == nil {
			cfg.recordMessages = on
		}
	}

	if v := os.Getenv("STEEP_SLOW"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.slow = d
		}
	}

	if v := os.Getenv("STEEP_TYPING_DELAY"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.typingDelay = d
		}
	}

	applyExportEnv(tb, cfg)

	if v := os.Getenv("STEEP_SNAPSHOT_DIR"); v != "" {
		cfg.snapshotDir = v
	}

	if v := os.Getenv("STEEP_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.timeout = d
		}
	}

	if v := os.Getenv("STEEP_CHECK"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.checkInterval = d
		}
	}

	if v := os.Getenv("STEEP_SETTLE"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.settleTimeout = d
		}
	}
}

// applyExportEnv applies the STEEP_EXPORT* and STEEP_RENDERER_* variables.
// Render options are appended to the profile's, so the environment wins
// without dropping the rest.
func applyExportEnv(tb testing.TB, cfg *options) {
	if v := os.Getenv("STEEP_EXPORT_MODE"); v != "" {
		if policy, ok := parseExportMode(v); ok {
			cfg.policy = policy
		}
	}
	if v := os.Getenv("STEEP_EXPORT_DIR"); v != "" {
		cfg.artifacts = v
	}

	var patch reel.Profile
	if v := os.Getenv("STEEP_EXPORT"); v != "" {
		outputs, stills, err := parseExport(v)
		if err != nil {
			tb.Fatalf("steep: STEEP_EXPORT: %v", err)
		}
		patch.Outputs = outputs
		cfg.stills = stills
	}
	if v := os.Getenv("STEEP_EXPORT_FPS"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			patch.FPS = n
		}
	}
	if v := os.Getenv("STEEP_EXPORT_SPEED"); v != "" {
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			patch.Speed = f
		}
	}

	var rend []render.Option
	if v := os.Getenv("STEEP_RENDERER_SCALE"); v != "" {
		if scale, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			rend = append(rend, render.WithScale(scale))
		}
	}
	if v := os.Getenv("STEEP_RENDERER_FONT_FAMILY"); v != "" {
		rend = append(rend, render.WithFontFamilyName(v))
	}
	if rend != nil {
		patch.Render = append(slices.Clip(cfg.recording.Render), rend...)
	}

	cfg.recording = cfg.recording.Merge(patch)
}

// parseExportMode parses STEEP_EXPORT_MODE.
func parseExportMode(raw string) (exportPolicy, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "on", "always", "true", "1":
		return exportAlways, true
	case "failure", "on-failure":
		return exportOnFailure, true
	case "off", "false", "0":
		return exportOff, true
	}
	return exportOff, false
}

// parseExport parses STEEP_EXPORT, a comma-separated list of outputs (see
// [parseOutput]). The outputs are never nil, so they replace the profile's
// in [reel.Profile.Merge] even when empty.
func parseExport(raw string) ([]reel.File, []hookStill, error) {
	outputs := []reel.File{}
	var stills []hookStill
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		f, events, hook, err := parseOutput(part)
		switch {
		case err != nil:
			return nil, nil, err
		case !hook:
			outputs = append(outputs, f)
			continue
		case len(events) == 0:
			events = captureEvents
		}
		if stills, err = addStill(stills, hookStill{file: f}, events); err != nil {
			return nil, nil, fmt.Errorf("output %q: %w", part, err)
		}
	}
	return outputs, stills, nil
}

// parseOutput parses one output in [reel.ParseFile]'s syntax. One whose
// path holds {mark} and that has no marks parameter is a hook still, at the
// events of its events parameters, as is a ".snap" output, which takes no
// other parameters.
func parseOutput(part string) (f reel.File, events []HookEvent, hook bool, err error) {
	path, query, _ := strings.Cut(part, "?")
	events, rest, err := cutEvents(query)
	if err != nil {
		return f, nil, false, fmt.Errorf("output %q: %w", part, err)
	}
	if isSnapPath(path) {
		if rest != "" {
			return f, nil, false, fmt.Errorf("output %q: a .snap output takes only events parameters", part)
		}
		if f.Path, err = reel.FilePath(path); err != nil {
			return f, nil, false, err
		}
		if !strings.Contains(f.Path, markVar) {
			return f, nil, false, fmt.Errorf("output %q: a .snap path must hold %s", part, markVar)
		}
		return f, events, true, nil
	}
	// Without events, reel parses (and reports) the output as written.
	raw := part
	if len(events) > 0 {
		raw = path
		if rest != "" {
			raw += "?" + rest
		}
	}
	if f, err = reel.ParseFile(raw); err != nil {
		return f, nil, false, err
	}
	hook = strings.Contains(f.Path, markVar) && len(f.Marks) == 0
	switch {
	case hook && strings.EqualFold(filepath.Ext(f.Path), ".cast"):
		return f, nil, false, fmt.Errorf("output %q: a .cast path cannot hold %s", part, markVar)
	case !hook && len(events) > 0:
		return f, nil, false, fmt.Errorf("output %q: events parameters need a path holding %s and no marks", part, markVar)
	}
	return f, events, hook, nil
}

// cutEvents removes the events parameters from query, returning their hook
// events and the rest of query.
func cutEvents(query string) ([]HookEvent, string, error) {
	values, err := url.ParseQuery(query)
	if err != nil {
		return nil, "", err
	}
	var events []HookEvent
	for _, name := range values["events"] {
		ev, ok := parseHookEvent(name)
		if !ok {
			return nil, "", fmt.Errorf("unknown hook event %q", name)
		}
		if !slices.Contains(events, ev) {
			events = append(events, ev)
		}
	}
	values.Del("events")
	return events, values.Encode(), nil
}

// parseHookEvent returns the event [HookEvent.String] names name.
func parseHookEvent(name string) (HookEvent, bool) {
	for ev := HookInput; ev <= HookCapture; ev++ {
		if ev.String() == name {
			return ev, true
		}
	}
	return 0, false
}
