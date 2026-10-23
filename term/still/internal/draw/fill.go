// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package draw //nolint:revive // name mirrors image/draw responsibilities

import (
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"sync"

	icol "github.com/lrstanley/x/term/still/internal/color"
)

var fillUniforms sync.Map // color.NRGBA -> *image.Uniform

// Fill paints area with a solid color using [draw.Src].
func Fill(img draw.Image, area image.Rectangle, col color.NRGBA) {
	if area.Empty() {
		return
	}
	if dst, ok := img.(*image.NRGBA); ok {
		b := dst.Bounds()
		area = area.Intersect(b)
		if area.Empty() {
			return
		}
		stride := dst.Stride
		pixel := uint32(col.R) | uint32(col.G)<<8 | uint32(col.B)<<16 | uint32(col.A)<<24
		rowBytes := area.Dx() * 4
		off0 := (area.Min.Y-b.Min.Y)*stride + (area.Min.X-b.Min.X)*4
		row0 := dst.Pix[off0 : off0+rowBytes]
		fillSpan(row0, pixel)
		for y := area.Min.Y + 1; y < area.Max.Y; y++ {
			off := (y-b.Min.Y)*stride + (area.Min.X-b.Min.X)*4
			copy(dst.Pix[off:off+rowBytes], row0)
		}
		return
	}
	draw.Draw(img, area, Uniform(col), image.Point{}, draw.Src)
}

// fillSpan writes pixel across row. row length is a positive multiple of 4.
// The first pixel is stored, then the filled prefix is doubled with copy so
// wide backgrounds use memmove instead of a store per pixel.
func fillSpan(row []byte, pixel uint32) {
	binary.LittleEndian.PutUint32(row[:4], pixel)
	for i := 4; i < len(row); i *= 2 {
		copy(row[i:], row[:i])
	}
}

// Overlay blends a solid color over area using [draw.Over].
func Overlay(img draw.Image, area image.Rectangle, c color.Color) {
	if area.Empty() {
		return
	}
	draw.Draw(img, area, Uniform(icol.NRGBA(c)), image.Point{}, draw.Over)
}

// Uniform returns a shared [image.Uniform] of col, so repeated draws in one
// color do not allocate a source image each time.
func Uniform(col color.NRGBA) *image.Uniform {
	u, ok := fillUniforms.Load(col)
	if !ok {
		u = image.NewUniform(col)
		fillUniforms.Store(col, u)
	}
	uniform, ok := u.(*image.Uniform)
	if !ok {
		panic("fillUniforms stored non-Uniform value")
	}
	return uniform
}
