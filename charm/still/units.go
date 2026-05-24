// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"time"

	"github.com/lrstanley/x/charm/still/units"
)

const (
	DefaultPPI              = units.DefaultPPI
	DefaultDPI              = units.DPI(96)
	DefaultFontSize         = units.Pt(11)
	DefaultCursorBlinkSpeed = 600 * time.Millisecond
	DefaultBgOpacity        = 1.0
	DefaultFaintFactor      = 0.5
	DefaultFocusDimming     = 0.16
)

type (
	Px           = units.Px
	Pt           = units.Pt
	DPI          = units.DPI
	Percent      = units.Percent
	FractionalPx = units.FractionalPx
	Adjustment   = units.Adjustment
)

// NewAdjustment returns a clamped adjustment in [-1, 1].
func NewAdjustment(v float64) Adjustment {
	return units.NewAdjustment(v)
}
