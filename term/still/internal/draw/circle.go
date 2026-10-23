// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package draw //nolint:revive // name mirrors image/draw responsibilities

import (
	"image"
	"math"
)

// CircleMask returns an antialiased alpha mask of a filled circle that
// touches the edges of a diameter x diameter square.
func CircleMask(diameter int) *image.Alpha {
	diameter = max(diameter, 0)
	mask := image.NewAlpha(image.Rect(0, 0, diameter, diameter))
	r := float64(diameter) / 2
	for y := range diameter {
		for x := range diameter {
			dist := math.Hypot(float64(x)+0.5-r, float64(y)+0.5-r)
			mask.Pix[y*mask.Stride+x] = uint8(math.Round(255 * min(max(r-dist+0.5, 0), 1)))
		}
	}
	return mask
}
