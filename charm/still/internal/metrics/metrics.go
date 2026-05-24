// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package metrics derives terminal cell geometry from OpenType face measurements
// and renderer [config.Options], producing [types.Metrics] for layout and draw.
package metrics //nolint:revive // cell geometry derivation, distinct from runtime/metrics

import (
	"math"

	"github.com/lrstanley/x/charm/still/internal/config"
	"github.com/lrstanley/x/charm/still/types"
	"github.com/lrstanley/x/charm/still/units"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// FaceMetrics is intermediate font geometry extracted before snapping to an integer grid.
type FaceMetrics struct {
	CellWidth              units.FractionalPx
	Ascent                 units.FractionalPx
	Descent                units.FractionalPx
	LineGap                units.FractionalPx
	CapHeight              units.FractionalPx
	ExHeight               units.FractionalPx
	UnderlineThickness     units.FractionalPx
	StrikethroughThickness units.FractionalPx
}

// Derive builds [types.Metrics] from options and measured grid face metrics.
func Derive(opts config.Options, grid FaceMetrics) types.Metrics {
	m := calcMetrics(opts, grid)
	applyAdjustments(&m, opts)
	return m
}

// MeasureFace constructs faceMetrics from a [font.Face].
func MeasureFace(face font.Face) FaceMetrics {
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

	return FaceMetrics{
		CellWidth:              units.FractionalPx(maxPrintableASCIIAdvance(face)),
		Ascent:                 units.FractionalPx(ascent),
		Descent:                units.FractionalPx(-descent),
		LineGap:                units.FractionalPx(lineGap),
		CapHeight:              units.FractionalPx(capHeight),
		ExHeight:               units.FractionalPx(exHeight),
		UnderlineThickness:     units.FractionalPx(underlineThickness),
		StrikethroughThickness: units.FractionalPx(underlineThickness),
	}
}

func calcMetrics(opts config.Options, face FaceMetrics) types.Metrics {
	faceWidth := face.CellWidth.Float64()
	faceHeight := face.Ascent.Float64() - face.Descent.Float64() + face.LineGap.Float64()
	cellWidth := max(1, int(math.Round(faceWidth)))
	cellHeight := max(1, int(math.Round(faceHeight)))

	halfLineGap := face.LineGap.Float64() / 2
	faceBaseline := halfLineGap - face.Descent.Float64()
	cellBaseline := math.Round(faceBaseline - (float64(cellHeight)-faceHeight)/2)
	faceY := cellBaseline - faceBaseline
	topToBaseline := float64(cellHeight) - cellBaseline

	underlineThickness := max(1, int(math.Ceil(face.UnderlineThickness.Float64())))
	strikethroughThickness := max(1, int(math.Ceil(face.StrikethroughThickness.Float64())))
	underlinePosition := int(math.Round(topToBaseline + float64(underlineThickness)))
	strikethroughPosition := int(math.Round(topToBaseline - (face.ExHeight.Float64()+float64(strikethroughThickness))*0.5))
	iconHeightSingle := int(math.Round((2*face.CapHeight.Float64() + faceHeight) / 3))

	return types.Metrics{
		DPI:                    opts.DPI,
		FontSize:               opts.FontSize,
		CellWidth:              units.Px(cellWidth),
		CellHeight:             units.Px(cellHeight),
		FontBaseline:           units.Px(max(0, int(cellBaseline))),
		UnderlinePosition:      units.Px(max(0, underlinePosition)),
		UnderlineThickness:     units.Px(underlineThickness),
		StrikethroughPosition:  units.Px(max(0, strikethroughPosition)),
		StrikethroughThickness: units.Px(strikethroughThickness),
		CursorThickness:        units.Px(1),
		CursorHeight:           units.Px(cellHeight),
		BoxThickness:           units.Px(underlineThickness),
		IconHeight:             units.Px(cellHeight),
		IconHeightSingle:       units.Px(max(1, iconHeightSingle)),
		FaceWidth:              units.FractionalPx(faceWidth),
		FaceHeight:             units.FractionalPx(faceHeight),
		FaceY:                  units.FractionalPx(faceY),
	}
}

func applyAdjustments(m *types.Metrics, opts config.Options) {
	if adjusted := opts.CellWidth.Apply(m.CellWidth.Int()); adjusted != m.CellWidth.Int() {
		m.CellWidth = units.Px(adjusted)
	}
	if adjusted := opts.CellHeight.Apply(m.CellHeight.Int()); adjusted != m.CellHeight.Int() {
		shiftCellHeight(m, adjusted)
	}
	if opts.FontBaseline != 0 {
		m.FontBaseline = units.Px(opts.FontBaseline.Apply(m.FontBaseline.Int()))
	}
	if opts.UnderlinePosition != 0 {
		m.UnderlinePosition = units.Px(opts.UnderlinePosition.Apply(m.UnderlinePosition.Int()))
	}
	if opts.UnderlineThickness > 0 {
		m.UnderlineThickness = opts.UnderlineThickness
	}
	if opts.StrikethroughPosition != 0 {
		m.StrikethroughPosition = units.Px(opts.StrikethroughPosition.Apply(m.StrikethroughPosition.Int()))
	}
	if opts.StrikethroughThickness > 0 {
		m.StrikethroughThickness = opts.StrikethroughThickness
	}
	if opts.CursorThickness > 0 {
		m.CursorThickness = opts.CursorThickness
	}
	if opts.CursorHeight != 0 {
		m.CursorHeight = units.Px(opts.CursorHeight.Apply(m.CursorHeight.Int()))
	}
	if opts.BoxThickness > 0 {
		m.BoxThickness = opts.BoxThickness
	}
	if opts.IconHeightScale != 1 {
		m.IconHeight = ScaleMetric(m.IconHeight, opts.IconHeightScale)
	}
	singleScale := opts.IconHeightScale
	if opts.IconHeightSingleSet {
		singleScale = opts.IconHeightSingleScale
	}
	if singleScale != 1 {
		m.IconHeightSingle = ScaleMetric(m.IconHeightSingle, singleScale)
	}

	m.UnderlineThickness = units.Px(max(1, m.UnderlineThickness.Int()))
	m.StrikethroughThickness = units.Px(max(1, m.StrikethroughThickness.Int()))
	m.BoxThickness = units.Px(max(1, m.BoxThickness.Int()))
	m.CursorThickness = units.Px(max(1, m.CursorThickness.Int()))
	m.CursorHeight = units.Px(max(1, m.CursorHeight.Int()))
	m.IconHeight = units.Px(max(1, m.IconHeight.Int()))
	m.IconHeightSingle = units.Px(max(1, m.IconHeightSingle.Int()))
}

func shiftCellHeight(m *types.Metrics, adjusted int) {
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

	m.CellHeight = units.Px(adjusted)
	m.FontBaseline = units.Px(max(0, m.FontBaseline.Int()+int(diffBottom)))
	m.FaceY = units.FractionalPx(m.FaceY.Float64() + diffBottom)
	m.UnderlinePosition = units.Px(max(0, m.UnderlinePosition.Int()+int(diffTop)))
	m.StrikethroughPosition = units.Px(max(0, m.StrikethroughPosition.Int()+int(diffTop)))
	m.CursorHeight = units.Px(max(0, m.CursorHeight.Int()+int(diff)))
	m.IconHeight = units.Px(max(0, m.IconHeight.Int()+int(diff)))
	m.IconHeightSingle = units.Px(max(0, m.IconHeightSingle.Int()+int(diff)))
}

// ScaleMetric multiplies a pixel extent by scale and rounds, enforcing a minimum of 1.
func ScaleMetric(v units.Px, scale float64) units.Px {
	return units.Px(max(1, int(math.Round(float64(v.Int())*scale))))
}

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

func glyphHeight(face font.Face, r rune) float64 {
	bounds, _, ok := face.GlyphBounds(r)
	if !ok {
		return 0
	}
	return fixedToFloat(bounds.Max.Y - bounds.Min.Y)
}

func fixedToFloat(v fixed.Int26_6) float64 {
	return float64(v) / 64
}
