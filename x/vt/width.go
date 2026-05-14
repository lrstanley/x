// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package vt

import "github.com/mattn/go-runewidth"

// DefaultWidthMeasurer matches typical terminal width rules (via go-runewidth).
// It is used when [Options.WidthMeasurer] is nil.
type DefaultWidthMeasurer struct{}

// StringWidth implements [WidthMeasurer].
func (DefaultWidthMeasurer) StringWidth(s string) int {
	return runewidth.StringWidth(s)
}
