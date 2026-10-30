// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"

	"github.com/lrstanley/x/term/reel/vt/internal/wasmvt"
)

// decodeKittyPixels converts raw pixels in a GhosttyKittyImageFormat to an
// NRGBA image. It returns nil when data is short.
func decodeKittyPixels(data []byte, format int32, w, h int) *image.NRGBA {
	var bpp int
	switch format {
	case wasmvt.KittyImageFormatRgba:
		bpp = 4
	case wasmvt.KittyImageFormatRgb:
		bpp = 3
	case wasmvt.KittyImageFormatGrayAlpha:
		bpp = 2
	case wasmvt.KittyImageFormatGray:
		bpp = 1
	default:
		return nil
	}
	if len(data) < w*h*bpp {
		return nil
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	if bpp == 4 {
		copy(img.Pix, data)
		return img
	}
	for i, o := 0, 0; o < len(img.Pix); i, o = i+bpp, o+4 {
		px, in := img.Pix[o:o+4:o+4], data[i:i+bpp:i+bpp]
		switch len(in) {
		case 3:
			px[0], px[1], px[2], px[3] = in[0], in[1], in[2], 0xff //nolint:gosec // G602: in has len(in) bytes.
		case 2:
			px[0], px[1], px[2], px[3] = in[0], in[0], in[0], in[1] //nolint:gosec // G602: in has len(in) bytes.
		case 1:
			px[0], px[1], px[2], px[3] = in[0], in[0], in[0], 0xff
		}
	}
	return img
}

// onDecodePNG is GhosttySysDecodePngFn. It decodes the PNG at data/n into
// RGBA pixels allocated with alloc, and fills the GhosttySysImage at out.
// It returns 1 on success.
func (t *Terminal) onDecodePNG(_, alloc, data, n, out int32) int32 {
	src, err := png.Decode(bytes.NewReader(t.mod.Slice(data, int(n))))
	if err != nil {
		return 0
	}
	b := src.Bounds()
	img := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(img, img.Bounds(), src, b.Min, draw.Src)
	size := len(img.Pix)
	buf := t.mod.Xghostty_alloc(alloc, wasmvt.Int32(size))
	if buf == 0 {
		return 0
	}
	t.mod.Put(buf, img.Pix)
	t.mod.SetU32(out, wasmvt.SysImageWidthOff, uint32(b.Dx()))  //nolint:gosec // decoded sizes are positive.
	t.mod.SetU32(out, wasmvt.SysImageHeightOff, uint32(b.Dy())) //nolint:gosec // decoded sizes are positive.
	t.mod.SetI32(out, wasmvt.SysImageDataOff, buf)
	t.mod.SetU32(out, wasmvt.SysImageDataLenOff, uint32(size)) //nolint:gosec // fits the Wasm address space.
	return 1
}
