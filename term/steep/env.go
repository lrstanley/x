// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lrstanley/x/term/steep/snapshot"
	"github.com/lrstanley/x/term/still"
	"github.com/lrstanley/x/term/still/export"
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

	if v := os.Getenv("STEEP_RENDERER_SCALE"); v != "" {
		if scale, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.imageRendererOpts = append(cfg.imageRendererOpts, still.WithScale(scale))
		}
	}

	if v := os.Getenv("STEEP_RENDERER_FONT_FAMILY"); v != "" {
		cfg.imageRendererOpts = append(cfg.imageRendererOpts, still.WithFontFamilyName(v))
	}

	if v := os.Getenv("STEEP_EXPORT_MODE"); v != "" {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "on", "1":
			cfg.exportMode = ExportOn
		case "false", "off", "0":
			cfg.exportMode = ExportOff
		case "on-failure", "failure":
			cfg.exportMode = ExportOnFailure
		}
	}

	if v := os.Getenv("STEEP_EXPORT_TYPE"); v != "" {
		cfg.exportTypes = parseExportTypes(v)
	}

	if v := os.Getenv("STEEP_EXPORT_OPTIMIZE"); v != "" {
		cfg.exportOpts = append(cfg.exportOpts, export.WithOptimize(parseOptimizeFlags(v)))
	}

	if v := os.Getenv("STEEP_EXPORT_MAX_FRAMES"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 {
			cfg.exportOpts = append(cfg.exportOpts, export.WithMaxFrames(n))
		}
	}

	if v := os.Getenv("STEEP_EXPORT_MAX_BYTES"); v != "" {
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil && n >= 0 {
			cfg.exportOpts = append(cfg.exportOpts, export.WithMaxBytes(n))
		}
	}

	if v := os.Getenv("STEEP_EXPORT_QUALITY"); v != "" {
		// Out of range values are ignored, as the video encoder would reject
		// them.
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 && n <= export.MaxQuality {
			cfg.exportOpts = append(cfg.exportOpts, export.WithQuality(n))
		}
	}

	if v := os.Getenv("STEEP_EXPORT_DIR"); v != "" {
		cfg.exportDir = v
	}

	if v := os.Getenv("STEEP_SLOW"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.slow = d
		}
	}

	if v := os.Getenv("STEEP_EXPORT_FPS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.exportFPS = n
		}
	}

	if v := os.Getenv("STEEP_EXPORT_SPEED"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.exportSpeed = f
		}
	}

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

func parseExportTypes(raw string) ExportType {
	var t ExportType
	for part := range strings.SplitSeq(strings.ToLower(raw), ",") {
		part = strings.TrimSpace(part)
		switch part {
		case "png":
			t |= ExportTypePNG
		case "gif":
			t |= ExportTypeGIF
		case "mp4":
			t |= ExportTypeMP4
		case "webm":
			t |= ExportTypeWebM
		case "webp":
			t |= ExportTypeWebP
		case "log":
			t |= ExportTypeLog
		case "snapshot", "snap":
			t |= ExportTypeSnapshot
		case "all":
			return ExportTypeAll
		}
	}
	return t
}

func parseOptimizeFlags(raw string) export.OptimizeFlags {
	var f export.OptimizeFlags
	for part := range strings.SplitSeq(strings.ToLower(raw), ",") {
		part = strings.TrimSpace(part)
		switch part {
		case "none":
			return export.OptimizeNone
		case "frames":
			f |= export.OptimizeFrames
		case "dirty", "dirtyrects":
			f |= export.OptimizeDirtyRects
		case "quantize", "color":
			f |= export.OptimizeColorQuantization
		case "all":
			return export.OptimizeAll
		}
	}
	if f == 0 {
		return export.OptimizeAll
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
