// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package draw_test

import (
	"testing"
	"time"

	idraw "github.com/lrstanley/x/term/reel/render/internal/draw"
	"github.com/lrstanley/x/term/reel/render/types"
	"github.com/lrstanley/x/term/reel/vt"
)

type testFrameSource struct {
	at               time.Duration
	cursorBlinkSpeed time.Duration
}

func (t testFrameSource) At() time.Duration { return t.at }
func (t testFrameSource) CursorBlinkSpeed() time.Duration {
	return t.cursorBlinkSpeed
}
func (t testFrameSource) Palette() types.Palette       { return types.Palette{} }
func (t testFrameSource) State() vt.State              { return vt.State{} }
func (t testFrameSource) FaintFactor() float64         { return 0 }
func (t testFrameSource) BackgroundOpacity() float64   { return 1 }
func (t testFrameSource) BackgroundOpacityCells() bool { return false }
func (t testFrameSource) Metrics() types.Metrics       { return types.Metrics{} }

func TestBlinkVisible(t *testing.T) {
	t.Parallel()

	if !idraw.BlinkVisible(0, 500*time.Millisecond) {
		t.Fatal("phase 0 should be visible")
	}
	if idraw.BlinkVisible(500*time.Millisecond, 500*time.Millisecond) {
		t.Fatal("phase 1 should be hidden")
	}
	if idraw.BlinkVisible(-time.Millisecond, 500*time.Millisecond) {
		t.Fatal("just before 0 should be in phase 1")
	}
	if !idraw.BlinkVisible(0, 0) {
		t.Fatal("zero half-cycle should always be visible")
	}
}

func TestCycle(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ at, want time.Duration }{
		{0, 0},
		{3, 3},
		{10, 0},
		{23, 3},
		{-1, 9},
		{-10, 0},
	} {
		if got := idraw.Cycle(tc.at, 10); got != tc.want {
			t.Errorf("Cycle(%d, 10) = %d, want %d", tc.at, got, tc.want)
		}
	}
}

func TestTextVisible(t *testing.T) {
	t.Parallel()

	src := testFrameSource{}
	if !idraw.TextVisible(src, nil) {
		t.Fatal("nil cell should be visible")
	}
	if !idraw.TextVisible(src, &vt.Cell{}) {
		t.Fatal("plain cell should be visible")
	}

	blink := &vt.Cell{Style: vt.Style{Attrs: vt.AttrBlink}}
	if !idraw.TextVisible(src, blink) {
		t.Fatal("blink cell at phase 0 should be visible")
	}
	src.at = 500 * time.Millisecond
	if idraw.TextVisible(src, blink) {
		t.Fatal("blink cell at phase 1 should be hidden")
	}
}

func TestCursorVisible(t *testing.T) {
	t.Parallel()

	src := testFrameSource{cursorBlinkSpeed: 500 * time.Millisecond}
	var cur vt.Cursor
	if !idraw.CursorVisible(src, cur) {
		t.Fatal("cursor blink disabled should always be visible")
	}
	cur.Blink = true
	if !idraw.CursorVisible(src, cur) {
		t.Fatal("cursor at blink phase 0 should be visible")
	}
	src.at = 500 * time.Millisecond
	if idraw.CursorVisible(src, cur) {
		t.Fatal("cursor at blink phase 1 should be hidden")
	}
}
