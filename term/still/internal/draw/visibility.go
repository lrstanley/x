// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package draw //nolint:revive // name mirrors image/draw responsibilities

import (
	"time"

	"github.com/lrstanley/x/term/still/internal/config"
	"github.com/lrstanley/x/term/still/types"
	"github.com/lrstanley/x/term/vt/screen"
)

// TextVisible reports whether a cell's foreground should be drawn for the current
// blink phase.
func TextVisible(src config.FrameSource, cell *screen.Cell) bool {
	if cell == nil {
		return true
	}
	now := src.Now()
	attrs := cell.Style.Attrs
	switch {
	case attrs&screen.AttrRapidBlink != 0:
		return BlinkVisible(now, 250*time.Millisecond)
	case attrs&screen.AttrBlink != 0:
		return BlinkVisible(now, 500*time.Millisecond)
	default:
		return true
	}
}

// CursorVisible reports whether the cursor should be drawn for the current blink
// phase.
func CursorVisible(src config.FrameSource, state types.EmulatorState, hasState bool) bool {
	if !hasState || !state.CursorBlink {
		return true
	}
	return BlinkVisible(src.Now(), src.CursorBlinkSpeed())
}

// BlinkVisible reports whether a blinking element is in its on (visible) phase.
func BlinkVisible(now time.Time, halfCycle time.Duration) bool {
	if halfCycle <= 0 {
		return true
	}
	return now.UnixNano()/int64(halfCycle)%2 == 0
}
