// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package effects applies post-processing overlays such as unfocused dimming
// and antialiased rounded window masks after the terminal grid is drawn.
package effects

import (
	"image"
	"image/color"

	"github.com/lrstanley/x/charm/still/internal/config"
)

// WindowContext exposes window geometry for post-processing effects.
type WindowContext interface {
	Snapshot() config.Snapshot
	WindowBounds() image.Rectangle
	MarginColor() color.Color
}
