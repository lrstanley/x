// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package alpha provides shared alpha-mask raster utilities.
package alpha

import (
	"image"
	"image/color"
	"image/draw"
)

// InkBounds returns the tight bounding box of non-zero alpha in alpha.
func InkBounds(alpha *image.Alpha) (image.Rectangle, bool) {
	bounds := image.Rectangle{
		Min: image.Pt(alpha.Bounds().Max.X, alpha.Bounds().Max.Y),
		Max: image.Pt(alpha.Bounds().Min.X, alpha.Bounds().Min.Y),
	}
	for y := alpha.Bounds().Min.Y; y < alpha.Bounds().Max.Y; y++ {
		for x := alpha.Bounds().Min.X; x < alpha.Bounds().Max.X; x++ {
			if alpha.AlphaAt(x, y).A == 0 {
				continue
			}
			if x < bounds.Min.X {
				bounds.Min.X = x
			}
			if y < bounds.Min.Y {
				bounds.Min.Y = y
			}
			if x+1 > bounds.Max.X {
				bounds.Max.X = x + 1
			}
			if y+1 > bounds.Max.Y {
				bounds.Max.Y = y + 1
			}
		}
	}
	return bounds, bounds.Min.X < bounds.Max.X && bounds.Min.Y < bounds.Max.Y
}

// SetMax writes a to dst at (x,y) only when a exceeds the current value.
func SetMax(dst *image.Alpha, x, y int, a uint8) {
	if !image.Pt(x, y).In(dst.Bounds()) {
		return
	}
	if cur := dst.AlphaAt(x, y).A; a > cur {
		dst.SetAlpha(x, y, color.Alpha{A: a})
	}
}

// Dilate expands non-zero pixels by radiusX/radiusY using max-alpha union.
func Dilate(alpha *image.Alpha, radiusX, radiusY int) *image.Alpha {
	if radiusX <= 0 && radiusY <= 0 {
		return alpha
	}
	b := alpha.Bounds()
	out := image.NewAlpha(image.Rect(0, 0, b.Dx()+2*radiusX, b.Dy()+2*radiusY))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			a := alpha.AlphaAt(x, y).A
			if a == 0 {
				continue
			}
			for dy := -radiusY; dy <= radiusY; dy++ {
				for dx := -radiusX; dx <= radiusX; dx++ {
					SetMax(out, x-b.Min.X+radiusX+dx, y-b.Min.Y+radiusY+dy, a)
				}
			}
		}
	}
	return out
}

// RasterGlyph copies a glyph mask into a local alpha image aligned to dr.
func RasterGlyph(dr image.Rectangle, mask image.Image, maskp image.Point) *image.Alpha {
	out := image.NewAlpha(image.Rect(0, 0, dr.Dx(), dr.Dy()))
	draw.DrawMask(out, out.Bounds(), image.Opaque, image.Point{}, mask, maskp, draw.Src)
	return out
}

// At reads the alpha channel at p from any [image.Image].
func At(img image.Image, p image.Point) uint8 {
	_, _, _, a := img.At(p.X, p.Y).RGBA()
	a >>= 8
	if a > 0xff {
		return 0xff
	}
	return uint8(a)
}

// DilateX expands ink to the right by up to radius pixels per column.
func DilateX(src *image.Alpha, radius int) *image.Alpha {
	b := src.Bounds()
	dst := image.NewAlpha(image.Rect(0, 0, b.Dx()+radius, b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			a := src.AlphaAt(x, y).A
			if a == 0 {
				continue
			}
			for dx := range radius + 1 {
				SetMax(dst, x-b.Min.X+dx, y-b.Min.Y, a)
			}
		}
	}
	return dst
}
