// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package export

import (
	"image"
	"image/color"
	"image/gif"
	"io"
)

// ReplayGIF decodes r and reconstructs each animation frame as a full-canvas
// NRGBA image, undoing disposal methods (None, Background, Previous) in order.
func ReplayGIF(r io.Reader) ([]image.Image, error) {
	g, err := gif.DecodeAll(r)
	if err != nil {
		return nil, err
	}
	if len(g.Image) == 0 {
		return nil, nil
	}

	w, h := g.Config.Width, g.Config.Height
	if w <= 0 || h <= 0 {
		b := g.Image[0].Bounds()
		w, h = b.Dx(), b.Dy()
	}
	screen := image.Rect(0, 0, w, h)
	bg := gifBackgroundColor(g)

	canvas := image.NewNRGBA(screen)
	before := image.NewNRGBA(screen)
	frames := make([]image.Image, 0, len(g.Image))

	for i, frame := range g.Image {
		if i > 0 {
			prevDisp := byte(gif.DisposalNone)
			if i-1 < len(g.Disposal) {
				prevDisp = g.Disposal[i-1]
			}
			applyDisposalNRGBA(canvas, before, g.Image[i-1], prevDisp, bg, screen)
		}

		copyNRGBA(before, canvas)
		blitPalettedOntoNRGBA(canvas, frame, transparentIndex(frame))

		snap := image.NewNRGBA(screen)
		copyNRGBA(snap, canvas)
		frames = append(frames, snap)
	}

	return frames, nil
}

// gifBackgroundColor returns the GIF background color for disposal clearing.
func gifBackgroundColor(g *gif.GIF) color.Color {
	if p, ok := g.Config.ColorModel.(color.Palette); ok && int(g.BackgroundIndex) < len(p) {
		return p[g.BackgroundIndex]
	}
	if len(g.Image) > 0 && g.Image[0].Palette != nil && int(g.BackgroundIndex) < len(g.Image[0].Palette) {
		return g.Image[0].Palette[g.BackgroundIndex]
	}
	return color.RGBA{}
}

// transparentIndex returns the palette index treated as transparent for frame,
// or -1 when no transparent slot is detected.
//
// Detection is palette-driven: compacted and finalized GIFs mark the
// transparent slot with zero alpha (see transparentMarkerColor). When several
// palette entries are zero-alpha, the index referenced in frame.Pix wins.
// TransparentPaletteIndex is used only as a tie-breaker among zero-alpha slots.
func transparentIndex(frame *image.Paletted) int {
	pal := frame.Palette
	var zeroAlpha []int
	for i, c := range pal {
		if _, _, _, a := c.RGBA(); a == 0 {
			zeroAlpha = append(zeroAlpha, i)
		}
	}
	switch len(zeroAlpha) {
	case 0:
		return -1
	case 1:
		return zeroAlpha[0]
	}

	used := make(map[int]struct{}, len(zeroAlpha))
	for _, idx := range frame.Pix {
		for _, z := range zeroAlpha {
			if int(idx) == z {
				used[z] = struct{}{}
			}
		}
	}
	switch len(used) {
	case 1:
		for z := range used {
			return z
		}
	case 0:
		if len(pal) > int(TransparentPaletteIndex) {
			if _, _, _, a := pal[TransparentPaletteIndex].RGBA(); a == 0 {
				return int(TransparentPaletteIndex)
			}
		}
	}

	// Compaction appends the transparent slot last; prefer the highest index.
	return zeroAlpha[len(zeroAlpha)-1]
}

// applyDisposalNRGBA mutates canvas according to the previous frame's disposal.
func applyDisposalNRGBA(canvas, before *image.NRGBA, prev *image.Paletted, disposal byte, bg color.Color, screen image.Rectangle) {
	switch disposal {
	case gif.DisposalPrevious:
		copyNRGBA(canvas, before)
	case gif.DisposalBackground:
		clearRectNRGBA(canvas, prev.Bounds().Intersect(screen), bg)
	case gif.DisposalNone:
	}
}

// blitPalettedOntoNRGBA copies non-transparent pixels from frame onto canvas.
func blitPalettedOntoNRGBA(canvas *image.NRGBA, frame *image.Paletted, transparentIdx int) {
	pal := frame.Palette
	r := frame.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			idx := paletteIndexAt(frame, x, y)
			if transparentIdx >= 0 && int(idx) == transparentIdx {
				continue
			}
			if int(idx) < len(pal) {
				canvas.Set(x, y, pal[idx])
			}
		}
	}
}

func copyNRGBA(dst, src *image.NRGBA) {
	if dst.Bounds() != src.Bounds() {
		return
	}
	copy(dst.Pix, src.Pix)
}

func clearRectNRGBA(pm *image.NRGBA, r image.Rectangle, bg color.Color) {
	br, bgC, bb, _ := bg.RGBA()
	c := color.NRGBA{R: uint8(br >> 8), G: uint8(bgC >> 8), B: uint8(bb >> 8), A: 0xff} //nolint:gosec // RGBA contract.
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if image.Pt(x, y).In(pm.Bounds()) {
				pm.SetNRGBA(x, y, c)
			}
		}
	}
}
