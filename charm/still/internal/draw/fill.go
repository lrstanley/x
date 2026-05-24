// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package draw //nolint:revive // name mirrors image/draw responsibilities

import (
	"image"
	"image/color"
	"image/draw"
	"sync"

	icol "github.com/lrstanley/x/charm/still/internal/color"
)

var fillUniforms sync.Map // color.NRGBA -> *image.Uniform

// Fill paints area with a solid color using [draw.Src].
func Fill(img draw.Image, area image.Rectangle, c color.Color) {
	if area.Empty() {
		return
	}
	col := icol.NRGBA(c)
	if dst, ok := img.(*image.NRGBA); ok {
		b := dst.Bounds()
		area = area.Intersect(b)
		if area.Empty() {
			return
		}
		stride := dst.Stride
		for y := area.Min.Y; y < area.Max.Y; y++ {
			off := (y-b.Min.Y)*stride + (area.Min.X-b.Min.X)*4
			row := dst.Pix[off : off+(area.Dx()*4)]
			for i := 0; i < len(row); i += 4 {
				row[i] = col.R
				row[i+1] = col.G
				row[i+2] = col.B
				row[i+3] = col.A
			}
		}
		return
	}
	u, ok := fillUniforms.Load(col)
	if !ok {
		u = image.NewUniform(col)
		fillUniforms.Store(col, u)
	}
	uniform, ok := u.(*image.Uniform)
	if !ok {
		panic("fillUniforms stored non-Uniform value")
	}
	draw.Draw(img, area, uniform, image.Point{}, draw.Src)
}

// Overlay blends a solid color over area using [draw.Over].
func Overlay(img draw.Image, area image.Rectangle, c color.Color) {
	if area.Empty() {
		return
	}
	col := icol.NRGBA(c)
	u, ok := fillUniforms.Load(col)
	if !ok {
		u = image.NewUniform(col)
		fillUniforms.Store(col, u)
	}
	uniform, ok := u.(*image.Uniform)
	if !ok {
		panic("fillUniforms stored non-Uniform value")
	}
	draw.Draw(img, area, uniform, image.Point{}, draw.Over)
}
