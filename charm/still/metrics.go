// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"image"
	"math"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// Metrics describes the terminal grid geometry used for a draw.
type Metrics struct {
	DPI                    DPI
	FontSize               Pt
	CellWidth              Px
	CellHeight             Px
	FontBaseline           Px
	UnderlinePosition      Px
	UnderlineThickness     Px
	StrikethroughPosition  Px
	StrikethroughThickness Px
	CursorThickness        Px
	CursorHeight           Px
	BoxThickness           Px
	IconHeight             Px
	IconHeightSingle       Px
	FaceWidth              FractionalPx
	FaceHeight             FractionalPx
	FaceY                  FractionalPx
}

// CellSize returns the pixel size of one terminal cell.
func (m Metrics) CellSize() image.Point {
	return image.Pt(m.CellWidth.Int(), m.CellHeight.Int())
}

// faceMetrics is the intermediate font geometry extracted before snapping to an
// integer grid. It corresponds to Ghostty's FaceMetrics: monospace ASCII cell
// width, typographic ascent, descent, line gap, measured cap/ex heights, and
// estimated underline/strikethrough thicknesses.
//
// Reference: https://github.com/ghostty-org/ghostty/blob/main/src/font/Metrics.zig (FaceMetrics).
type faceMetrics struct {
	cellWidth              FractionalPx
	ascent                 FractionalPx
	descent                FractionalPx
	lineGap                FractionalPx
	capHeight              FractionalPx
	exHeight               FractionalPx
	underlineThickness     FractionalPx
	strikethroughThickness FractionalPx
}

// deriveMetrics builds [Metrics] by measuring [fontSet.regular], running
// Ghostty-style calcMetrics math, then layering [rendererOptions] adjustments
// analogous to Ghostty's Metrics.apply ModifierSet path.
//
// References:
//   - https://github.com/ghostty-org/ghostty/blob/main/src/font/Metrics.zig - FaceMetrics, calc, apply
//   - https://github.com/ghostty-org/ghostty/blob/main/src/font/face.zig - backend-specific face loading
func deriveMetrics(opts rendererOptions, fs *fontSet) Metrics {
	if fs == nil || fs.regular == nil {
		panic("fonts are not loaded")
	}
	metrics := calcMetrics(opts, fs.gridMetrics)
	applyMetricAdjustments(&metrics, opts)
	return metrics
}

// calcMetrics converts [faceMetrics] into integer cell [Metrics] using the same
// layout rules as Ghostty's Metrics.calc: preserve unrounded face_width and
// face_height (line height), round cell width/height with [math.Round], split
// line_gap evenly above/below the glyph box, center the baseline in the rounded
// cell, derive top-relative underline/strikethrough from top_to_baseline and
// FaceMetrics defaults (underline one thickness below baseline; strikethrough
// centered on ex-height), ceil decoration thicknesses to whole pixels with a
// minimum of 1, and apply the icon_height_single heuristic (2*cap_height + face_height)/3.
//
// FontBaseline is the distance from the bottom of the cell to the text baseline
// (Ghostty's cell_baseline).
//
// Reference: https://github.com/ghostty-org/ghostty/blob/main/src/font/Metrics.zig (pub fn calc).
func calcMetrics(opts rendererOptions, face faceMetrics) Metrics {
	faceWidth := face.cellWidth.Float64()
	faceHeight := face.ascent.Float64() - face.descent.Float64() + face.lineGap.Float64()
	cellWidth := max(1, int(math.Round(faceWidth)))
	cellHeight := max(1, int(math.Round(faceHeight)))

	halfLineGap := face.lineGap.Float64() / 2
	faceBaseline := halfLineGap - face.descent.Float64()
	cellBaseline := math.Round(faceBaseline - (float64(cellHeight)-faceHeight)/2)
	faceY := cellBaseline - faceBaseline
	topToBaseline := float64(cellHeight) - cellBaseline

	underlineThickness := max(1, int(math.Ceil(face.underlineThickness.Float64())))
	strikethroughThickness := max(1, int(math.Ceil(face.strikethroughThickness.Float64())))
	underlinePosition := int(math.Round(topToBaseline + float64(underlineThickness)))
	strikethroughPosition := int(math.Round(topToBaseline - (face.exHeight.Float64()+float64(strikethroughThickness))*0.5))
	iconHeightSingle := int(math.Round((2*face.capHeight.Float64() + faceHeight) / 3))

	return Metrics{
		DPI:                    opts.dpi,
		FontSize:               opts.fontSize,
		CellWidth:              Px(cellWidth),
		CellHeight:             Px(cellHeight),
		FontBaseline:           Px(max(0, int(cellBaseline))),
		UnderlinePosition:      Px(max(0, underlinePosition)),
		UnderlineThickness:     Px(underlineThickness),
		StrikethroughPosition:  Px(max(0, strikethroughPosition)),
		StrikethroughThickness: Px(strikethroughThickness),
		CursorThickness:        Px(1),
		CursorHeight:           Px(cellHeight),
		BoxThickness:           Px(underlineThickness),
		IconHeight:             Px(cellHeight),
		IconHeightSingle:       Px(max(1, iconHeightSingle)),
		FaceWidth:              FractionalPx(faceWidth),
		FaceHeight:             FractionalPx(faceHeight),
		FaceY:                  FractionalPx(faceY),
	}
}

// applyMetricAdjustments applies option overrides and clamps minimum thicknesses/sizes.
// Semantically this mirrors Ghostty's Metrics.apply + Metrics.clamp: per-field
// modifiers (cell dimensions, baseline, underline/strikethrough, cursor,
// box drawing thickness, icon heights) update the snapshot in place; thickness
// floors match Ghostty's Minimums.
//
// Reference: https://github.com/ghostty-org/ghostty/blob/main/src/font/Metrics.zig (pub fn apply, clamp).
func applyMetricAdjustments(m *Metrics, opts rendererOptions) {
	if adjusted := opts.cellWidth.Apply(m.CellWidth.Int()); adjusted != m.CellWidth.Int() {
		m.CellWidth = Px(adjusted)
	}
	if adjusted := opts.cellHeight.Apply(m.CellHeight.Int()); adjusted != m.CellHeight.Int() {
		shiftCellHeightMetrics(m, adjusted)
	}
	if opts.fontBaseline != 0 {
		m.FontBaseline = Px(opts.fontBaseline.Apply(m.FontBaseline.Int()))
	}
	if opts.underlinePosition != 0 {
		m.UnderlinePosition = Px(opts.underlinePosition.Apply(m.UnderlinePosition.Int()))
	}
	if opts.underlineThickness > 0 {
		m.UnderlineThickness = opts.underlineThickness
	}
	if opts.strikethroughPosition != 0 {
		m.StrikethroughPosition = Px(opts.strikethroughPosition.Apply(m.StrikethroughPosition.Int()))
	}
	if opts.strikethroughThickness > 0 {
		m.StrikethroughThickness = opts.strikethroughThickness
	}
	if opts.cursorThickness > 0 {
		m.CursorThickness = opts.cursorThickness
	}
	if opts.cursorHeight != 0 {
		m.CursorHeight = Px(opts.cursorHeight.Apply(m.CursorHeight.Int()))
	}
	if opts.boxThickness > 0 {
		m.BoxThickness = opts.boxThickness
	}
	if opts.iconHeightScale != 1 {
		m.IconHeight = scaleMetric(m.IconHeight, opts.iconHeightScale)
	}
	singleScale := opts.iconHeightScale
	if opts.iconHeightSingleSet {
		singleScale = opts.iconHeightSingleScale
	}
	if singleScale != 1 {
		m.IconHeightSingle = scaleMetric(m.IconHeightSingle, singleScale)
	}

	m.UnderlineThickness = Px(max(1, m.UnderlineThickness.Int()))
	m.StrikethroughThickness = Px(max(1, m.StrikethroughThickness.Int()))
	m.BoxThickness = Px(max(1, m.BoxThickness.Int()))
	m.CursorThickness = Px(max(1, m.CursorThickness.Int()))
	m.CursorHeight = Px(max(1, m.CursorHeight.Int()))
	m.IconHeight = Px(max(1, m.IconHeight.Int()))
	m.IconHeightSingle = Px(max(1, m.IconHeightSingle.Int()))
}

// shiftCellHeightMetrics reapplies vertical metrics after the cell height changes
// -- the same redistribution Ghostty performs when a cell_height modifier differs
// from the prior value: split the pixel delta between top and bottom using
// ceil/floor on half_diff depending on whether the face sits high or low relative
// to perfect vertical centering (via FaceY vs cell_height and face_height), shift
// baseline/FaceY by the bottom portion, and shift top-anchored decoration positions
// by the top portion. That pairing keeps text visually centered when users apply
// percentage adjustments analogous to Ghostty adjust-cell-height tuning.
//
// CursorHeight and icon heights also absorb the full height delta here so
// render-local shapes stay aligned with the stretched cell.
//
// References:
//   - https://github.com/ghostty-org/ghostty/blob/main/src/font/Metrics.zig - Metrics.apply cell_height branch
//   - https://github.com/ghostty-org/ghostty/blob/main/src/font/Metrics.zig - comments on adjust-cell-height in Metrics.calc
func shiftCellHeightMetrics(m *Metrics, adjusted int) {
	original := m.CellHeight.Int()
	diff := float64(adjusted - original)
	halfDiff := diff / 2
	positionWithRespectToCenter := m.FaceY.Float64() - (float64(original)-m.FaceHeight.Float64())/2

	var diffTop, diffBottom float64
	if positionWithRespectToCenter > 0 {
		diffTop, diffBottom = math.Ceil(halfDiff), math.Floor(halfDiff)
	} else {
		diffTop, diffBottom = math.Floor(halfDiff), math.Ceil(halfDiff)
	}

	m.CellHeight = Px(adjusted)
	m.FontBaseline = Px(max(0, m.FontBaseline.Int()+int(diffBottom)))
	m.FaceY = FractionalPx(m.FaceY.Float64() + diffBottom)
	m.UnderlinePosition = Px(max(0, m.UnderlinePosition.Int()+int(diffTop)))
	m.StrikethroughPosition = Px(max(0, m.StrikethroughPosition.Int()+int(diffTop)))
	m.CursorHeight = Px(max(0, m.CursorHeight.Int()+int(diff)))
	m.IconHeight = Px(max(0, m.IconHeight.Int()+int(diff)))
	m.IconHeightSingle = Px(max(0, m.IconHeightSingle.Int()+int(diff)))
}

// scaleMetric multiplies a pixel extent by scale and rounds, enforcing a minimum
// of 1. Used for icon height scaling parallel to Ghostty applying icon_height
// modifiers after base metrics exist.
//
// Reference: https://github.com/ghostty-org/ghostty/blob/main/src/font/Metrics.zig (Metrics.apply icon_height arm).
func scaleMetric(v Px, scale float64) Px {
	return Px(max(1, int(math.Round(float64(v.Int())*scale))))
}

// measureFace constructs faceMetrics from a [font.Face], following Ghostty's
// FaceMetrics conventions: max printable ASCII advance as cell width; ascent,
// descent (stored with +Y up like Zig), line_gap derived from OS/2 height minus
// ascent/descent; cap/ex heights from glyph bounds with 0.75 fallbacks;
// underline/strikethrough thickness defaulting to 0.15 times ex height.
//
// References:
//   - https://github.com/ghostty-org/ghostty/blob/main/src/font/Metrics.zig - FaceMetrics helpers (lineHeight, capHeight, exHeight, underlineThickness)
//   - https://github.com/ghostty-org/ghostty/blob/main/src/font/face.zig - native face metric extraction
func measureFace(face font.Face) faceMetrics {
	m := face.Metrics()
	ascent := fixedToFloat(m.Ascent)
	descent := fixedToFloat(m.Descent)
	lineGap := max(0, fixedToFloat(m.Height)-ascent-descent)
	capHeight := glyphHeight(face, 'H')
	if capHeight <= 0 {
		capHeight = 0.75 * ascent
	}
	exHeight := glyphHeight(face, 'x')
	if exHeight <= 0 {
		exHeight = 0.75 * capHeight
	}
	underlineThickness := 0.15 * exHeight

	return faceMetrics{
		cellWidth:              FractionalPx(maxPrintableASCIIAdvance(face)),
		ascent:                 FractionalPx(ascent),
		descent:                FractionalPx(-descent),
		lineGap:                FractionalPx(lineGap),
		capHeight:              FractionalPx(capHeight),
		exHeight:               FractionalPx(exHeight),
		underlineThickness:     FractionalPx(underlineThickness),
		strikethroughThickness: FractionalPx(underlineThickness),
	}
}

// maxPrintableASCIIAdvance returns the largest horizontal advance among printable
// ASCII glyphs (U+0020-U+007E), matching Ghostty's notion of the minimum cell
// width that can contain ASCII.
//
// Reference: https://github.com/ghostty-org/ghostty/blob/main/src/font/Metrics.zig (FaceMetrics.cell_width).
func maxPrintableASCIIAdvance(face font.Face) float64 {
	var maxAdvance float64
	for r := rune(0x20); r <= 0x7e; r++ {
		advance, ok := face.GlyphAdvance(r)
		if !ok {
			continue
		}
		maxAdvance = max(maxAdvance, fixedToFloat(advance))
	}
	if maxAdvance <= 0 {
		return 1
	}
	return maxAdvance
}

// glyphHeight measures the bounding-box height of a single glyph in pixels, used
// like Ghostty's measured cap_height / ex_height from concrete glyphs ('H', 'x')
// when OS/2 metrics are absent.
//
// Reference: https://github.com/ghostty-org/ghostty/blob/main/src/font/Metrics.zig (FaceMetrics.cap_height, ex_height).
func glyphHeight(face font.Face, r rune) float64 {
	bounds, _, ok := face.GlyphBounds(r)
	if !ok {
		return 0
	}
	return fixedToFloat(bounds.Max.Y - bounds.Min.Y)
}

// fixedToFloat converts fixed-point font distances (26.6) to float64 pixels so
// intermediate Ghostty-style math stays in higher precision before rounding to
// integer cells.
//
// Reference: https://github.com/ghostty-org/ghostty/blob/main/src/font/Metrics.zig (calc uses f64 throughout FaceMetrics).
func fixedToFloat(v fixed.Int26_6) float64 {
	return float64(v) / 64
}
