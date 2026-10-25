// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package draw //nolint:revive // name mirrors image/draw responsibilities

import "image"

// ColorGlyph is a glyph that draws itself in its own colors, such as an emoji,
// instead of as a mask tinted by the foreground.
type ColorGlyph interface {
	// Image draws the glyph fitted into a w×h box, as premultiplied RGBA. The
	// result is shared and must not be modified. It returns nil if the glyph
	// cannot be drawn.
	Image(w, h int) *image.RGBA
}
