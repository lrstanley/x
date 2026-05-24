// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import "time"

const (
	// DefaultPPI is the typographic definition of a point (72 points per inch),
	// used when converting [Pt] to pixels: px = pt * dpi / DefaultPPI.
	DefaultPPI = 72.0

	// DefaultDPI is the renderer's default [DPI] for point-to-pixel conversion.
	DefaultDPI DPI = 96

	// DefaultFontSize is the renderer's default primary font size in points.
	DefaultFontSize Pt = 11

	// DefaultCursorBlinkSpeed is the default blink speed for the cursor.
	DefaultCursorBlinkSpeed = 600 * time.Millisecond

	// DefaultBgOpacity is the default background opacity.
	DefaultBgOpacity = 1.0

	// DefaultFaintFactor is the default faint factor (0.5 means 50% of the
	// foreground color is blended with the background color).
	DefaultFaintFactor = 0.5

	// DefaultFocusDimming is the default focus dimming factor (0.16 means 16% of
	// the foreground color is blended with the background color).
	DefaultFocusDimming = 0.16
)
