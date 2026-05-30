// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package draw_test

import (
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	idraw "github.com/lrstanley/x/charm/still/internal/draw"
	"github.com/lrstanley/x/charm/still/types"
)

type testFrameSource struct {
	now              time.Time
	cursorBlinkSpeed time.Duration
}

func (t testFrameSource) Now() time.Time { return t.now }
func (t testFrameSource) CursorBlinkSpeed() time.Duration {
	return t.cursorBlinkSpeed
}
func (t testFrameSource) Palette() types.Palette { return types.Palette{} }
func (t testFrameSource) EmulatorState() (types.EmulatorState, bool) {
	return types.EmulatorState{}, false
}
func (t testFrameSource) FaintFactor() float64         { return 0 }
func (t testFrameSource) BackgroundOpacity() float64   { return 1 }
func (t testFrameSource) BackgroundOpacityCells() bool { return false }
func (t testFrameSource) Metrics() types.Metrics       { return types.Metrics{} }

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

	src := testFrameSource{now: time.Unix(0, 0)}
	if !idraw.TextVisible(src, nil) {
		t.Fatal("nil cell should be visible")
	}
	if !idraw.TextVisible(src, &uv.Cell{}) {
		t.Fatal("plain cell should be visible")
	}

	blink := &uv.Cell{Style: uv.Style{Attrs: uv.AttrBlink}}
	if !idraw.TextVisible(src, blink) {
		t.Fatal("blink cell at phase 0 should be visible")
	}
	src.now = time.Unix(0, int64(500*time.Millisecond))
	if idraw.TextVisible(src, blink) {
		t.Fatal("blink cell at phase 1 should be hidden")
	}
}

func TestCursorVisible(t *testing.T) {
	t.Parallel()

	src := testFrameSource{now: time.Unix(0, 0), cursorBlinkSpeed: 500 * time.Millisecond}
	state := types.EmulatorState{}
	if !idraw.CursorVisible(src, state, true) {
		t.Fatal("cursor blink disabled should always be visible")
	}
	state.CursorBlink = true
	if !idraw.CursorVisible(src, state, true) {
		t.Fatal("cursor at blink phase 0 should be visible")
	}
	src.now = time.Unix(0, int64(500*time.Millisecond))
	if idraw.CursorVisible(src, state, true) {
		t.Fatal("cursor at blink phase 1 should be hidden")
	}
}
