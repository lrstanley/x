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

	"github.com/lrstanley/x/term/still/internal/config"
	idraw "github.com/lrstanley/x/term/still/internal/draw"
	"github.com/lrstanley/x/term/still/types"
	"github.com/lrstanley/x/term/vt/screen"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// GlyphContext exposes glyph layout and rasterization inputs.
type GlyphContext interface {
	config.CellFrameSource
	FontFace(cell *screen.Cell) font.Face
	UsesGridLayout(face font.Face) bool
	Glyph(cell *screen.Cell) string
	BoxThicknessOverride() bool
}

// Kind classifies glyph layout strategy.
type Kind uint8

const (
	// KindGrid glyphs come from a primary grid face and sit on the cell
	// baseline.
	KindGrid Kind = iota
	// KindPowerline glyphs are stretched or fitted to the cell.
	KindPowerline
	// KindFallback glyphs come from any other face (Nerd Font icons, bundled
	// and system fallback fonts) and are fitted into the cell when they
	// overflow it.
	KindFallback
	// KindSprite glyphs ([IsSpriteRune]) are drawn from the cell geometry and
	// have no face.
	KindSprite
)

// Layout holds resolved glyph rasterization parameters.
type Layout struct {
	Kind      Kind
	Face      font.Face
	Dot       fixed.Point26_6
	Area      image.Rectangle
	Target    image.Rectangle // Band that Powerline and Nerd Font glyphs scale into.
	Glyph     string
	Rune      rune // First rune of Glyph.
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
// Sprites are recognized before any face is looked up, since they never use
// one.
func LayoutGlyph(ctx GlyphContext, area image.Rectangle, cell *screen.Cell, fg color.NRGBA, out *Layout) bool {
	glyph := ctx.Glyph(cell)
	r, ok := idraw.FirstRune(glyph)
	if !ok {
		return false
	}
	if IsSpriteRune(r) {
		*out = Layout{Kind: KindSprite, Area: area, Glyph: glyph, Rune: r, Fg: fg}
		return true
	}

	face := ctx.FontFace(cell)
	if face == nil {
		return false
	}

	metrics := ctx.Metrics()
	dot := fixed.Point26_6{
		X: GridGlyphDotX(area, metrics),
		Y: fixed.I(area.Max.Y - metrics.FontBaseline.Int()),
	}

	kind, scaleMode, alignEnd := classifyGlyph(ctx, face, glyph)
	*out = Layout{
		Kind:      kind,
		Face:      face,
		Dot:       dot,
		Area:      area,
		Glyph:     glyph,
		Rune:      r,
		Fg:        fg,
		ScaleMode: scaleMode,
		AlignEnd:  alignEnd,
	}
	switch kind {
	case KindGrid, KindSprite:
	case KindPowerline:
		out.Target = area
	case KindFallback:
		out.Target = idraw.FallbackGlyphTarget(ctx, area, cell)
	}
	return true
}

func classifyGlyph(ctx GlyphContext, face font.Face, glyph string) (Kind, idraw.ScaleMode, bool) {
	if ctx.UsesGridLayout(face) {
		return KindGrid, idraw.ScaleDefault, false
	}
	scaleMode, alignEnd := idraw.PowerlineFallbackScale(glyph)
	if scaleMode != idraw.ScaleDefault {
		return KindPowerline, scaleMode, alignEnd
	}
	return KindFallback, idraw.ScaleDefault, false
}
