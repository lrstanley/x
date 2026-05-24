// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package draw_test

import (
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/charm/still/internal/config"
	idraw "github.com/lrstanley/x/charm/still/internal/draw"
)

func TestBlinkVisible(t *testing.T) {
	t.Parallel()

	if !idraw.BlinkVisible(time.Unix(0, 0), 500*time.Millisecond) {
		t.Fatal("phase 0 should be visible")
	}
	if idraw.BlinkVisible(time.Unix(0, int64(500*time.Millisecond)), 500*time.Millisecond) {
		t.Fatal("phase 1 should be hidden")
	}
	if !idraw.BlinkVisible(time.Unix(0, 0), 0) {
		t.Fatal("zero half-cycle should always be visible")
	}
}

func TestTextVisible(t *testing.T) {
	t.Parallel()

	cfg := config.Snapshot{Now: time.Unix(0, 0)}
	if !idraw.TextVisible(cfg, nil) {
		t.Fatal("nil cell should be visible")
	}
	if !idraw.TextVisible(cfg, &uv.Cell{}) {
		t.Fatal("plain cell should be visible")
	}

	blink := &uv.Cell{Style: uv.Style{Attrs: uv.AttrBlink}}
	if !idraw.TextVisible(cfg, blink) {
		t.Fatal("blink cell at phase 0 should be visible")
	}
	cfg.Now = time.Unix(0, int64(500*time.Millisecond))
	if idraw.TextVisible(cfg, blink) {
		t.Fatal("blink cell at phase 1 should be hidden")
	}
}

func TestCursorVisible(t *testing.T) {
	t.Parallel()

	cfg := config.Snapshot{Now: time.Unix(0, 0), CursorBlinkSpeed: 500 * time.Millisecond}
	if !idraw.CursorVisible(cfg) {
		t.Fatal("cursor blink disabled should always be visible")
	}
	cfg.State.CursorBlink = true
	if !idraw.CursorVisible(cfg) {
		t.Fatal("cursor at blink phase 0 should be visible")
	}
	cfg.Now = time.Unix(0, int64(500*time.Millisecond))
	if idraw.CursorVisible(cfg) {
		t.Fatal("cursor at blink phase 1 should be hidden")
	}
}
