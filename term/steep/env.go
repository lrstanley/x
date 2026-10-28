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
	"github.com/lrstanley/x/term/reel/sink"
	"github.com/lrstanley/x/term/steep/snapshot"
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

	applyExportEnv(cfg)

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

// applyExportEnv applies the STEEP_EXPORT_* and STEEP_RENDERER_* variables:
// the policy, the artifacts directory, and a patch of the recording profile,
// merged with [reel.Profile.Merge]. Encode and Render options are appended to
// the profile's, so the environment wins without dropping the rest.
func applyExportEnv(cfg *options) {
	if v := os.Getenv("STEEP_EXPORT_MODE"); v != "" {
		if policy, ok := parseExportMode(v); ok {
			cfg.policy = policy
		}
	}
	if v := os.Getenv("STEEP_EXPORT_DIR"); v != "" {
		cfg.artifacts = v
	}

	var patch reel.Profile
	if v := os.Getenv("STEEP_EXPORT_TYPE"); v != "" {
		var stills StillKind
		patch.Outputs, stills = parseExportTypes(v)
		cfg.stills = addStills(nil, stills, captureEvents)
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

	var encode []sink.Option
	if v := os.Getenv("STEEP_EXPORT_OPTIMIZE"); v != "" {
		encode = append(encode, sink.WithOptimize(parseOptimizeFlags(v)))
	}
	if v := os.Getenv("STEEP_EXPORT_MAX_FRAMES"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 {
			encode = append(encode, sink.WithMaxFrames(n))
		}
	}
	if v := os.Getenv("STEEP_EXPORT_MAX_BYTES"); v != "" {
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil && n >= 0 {
			encode = append(encode, sink.WithMaxBytes(n))
		}
	}
	if v := os.Getenv("STEEP_EXPORT_QUALITY"); v != "" {
		// Out of range values are ignored, as the video encoder would reject
		// them.
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 && n <= sink.MaxQuality {
			encode = append(encode, sink.WithQuality(n))
		}
	}
	if encode != nil {
		patch.Encode = append(slices.Clip(cfg.recording.Encode), encode...)
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

// exportTypes are the STEEP_EXPORT_TYPE names of recording outputs, in the
// order they are written.
var exportTypes = []string{"gif", "mp4", "webm", "webp", "cast"}

// parseExportTypes parses STEEP_EXPORT_TYPE into the outputs and the stills
// it names. Unknown names are ignored. The outputs are never nil, so with
// [reel.Profile.Merge] they replace the profile's even when there are none.
func parseExportTypes(raw string) ([]reel.File, StillKind) {
	outputs := []reel.File{}
	var stills StillKind
	seen := map[string]bool{}
	add := func(typ string) {
		if !seen[typ] {
			seen[typ] = true
			outputs = append(outputs, reel.File{Path: "{name}_{status}." + typ})
		}
	}
	for part := range strings.SplitSeq(strings.ToLower(raw), ",") {
		switch part = strings.TrimSpace(part); part {
		case "png":
			stills |= StillPNG
		case "snapshot", "snap":
			stills |= StillSnapshot
		case "all":
			add("gif")
			add("cast")
			stills |= StillPNG | StillSnapshot
		default:
			if slices.Contains(exportTypes, part) {
				add(part)
			}
		}
	}
	return outputs, stills
}

func parseOptimizeFlags(raw string) sink.OptimizeFlags {
	var f sink.OptimizeFlags
	for part := range strings.SplitSeq(strings.ToLower(raw), ",") {
		part = strings.TrimSpace(part)
		switch part {
		case "none":
			return sink.OptimizeNone
		case "frames":
			f |= sink.OptimizeFrames
		case "dirty", "dirtyrects":
			f |= sink.OptimizeDirtyRects
		case "quantize", "color":
			f |= sink.OptimizeColorQuantization
		case "all":
			return sink.OptimizeAll
		}
	}
	if f == 0 {
		return sink.OptimizeAll
	}
	return f
}

// snapshotDirFromEnv returns snapshot.WithDir when STEEP_SNAPSHOT_DIR is set.
func snapshotDirFromEnv(cfg options) snapshot.Option {
	if cfg.snapshotDir == "" {
		return nil
	}
	return snapshot.WithDir(cfg.snapshotDir)
}
