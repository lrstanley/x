// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"image/color"
	"image/draw"
	"math"

	idraw "github.com/lrstanley/x/charm/still/internal/draw"
)

// ApplyFocusDimming darkens the terminal window when unfocused.
func ApplyFocusDimming(ctx WindowContext, img draw.Image) {
	cfg := ctx.Snapshot()
	if cfg.FocusDimming <= 0 {
		return
	}
	idraw.Overlay(img, ctx.WindowBounds(), color.NRGBA{A: uint8(math.Round(255 * cfg.FocusDimming))})
}
