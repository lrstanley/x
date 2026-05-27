// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package export

import (
	"image"
	"image/png"
	"io"
	"os"
)

// PNG writes img to path using opts. By default [OptimizeAll] applies (indexed
// paletted PNG via the internal palette). Pass [WithOptimize]([OptimizeNone])
// to encode full RGBA with alpha preserved.
func PNG(img image.Image, path string, opts ...Option) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if werr := WritePNG(img, f, opts...); werr != nil {
		return werr
	}
	return nil
}

// WritePNG encodes img to w. See [PNG] for format behavior and option notes.
func WritePNG(img image.Image, w io.Writer, opts ...Option) error {
	cfg, err := collectOptions(formatPNG, opts...)
	if err != nil {
		return err
	}
	return encodePNG(img, w, cfg)
}

// MustPNG is a convenience wrapper around [PNG] that panics on error.
func MustPNG(img image.Image, path string, opts ...Option) {
	if err := PNG(img, path, opts...); err != nil {
		panic(err)
	}
}

func encodePNG(img image.Image, w io.Writer, cfg options) error {
	if cfg.optimize.has(OptimizeColorQuantization) {
		pal := defaultGIFPalette()
		pm := image.NewPaletted(img.Bounds(), pal)
		palettizeInto(pm, img)
		compactPalette([]*image.Paletted{pm}, pal)
		return png.Encode(w, pm)
	}
	return png.Encode(w, img)
}
