// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package draw //nolint:revive // name mirrors image/draw responsibilities

import (
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/charm/still/internal/config"
)

// TextVisible reports whether a cell's foreground should be drawn for the current
// blink phase.
func TextVisible(cfg config.Snapshot, cell *uv.Cell) bool {
	if cell == nil {
		return true
	}
	attrs := cell.Style.Attrs
	switch {
	case attrs&uv.AttrRapidBlink != 0:
		return BlinkVisible(cfg.Now, 250*time.Millisecond)
	case attrs&uv.AttrBlink != 0:
		return BlinkVisible(cfg.Now, 500*time.Millisecond)
	default:
		return true
	}
}

// CursorVisible reports whether the cursor should be drawn for the current blink
// phase.
func CursorVisible(cfg config.Snapshot) bool {
	if !cfg.State.CursorBlink {
		return true
	}
	return BlinkVisible(cfg.Now, cfg.CursorBlinkSpeed)
}

// BlinkVisible reports whether a blinking element is in its on (visible) phase.
func BlinkVisible(now time.Time, halfCycle time.Duration) bool {
	if halfCycle <= 0 {
		return true
	}
	return now.UnixNano()/int64(halfCycle)%2 == 0
}
