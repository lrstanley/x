// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package draw //nolint:revive // name mirrors image/draw responsibilities

import (
	"image"
	"image/color"
	"image/draw"
)

// OverCoverage blends col over area of img. In every row of area, the pixel at
// column x is weighted by cov[x-area.Min.X], 0 through 255, times col's alpha,
// so cov is the same coverage row for each row of the area, and must hold one
// byte for each column of it. The destination alpha is blended, not forced
// opaque, so translucent windows stay translucent.
//
// NRGBA is blended directly: [draw.DrawMask] converts every pixel of an NRGBA
// destination through premultiplied 16-bit color, several times slower.
func OverCoverage(img draw.Image, area image.Rectangle, col color.NRGBA, cov []uint8) {
	clipped := area.Intersect(img.Bounds())
	if clipped.Empty() {
		return
	}
	dst, ok := img.(*image.NRGBA)
	if !ok {
		mask := &image.Alpha{Pix: cov, Rect: area} // stride 0: every row is cov
		draw.DrawMask(img, clipped, Uniform(col), image.Point{}, mask, clipped.Min, draw.Over)
		return
	}
	w := clipped.Dx()
	row := cov[clipped.Min.X-area.Min.X:][:w]
	for y := clipped.Min.Y; y < clipped.Max.Y; y++ {
		px := dst.Pix[dst.PixOffset(clipped.Min.X, y):][:4*w]
		for x, c := range row {
			overPixel(px[4*x:4*x+4:4*x+4], col, c)
		}
	}
}

// overPixel blends col at coverage cov over the NRGBA pixel px.
func overPixel(px []byte, col color.NRGBA, cov uint8) {
	a := int(cov)
	if col.A != 255 {
		a = int(div255(a * int(col.A)))
	}
	switch da := int(px[3]); {
	case a == 0:
	case a == 255:
		px[0], px[1], px[2], px[3] = col.R, col.G, col.B, 255
	case da == 255:
		inv := 255 - a
		px[0] = div255(int(col.R)*a + int(px[0])*inv)
		px[1] = div255(int(col.G)*a + int(px[1])*inv)
		px[2] = div255(int(col.B)*a + int(px[2])*inv)
	default:
		// Non-premultiplied source-over. oa is 255 times the result alpha,
		// and is not zero because a is not.
		inv := 255 - a
		oa := a*255 + da*inv
		px[0] = u8((int(col.R)*a*255 + int(px[0])*da*inv + oa/2) / oa)
		px[1] = u8((int(col.G)*a*255 + int(px[1])*da*inv + oa/2) / oa)
		px[2] = u8((int(col.B)*a*255 + int(px[2])*da*inv + oa/2) / oa)
		px[3] = u8((oa + 127) / 255)
	}
}

// div255 is v/255 rounded, for v in 0 through 255*255.
func div255(v int) uint8 {
	return u8((v + 1 + (v >> 8)) >> 8)
}

// u8 converts a channel value that the blend math keeps within 0 through 255.
func u8(v int) uint8 {
	return uint8(v) //nolint:gosec // callers keep v in 0..255.
}
