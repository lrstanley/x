// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"os"
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
		outputs, err := reel.ParseFiles(v)
		if err != nil {
			tb.Fatalf("steep: STEEP_EXPORT: %v", err)
		}
		// Not nil, so it replaces the profile's outputs even when empty.
		patch.Outputs = append([]reel.File{}, outputs...)
		cfg.stills = nil
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
