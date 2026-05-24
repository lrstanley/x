// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package raster lays out and draws terminal glyphs—including grid text,
// powerline symbols, and Nerd Font icons—into coverage buffers for compositing.
package raster

import (
	"image"
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"
	idraw "github.com/lrstanley/x/charm/still/internal/draw"
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
	Mode      rasterMode
	Target    image.Rectangle
	Glyph     string
	Fg        color.Color
	ScaleMode idraw.ScaleMode
	AlignEnd  bool
}

// LayoutGlyph resolves face, baseline dot, and raster mode for a cell glyph.
func LayoutGlyph(ctx GlyphContext, area image.Rectangle, cell *uv.Cell, fg color.Color) (Layout, bool) {
	face := ctx.FontFace(cell)
	if face == nil {
		return Layout{}, false
	}

	metrics := ctx.Metrics()
	glyph := ctx.Glyph(cell)
	dot := fixed.Point26_6{
		X: fixed.I(area.Min.X),
		Y: fixed.I(area.Max.Y - metrics.FontBaseline.Int()),
	}

	kind, scaleMode, alignEnd := classifyGlyph(ctx, face, glyph)
	layout := Layout{
		Kind:  kind,
		Face:  face,
		Dot:   dot,
		Mode:  rasterDirect,
		Glyph: glyph,
		Fg:    fg,
	}

	switch kind {
	case KindGrid:
		return layout, true
	case KindPowerline:
		layout.Mode = rasterScaled
		layout.Target = area
		layout.ScaleMode = scaleMode
		layout.AlignEnd = alignEnd
		return layout, true
	case KindNerdIcon:
		dot.Y += fixed.I(idraw.GlyphVerticalAdjust(face, dot, glyph, area))
		layout.Dot = dot
		if dr, ok := idraw.GlyphRasterBounds(face, dot, glyph); ok {
			target := idraw.FallbackGlyphTarget(ctx, area, cell)
			if idraw.FallbackGlyphNeedsScale(dr, target) {
				layout.Mode = rasterScaled
				layout.Target = target
			}
		}
		return layout, true
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
