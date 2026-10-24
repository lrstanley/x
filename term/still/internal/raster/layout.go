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
	"unicode/utf8"

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
	FontFace(cell *screen.Cell, cluster idraw.Cluster) font.Face
	UsesGridLayout(face font.Face) bool
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
	SpansNext bool            // Target covers the next cell too: a one-cell icon with room to grow.
	Cluster   idraw.Cluster   // Marks are drawn over the base by markedGlyphMask.
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
//
// spansNext lets a one-cell icon grow into the cell after it, as decided by
// [IsSpanIcon] and [IconRoom]. Only fallback glyphs use the room.
func LayoutGlyph(ctx GlyphContext, area image.Rectangle, cell *screen.Cell, spansNext bool, fg color.NRGBA, out *Layout) bool {
	cluster, ok := idraw.NewCluster(cell.Content)
	if !ok {
		return false
	}
	if IsSpriteRune(cluster.Base) {
		*out = Layout{Kind: KindSprite, Area: area, Cluster: cluster, Fg: fg}
		return true
	}

	face := ctx.FontFace(cell, cluster)
	if face == nil {
		return false
	}

	kind, scaleMode, alignEnd := classifyGlyph(ctx, face, cluster.Text)
	*out = Layout{
		Kind:      kind,
		Face:      face,
		Cluster:   cluster,
		Fg:        fg,
		ScaleMode: scaleMode,
		AlignEnd:  alignEnd,
		SpansNext: kind == KindFallback && spansNext,
	}
	out.SetArea(ctx, area, cell)
	return true
}

// SetArea moves l, laid out by [LayoutGlyph] for cell, into area, without
// looking up its face or classifying it again.
func (l *Layout) SetArea(ctx GlyphContext, area image.Rectangle, cell *screen.Cell) {
	l.Area = area
	if l.Kind == KindSprite {
		return
	}
	metrics := ctx.Metrics()
	l.Dot = fixed.Point26_6{
		X: GridGlyphDotX(area, metrics),
		Y: fixed.I(area.Max.Y - metrics.FontBaseline.Int()),
	}
	switch l.Kind {
	case KindGrid, KindSprite:
	case KindPowerline:
		l.Target = area
	case KindFallback:
		if l.SpansNext {
			l.Target = idraw.FallbackGlyphSpanTarget(ctx, area)
		} else {
			l.Target = idraw.FallbackGlyphTarget(ctx, area, cell)
		}
	}
}

// IsSpanIcon reports whether content is a Nerd Font icon that Ghostty lets
// grow into the cell after it, when there is room: see [IconRoom].
func IsSpanIcon(content string) bool {
	if content == "" || content[0] < utf8.RuneSelf {
		return false
	}
	r, ok := idraw.FirstRune(content)
	return ok && idraw.IsNerdIconRune(r)
}

// IconRoom reports whether a one-cell icon has room to grow into the cell
// after it, given the content of the cells on each side. It follows Ghostty:
// the next cell must be empty, a space, or an en space, and the icon stays in
// one cell after another symbol, so that runs of icons stay aligned. Callers
// keep an icon at the end of a row in one cell themselves.
func IconRoom(prev, next string) bool {
	if pr, ok := idraw.FirstRune(prev); ok && idraw.IsSymbolRune(pr) && !idraw.IsGraphicsElement(pr) {
		return false
	}
	return next == "" || next == " " || next == "\u2002"
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
