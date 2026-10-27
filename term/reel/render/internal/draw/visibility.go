// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package draw //nolint:revive // name mirrors image/draw responsibilities

import (
	"time"

	"github.com/lrstanley/x/term/reel/render/internal/config"
	"github.com/lrstanley/x/term/reel/vt"
)

// TextVisible reports whether a cell's foreground should be drawn for the current
// blink phase.
func TextVisible(src config.FrameSource, cell *vt.Cell) bool {
	if cell == nil {
		return true
	}
	at := src.At()
	attrs := cell.Style.Attrs
	switch {
	case attrs&vt.AttrRapidBlink != 0:
		return BlinkVisible(at, 250*time.Millisecond)
	case attrs&vt.AttrBlink != 0:
		return BlinkVisible(at, 500*time.Millisecond)
	default:
		return true
	}
}

// CursorVisible reports whether the cursor should be drawn for the current blink
// phase.
func CursorVisible(src config.FrameSource, cur vt.Cursor) bool {
	if !cur.Blink {
		return true
	}
	return BlinkVisible(src.At(), src.CursorBlinkSpeed())
}

// BlinkVisible reports whether a blinking element is in its on (visible) phase
// at offset at.
func BlinkVisible(at, halfCycle time.Duration) bool {
	if halfCycle <= 0 {
		return true
	}
	return Cycle(at, 2*halfCycle) < halfCycle
}

// Cycle is how far at is into its period, 0 through period-1. A negative at
// counts back from 0, so a phase keeps the same direction across 0. period
// must be positive. Every time-driven phase the renderer draws, blinks,
// sweeps, spinners, and breaths alike, derives from it.
func Cycle(at, period time.Duration) time.Duration {
	c := at % period
	if c < 0 {
		c += period
	}
	return c
}
