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
// to encode full RGBA with alpha preserved. Options are checked before path
// is opened, so an invalid option leaves an existing file alone.
func PNG(img image.Image, path string, opts ...Option) error {
	cfg, err := collectOptions(validatePNG, opts...)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	return encodePNG(img, f, cfg)
}

// WritePNG encodes img to w. See [PNG] for format behavior and option notes.
func WritePNG(img image.Image, w io.Writer, opts ...Option) error {
	cfg, err := collectOptions(validatePNG, opts...)
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
		adaptive := newAdaptivePalette(false)
		pm := image.NewPaletted(img.Bounds(), adaptive.pal)
		adaptive.palettize(pm, img)
		compactPalette([]*image.Paletted{pm}, adaptive.pal)
		return png.Encode(w, pm)
	}
	return png.Encode(w, img)
}
