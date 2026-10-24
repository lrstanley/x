// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package draw //nolint:revive // name mirrors image/draw responsibilities

import (
	"image"
	"image/color"
	"image/draw"

	icol "github.com/lrstanley/x/term/still/internal/color"
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
			OverPixel(px[4*x:4*x+4:4*x+4], col, c)
		}
	}
}

// OverPixel blends col at coverage cov over px, the four bytes of one
// [image.NRGBA] pixel, with the same blending as [OverCoverage].
func OverPixel(px []byte, col color.NRGBA, cov uint8) {
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

// OverAt blends col at coverage cov over the pixel (x, y) of img, with the
// same blending as [OverCoverage]. Pixels outside img are left alone.
// Destinations other than [image.NRGBA] are read and written through
// [draw.RGBA64Image] when they have it, so nothing is boxed.
func OverAt(img draw.Image, x, y int, col color.NRGBA, cov uint8) {
	if cov == 0 || !image.Pt(x, y).In(img.Bounds()) {
		return
	}
	if dst, ok := img.(*image.NRGBA); ok {
		i := dst.PixOffset(x, y)
		OverPixel(dst.Pix[i:i+4:i+4], col, cov)
		return
	}
	px := nrgbaAt(img, x, y)
	buf := [4]byte{px.R, px.G, px.B, px.A}
	OverPixel(buf[:], col, cov)
	setNRGBA(img, x, y, color.NRGBA{R: buf[0], G: buf[1], B: buf[2], A: buf[3]})
}

// OverRow blends col at coverage cov over the pixels x0..x1 of row y of img,
// as [OverAt] does each of them. Pixels outside img are left alone.
func OverRow(img draw.Image, x0, x1, y int, col color.NRGBA, cov uint8) {
	b := img.Bounds()
	x0, x1 = max(x0, b.Min.X), min(x1, b.Max.X)
	if cov == 0 || x0 >= x1 || y < b.Min.Y || y >= b.Max.Y {
		return
	}
	dst, ok := img.(*image.NRGBA)
	if !ok {
		for x := x0; x < x1; x++ {
			OverAt(img, x, y, col, cov)
		}
		return
	}
	px := dst.Pix[dst.PixOffset(x0, y):][:4*(x1-x0)]
	for i := 0; i < len(px); i += 4 {
		OverPixel(px[i:i+4:i+4], col, cov)
	}
}

// RecolorPixel blends the color of px, the four bytes of one [image.NRGBA]
// pixel, toward col by k, keeping its alpha, when px is translucent, and
// reports whether it is. A transparent pixel is left alone. It recolors what
// is there instead of adding to its alpha, so a stroke over an antialiased
// edge does not leave a brighter blob where the edge was faint.
func RecolorPixel(px []byte, col color.NRGBA, k float64) bool {
	if px[3] == 0xff {
		return false
	}
	if px[3] > 0 && k > 0 {
		c := icol.BlendNRGBA(color.NRGBA{R: px[0], G: px[1], B: px[2], A: px[3]}, color.NRGBA{R: col.R, G: col.G, B: col.B, A: px[3]}, k)
		px[0], px[1], px[2] = c.R, c.G, c.B
	}
	return true
}

// RecolorAt is [RecolorPixel] for the pixel (x, y) of img, which must be in
// its bounds.
func RecolorAt(img draw.Image, x, y int, col color.NRGBA, k float64) bool {
	if dst, ok := img.(*image.NRGBA); ok {
		i := dst.PixOffset(x, y)
		return RecolorPixel(dst.Pix[i:i+4:i+4], col, k)
	}
	px := nrgbaAt(img, x, y)
	buf := [4]byte{px.R, px.G, px.B, px.A}
	if !RecolorPixel(buf[:], col, k) {
		return false
	}
	setNRGBA(img, x, y, color.NRGBA{R: buf[0], G: buf[1], B: buf[2], A: buf[3]})
	return true
}

// nrgbaAt reads the pixel (x, y) of img as NRGBA, as [color.NRGBAModel]
// converts it.
func nrgbaAt(img image.Image, x, y int) color.NRGBA {
	src, ok := img.(image.RGBA64Image)
	if !ok {
		px, _ := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA) // NRGBAModel always returns NRGBA.
		return px
	}
	c := src.RGBA64At(x, y)
	r, g, b, a := uint32(c.R), uint32(c.G), uint32(c.B), uint32(c.A)
	switch a {
	case 0xffff:
		return color.NRGBA{R: u8(int(r >> 8)), G: u8(int(g >> 8)), B: u8(int(b >> 8)), A: 0xff}
	case 0:
		return color.NRGBA{}
	}
	// The same un-premultiplying as color.NRGBAModel.
	r, g, b = r*0xffff/a, g*0xffff/a, b*0xffff/a
	return color.NRGBA{R: u8(int(r >> 8)), G: u8(int(g >> 8)), B: u8(int(b >> 8)), A: u8(int(a >> 8))}
}

// setNRGBA writes c to the pixel (x, y) of img.
func setNRGBA(img draw.Image, x, y int, c color.NRGBA) {
	dst, ok := img.(draw.RGBA64Image)
	if !ok {
		img.Set(x, y, c)
		return
	}
	r, g, b, a := c.RGBA()
	dst.SetRGBA64(x, y, color.RGBA64{R: u16(r), G: u16(g), B: u16(b), A: u16(a)})
}

// u16 converts a 16-bit channel value from [color.Color.RGBA].
func u16(v uint32) uint16 {
	return uint16(v) //nolint:gosec // RGBA keeps channels in 0..0xffff.
}
