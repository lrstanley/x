// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image"
	"image/color"
	"image/draw"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

type glyphKind uint8

const (
	// glyphGrid is primary monospace text: direct raster at the cell baseline and
	// max-alpha coverage compositing on NRGBA destinations.
	glyphGrid glyphKind = iota
	// glyphPowerline is a mapped-face Powerline separator scaled to the full cell
	// with Ghostty-style stretch or fit_cover1 rules.
	glyphPowerline
	// glyphNerdIcon is a mapped-face Nerd Font icon vertically centered in the
	// icon-height band and downscaled when ink overflows.
	glyphNerdIcon
)

type rasterMode uint8

const (
	rasterDirect rasterMode = iota
	rasterScaled
)

type fallbackScaleMode uint8

const (
	scaleDefault fallbackScaleMode = iota
	scaleStretch
	scaleFitCover1
)

type glyphLayout struct {
	kind      glyphKind
	face      font.Face
	dot       fixed.Point26_6
	mode      rasterMode
	target    image.Rectangle
	glyph     string
	fg        color.Color
	scaleMode fallbackScaleMode
	alignEnd  bool
}

// classifyGlyph selects grid, powerline, or nerd-icon layout once per glyph.
// Powerline codepoints take precedence over the nerd-icon band even on mapped faces.
func classifyGlyph(ctx Context, face font.Face, glyph string) (glyphKind, fallbackScaleMode, bool) {
	if ctx.usesGridLayout(face) {
		return glyphGrid, scaleDefault, false
	}
	scaleMode, alignEnd := powerlineFallbackScale(glyph)
	if scaleMode != scaleDefault {
		return glyphPowerline, scaleMode, alignEnd
	}
	return glyphNerdIcon, scaleDefault, false
}

// layoutGlyph resolves face, baseline dot, and raster mode for a cell glyph.
func layoutGlyph(ctx Context, area image.Rectangle, cell *uv.Cell, fg color.Color) (glyphLayout, bool) {
	face := ctx.FontFace(cell)
	if face == nil {
		return glyphLayout{}, false
	}

	metrics := ctx.Metrics()
	glyph := ctx.Glyph(cell)
	dot := fixed.Point26_6{
		X: fixed.I(area.Min.X),
		Y: fixed.I(area.Max.Y - metrics.FontBaseline.Int()),
	}

	kind, scaleMode, alignEnd := classifyGlyph(ctx, face, glyph)
	layout := glyphLayout{
		kind:  kind,
		face:  face,
		dot:   dot,
		mode:  rasterDirect,
		glyph: glyph,
		fg:    fg,
	}

	switch kind {
	case glyphGrid:
		return layout, true
	case glyphPowerline:
		layout.mode = rasterScaled
		layout.target = area
		layout.scaleMode = scaleMode
		layout.alignEnd = alignEnd
		return layout, true
	case glyphNerdIcon:
		dot.Y += fixed.I(glyphVerticalAdjust(face, dot, glyph, area))
		layout.dot = dot
		if dr, ok := glyphRasterBounds(face, dot, glyph); ok {
			target := fallbackGlyphTarget(ctx, area, cell)
			if fallbackGlyphNeedsScale(dr, target) {
				layout.mode = rasterScaled
				layout.target = target
			}
		}
		return layout, true
	default:
		panic("invalid glyph kind")
	}
}

// drawGlyph rasterizes the cell's grapheme into area using the face from
// [Context.FontFace] and a baseline derived from [Context.Metrics].
func drawGlyph(ctx Context, img draw.Image, area image.Rectangle, cell *uv.Cell, fg color.Color) {
	layout, ok := layoutGlyph(ctx, area, cell, fg)
	if !ok {
		return
	}
	drawGlyphLayout(ctx, img, layout)
}

func drawGlyphLayout(ctx Context, img draw.Image, layout glyphLayout) {
	if mask, ok := layoutBoxGlyphMask(ctx, layout); ok {
		drawBoxGlyphMask(img, layout.fg, mask)
		return
	}
	switch layout.mode {
	case rasterScaled:
		drawFallbackGlyphScaled(img, layout.target, layout.face, layout.fg, layout.glyph, layout.dot, layout.scaleMode, layout.alignEnd)
	case rasterDirect:
		drawer := font.Drawer{
			Dst:  img,
			Src:  image.NewUniform(layout.fg),
			Face: layout.face,
			Dot:  layout.dot,
		}
		drawer.DrawString(layout.glyph)
	default:
		panic("invalid glyph layout mode")
	}
}

func accumulateGlyphLayout(ctx Context, cov *glyphCoverage, layout glyphLayout) {
	if mask, ok := layoutBoxGlyphMask(ctx, layout); ok {
		fg := nrgba(layout.fg)
		dr := mask.dr
		for y := dr.Min.Y; y < dr.Max.Y; y++ {
			for x := dr.Min.X; x < dr.Max.X; x++ {
				ax := x - dr.Min.X
				ay := y - dr.Min.Y
				if ga := int(mask.alpha.AlphaAt(ax, ay).A); ga != 0 {
					cov.accumulate(x, y, uint8(ga), fg)
				}
			}
		}
		return
	}
	r, n := utf8.DecodeRuneInString(layout.glyph)
	if n == 0 || r == utf8.RuneError {
		return
	}
	dr, mask, maskp, _, ok := layout.face.Glyph(layout.dot, r)
	if !ok || dr.Empty() || mask == nil {
		return
	}

	fg := nrgba(layout.fg)
	for y := dr.Min.Y; y < dr.Max.Y; y++ {
		for x := dr.Min.X; x < dr.Max.X; x++ {
			if ga := glyphMaskAlpha(mask, maskp, dr, x, y); ga != 0 {
				cov.accumulate(x, y, ga, fg)
			}
		}
	}
}

func glyphMaskAlpha(mask image.Image, maskp image.Point, dr image.Rectangle, x, y int) uint8 {
	mx := maskp.X + (x - dr.Min.X)
	my := maskp.Y + (y - dr.Min.Y)
	if !image.Pt(mx, my).In(mask.Bounds()) {
		return 0
	}
	_, _, _, a := mask.At(mx, my).RGBA()
	return uint8(a >> 8)
}
