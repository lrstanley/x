// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package raster lays out and draws terminal glyphs—including grid text,
// powerline symbols, and Nerd Font icons—into coverage buffers for compositing.
package raster

import (
	"image"
	"image/color"
	"math"

	uv "github.com/charmbracelet/ultraviolet"
	idraw "github.com/lrstanley/x/charm/still/internal/draw"
	"github.com/lrstanley/x/charm/still/types"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// GlyphContext exposes glyph layout and rasterization inputs.
type GlyphContext interface {
	idraw.FrameContext
	FontFace(cell *uv.Cell) font.Face
	UsesGridLayout(face font.Face) bool
	Glyph(cell *uv.Cell) string
}

// Kind classifies glyph layout strategy.
type Kind uint8

const (
	KindGrid Kind = iota
	KindPowerline
	KindNerdIcon
)

type rasterMode uint8

const (
	rasterDirect rasterMode = iota
	rasterScaled
)

// Layout holds resolved glyph rasterization parameters.
type Layout struct {
	Kind      Kind
	Face      font.Face
	Dot       fixed.Point26_6
	Area      image.Rectangle
	Mode      rasterMode
	Target    image.Rectangle
	Glyph     string
	Fg        color.NRGBA
	ScaleMode idraw.ScaleMode
	AlignEnd  bool
}

// GridGlyphDotX returns the horizontal pen position for a grid glyph centered
// within area using Ghostty-style subpixel adjustment when the face is wider
// than the cell.
func GridGlyphDotX(area image.Rectangle, metrics types.Metrics) fixed.Int26_6 {
	dx := (float64(area.Dx()) - metrics.FaceWidth.Float64()) / 2
	x := float64(area.Min.X) + dx
	if dx < 0 {
		x -= math.Trunc(dx)
	}
	return fixed.Int26_6(math.Round(x * 64))
}

// LayoutGlyph resolves face, baseline dot, and raster mode for a cell glyph.
func LayoutGlyph(ctx GlyphContext, area image.Rectangle, cell *uv.Cell, fg color.NRGBA, out *Layout) bool {
	face := ctx.FontFace(cell)
	if face == nil {
		return false
	}

	metrics := ctx.Metrics()
	glyph := ctx.Glyph(cell)
	dot := fixed.Point26_6{
		X: GridGlyphDotX(area, metrics),
		Y: fixed.I(area.Max.Y - metrics.FontBaseline.Int()),
	}

	kind, scaleMode, alignEnd := classifyGlyph(ctx, face, glyph)
	out.Kind = kind
	out.Face = face
	out.Dot = dot
	out.Area = area
	out.Mode = rasterDirect
	out.Glyph = glyph
	out.Fg = fg
	out.Target = image.Rectangle{}
	out.ScaleMode = idraw.ScaleDefault
	out.AlignEnd = false

	switch kind {
	case KindGrid:
		return true
	case KindPowerline:
		out.Mode = rasterScaled
		out.Target = area
		out.ScaleMode = scaleMode
		out.AlignEnd = alignEnd
		return true
	case KindNerdIcon:
		dot.Y += fixed.I(idraw.GlyphVerticalAdjust(face, dot, glyph, area))
		out.Dot = dot
		if dr, ok := idraw.GlyphRasterBounds(face, dot, glyph); ok {
			target := idraw.FallbackGlyphTarget(ctx, area, cell)
			if idraw.FallbackGlyphNeedsScale(dr, target) {
				out.Mode = rasterScaled
				out.Target = target
			}
		}
		return true
	default:
		panic("invalid glyph kind")
	}
}

func classifyGlyph(ctx GlyphContext, face font.Face, glyph string) (Kind, idraw.ScaleMode, bool) {
	if ctx.UsesGridLayout(face) {
		return KindGrid, idraw.ScaleDefault, false
	}
	scaleMode, alignEnd := idraw.PowerlineFallbackScale(glyph)
	if scaleMode != idraw.ScaleDefault {
		return KindPowerline, scaleMode, alignEnd
	}
	return KindNerdIcon, idraw.ScaleDefault, false
}
