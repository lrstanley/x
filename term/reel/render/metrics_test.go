// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"image"
	"math"
	"testing"

	"github.com/lrstanley/x/term/reel/render/fonts"
	imetrics "github.com/lrstanley/x/term/reel/render/internal/metrics"
	"github.com/lrstanley/x/term/reel/render/internal/testutil"
	"github.com/lrstanley/x/term/reel/vt/screen"
)

func TestWithScale(t *testing.T) {
	t.Parallel()

	// The default scale is 11pt at 96 DPI, which gives 9x20 cells.
	base := MustNew()
	if got, want := base.Layout(1, 1).Cell, (image.Point{X: 9, Y: 20}); got != want {
		t.Fatalf("default cell size = %v, want %v", got, want)
	}

	for _, scale := range []float64{0.5, 1.5, 2, 3} {
		d := MustNew(WithScale(scale))
		if got := d.Metrics().Scale; got != scale {
			t.Fatalf("Metrics().Scale = %v, want %v", got, scale)
		}
		// Cells snap to whole pixels, so allow a pixel of rounding.
		got, want := d.Layout(1, 1).Cell, base.Layout(1, 1).Cell
		if math.Abs(float64(got.X)-float64(want.X)*scale) > 1 || math.Abs(float64(got.Y)-float64(want.Y)*scale) > 1 {
			t.Fatalf("scale %v: cell size = %v, want ~%v x %v", scale, got, scale*float64(want.X), scale*float64(want.Y))
		}
	}

	// Fixed pixel lengths do not scale, only the grid does.
	extra := func(d *Renderer) image.Point {
		l := d.Layout(10, 2)
		return l.Image.Size().Sub(l.Grid.Size())
	}
	if a, b := extra(MustNew(WithPadding(8))), extra(MustNew(WithPadding(8), WithScale(2))); a != b {
		t.Fatalf("padding changed with scale: %v vs %v", a, b)
	}

	for _, scale := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		if _, err := New(WithScale(scale)); err == nil {
			t.Fatalf("New(WithScale(%v)) error = nil, want error", scale)
		}
	}
}

func TestDefaultMetricsUseBundledFonts(t *testing.T) {
	t.Parallel()

	d := MustNew()
	m := d.Metrics()

	if m.Scale != 1 {
		t.Fatalf("Scale = %v, want 1", m.Scale)
	}
	if m.CellWidth < 1 || m.CellHeight < 1 {
		t.Fatalf("cell size = %dx%d, want positive", m.CellWidth, m.CellHeight)
	}
	if math.Abs(float64(m.CellWidth)-m.FaceWidth.Float64()) > 0.5 {
		t.Fatalf("CellWidth = %d, FaceWidth = %.3f, want rounded width", m.CellWidth, m.FaceWidth)
	}
	if math.Abs(float64(m.CellHeight)-m.FaceHeight.Float64()) > 0.5 {
		t.Fatalf("CellHeight = %d, FaceHeight = %.3f, want rounded height", m.CellHeight, m.FaceHeight)
	}
	if m.UnderlineThickness < 1 || m.StrikethroughThickness < 1 || m.BoxThickness < 1 {
		t.Fatalf("thicknesses = underline:%d strike:%d box:%d, want >= 1", m.UnderlineThickness, m.StrikethroughThickness, m.BoxThickness)
	}
	if d.fonts.GridMetrics().CellWidth <= 0 {
		t.Fatalf("grid metrics were not measured from primary regular face")
	}
}

func TestCellHeightAdjustmentShiftsVerticalMetrics(t *testing.T) {
	t.Parallel()

	base := MustNew().Metrics()
	adjusted := MustNew(WithCellHeight(0.2)).Metrics()

	diff := adjusted.CellHeight.Int() - base.CellHeight.Int()
	if diff <= 0 {
		t.Fatalf("adjusted CellHeight = %d, want greater than %d", adjusted.CellHeight, base.CellHeight)
	}

	top, bottom := expectedCellHeightShift(base, diff)
	if got, want := adjusted.FontBaseline.Int(), base.FontBaseline.Int()+bottom; got != want {
		t.Fatalf("FontBaseline = %d, want %d", got, want)
	}
	if got, want := adjusted.UnderlinePosition.Int(), base.UnderlinePosition.Int()+top; got != want {
		t.Fatalf("UnderlinePosition = %d, want %d", got, want)
	}
	if got, want := adjusted.StrikethroughPosition.Int(), base.StrikethroughPosition.Int()+top; got != want {
		t.Fatalf("StrikethroughPosition = %d, want %d", got, want)
	}
}

func TestIconHeight(t *testing.T) {
	t.Parallel()

	t.Run("scales metrics without changing grid", func(t *testing.T) {
		t.Parallel()

		base := MustNew().Metrics()
		scaled := MustNew(WithIconHeight(0.5)).Metrics()
		single := MustNew(WithIconHeight(0.5), WithIconHeightSingle(0.25)).Metrics()

		if scaled.CellWidth != base.CellWidth || scaled.CellHeight != base.CellHeight {
			t.Fatalf("WithIconHeight changed cell size: %dx%d -> %dx%d", base.CellWidth, base.CellHeight, scaled.CellWidth, scaled.CellHeight)
		}
		if got, want := scaled.IconHeight.Int(), imetrics.ScaleMetric(base.IconHeight, 0.5).Int(); got != want {
			t.Fatalf("IconHeight = %d, want %d", got, want)
		}
		if got, want := scaled.IconHeightSingle.Int(), imetrics.ScaleMetric(base.IconHeightSingle, 0.5).Int(); got != want {
			t.Fatalf("IconHeightSingle = %d, want %d", got, want)
		}
		if got, want := single.IconHeightSingle.Int(), imetrics.ScaleMetric(base.IconHeightSingle, 0.25).Int(); got != want {
			t.Fatalf("IconHeightSingle override = %d, want %d", got, want)
		}
	})

	t.Run("rejects negative scale", func(t *testing.T) {
		t.Parallel()

		assertPanic(t, "WithIconHeight negative scale", func() {
			_ = MustNew(WithIconHeight(-0.5))
		})
		assertPanic(t, "WithIconHeightSingle negative scale", func() {
			_ = MustNew(WithIconHeightSingle(-0.5))
		})
	})
}

func TestCodepointMapDefaultsNilEmptyAndOverride(t *testing.T) {
	t.Parallel()

	powerline := &screen.Cell{Content: "\ue0b0"}
	defaultFamily := mustDefaultFamily(t)
	symbolFamily := mustDefaultSymbolFamily(t)

	disabled := MustNew(WithCodepointMap(nil))
	if got := faceForCell(disabled.fonts, powerline); got != disabled.fonts.Regular() {
		t.Fatalf("nil codepoint map face = %p, want regular %p", got, disabled.fonts.Regular())
	}

	defaulted := MustNew(WithCodepointMap(map[string]fonts.FontFamily{}))
	defaultedRegular := faceForCell(defaulted.fonts, powerline)
	if defaultedRegular == defaulted.fonts.Regular() {
		t.Fatalf("empty codepoint map resolved to primary regular face, want default symbol face")
	}
	for name, style := range map[string]screen.Style{
		"bold":        {Attrs: screen.AttrBold},
		"italic":      {Attrs: screen.AttrItalic},
		"bold italic": {Attrs: screen.AttrBold | screen.AttrItalic},
	} {
		got := faceForCell(defaulted.fonts, &screen.Cell{Content: "\ue0b0", Style: style})
		if got == defaultedRegular {
			t.Fatalf("default symbol %s face = regular symbol face %p, want synthetic", name, defaultedRegular)
		}
		testutil.AssertSyntheticFaceDiff(t, defaultedRegular, got, '\ue0b0')
	}

	overridden := MustNew(WithCodepointMap(map[string]fonts.FontFamily{
		"U+E000-U+E0FF": defaultFamily,
	}))
	if got := faceForCell(overridden.fonts, powerline); got != overridden.fonts.Regular() {
		t.Fatalf("user override face = %p, want regular %p", got, overridden.fonts.Regular())
	}
	if got := faceForCell(overridden.fonts, &screen.Cell{Content: "\ue0b0", Style: screen.Style{Attrs: screen.AttrBold}}); got != overridden.fonts.Bold() {
		t.Fatalf("user override bold face = %p, want primary bold %p", got, overridden.fonts.Bold())
	}

	regularOnly := MustNew(WithCodepointMap(map[string]fonts.FontFamily{
		"U+E000-U+E0FF": {Regular: symbolFamily.Regular},
	}))
	regularOnlyFace := faceForCell(regularOnly.fonts, powerline)
	regularOnlyItalic := faceForCell(regularOnly.fonts, &screen.Cell{Content: "\ue0b0", Style: screen.Style{Attrs: screen.AttrItalic}})
	if regularOnlyItalic == regularOnlyFace {
		t.Fatalf("regular-only codepoint italic face = regular face %p, want synthetic", regularOnlyFace)
	}
	testutil.AssertSyntheticFaceDiff(t, regularOnlyFace, regularOnlyItalic, '\ue0b0')

	syntheticDisabled := MustNew(WithCodepointMap(map[string]fonts.FontFamily{
		"U+E000-U+E0FF": {Regular: symbolFamily.Regular, DisableSynthetic: true},
	}))
	disabledFace := faceForCell(syntheticDisabled.fonts, powerline)
	if got := faceForCell(syntheticDisabled.fonts, &screen.Cell{Content: "\ue0b0", Style: screen.Style{Attrs: screen.AttrItalic}}); got != disabledFace {
		t.Fatalf("synthetic-disabled codepoint italic face = %p, want regular face %p", got, disabledFace)
	}
}

func TestDefaultCodepointMapKeepsBoxDrawingOnRegularFace(t *testing.T) {
	t.Parallel()

	d := MustNew()
	for _, glyph := range []string{"┌", "─", "┐", "│", "└", "┘", "█"} {
		if got := faceForCell(d.fonts, &screen.Cell{Content: glyph}); got != d.fonts.Regular() {
			t.Fatalf("glyph %q face = %p, want regular %p", glyph, got, d.fonts.Regular())
		}
		for _, r := range glyph {
			if _, _, ok := d.fonts.Regular().GlyphBounds(r); !ok {
				t.Fatalf("regular face has no bounds for glyph %q", glyph)
			}
			if _, ok := d.fonts.Regular().GlyphAdvance(r); !ok {
				t.Fatalf("regular face has no advance for glyph %q", glyph)
			}
		}
	}
}

func TestStyleFaceSelectionDefaultsFallbackAndError(t *testing.T) {
	t.Parallel()

	defaults := MustNew()
	if got := faceForCell(defaults.fonts, &screen.Cell{Style: screen.Style{Attrs: screen.AttrBold}}); got == defaults.fonts.Regular() {
		t.Fatalf("default bold face resolved to regular")
	}
	if got := faceForCell(defaults.fonts, &screen.Cell{Style: screen.Style{Attrs: screen.AttrItalic}}); got == defaults.fonts.Regular() {
		t.Fatalf("default italic face resolved to regular")
	}
	if got := faceForCell(defaults.fonts, &screen.Cell{Style: screen.Style{Attrs: screen.AttrBold | screen.AttrItalic}}); got == defaults.fonts.Regular() {
		t.Fatalf("default bold italic face resolved to regular")
	}

	defaultFamily := mustDefaultFamily(t)
	customRegularOnly := MustNew(WithFontFamily(fonts.FontFamily{Regular: defaultFamily.Regular}))
	if customRegularOnly.fonts.Bold() == customRegularOnly.fonts.Regular() {
		t.Fatal("custom regular without bold variant did not synthesize bold")
	}
	testutil.AssertSyntheticFaceDiff(t, customRegularOnly.fonts.Regular(), customRegularOnly.fonts.Bold(), 'H')
	if customRegularOnly.fonts.Italic() == customRegularOnly.fonts.Regular() {
		t.Fatal("custom regular without italic variant did not synthesize italic")
	}
	testutil.AssertSyntheticFaceDiff(t, customRegularOnly.fonts.Regular(), customRegularOnly.fonts.Italic(), 'H')
	if customRegularOnly.fonts.BoldItalic() == customRegularOnly.fonts.Regular() {
		t.Fatal("custom regular without bold italic variant did not synthesize bold italic")
	}
	testutil.AssertSyntheticFaceDiff(t, customRegularOnly.fonts.Regular(), customRegularOnly.fonts.BoldItalic(), 'H')

	syntheticDisabled := MustNew(WithFontFamily(fonts.FontFamily{
		Regular:          defaultFamily.Regular,
		DisableSynthetic: true,
	}))
	if syntheticDisabled.fonts.Bold() != syntheticDisabled.fonts.Regular() ||
		syntheticDisabled.fonts.Italic() != syntheticDisabled.fonts.Regular() ||
		syntheticDisabled.fonts.BoldItalic() != syntheticDisabled.fonts.Regular() {
		t.Fatal("DisableSynthetic did not preserve regular fallback for missing variants")
	}

	if _, err := New(WithFontFamily(fonts.FontFamily{})); err == nil {
		t.Fatal("nil primary regular font: New() error = nil, want error")
	}
}

func expectedCellHeightShift(m Metrics, diff int) (top, bottom int) {
	half := float64(diff) / 2
	positionWithRespectToCenter := m.FaceY.Float64() - (float64(m.CellHeight.Int())-m.FaceHeight.Float64())/2
	if positionWithRespectToCenter > 0 {
		return int(math.Ceil(half)), int(math.Floor(half))
	}
	return int(math.Floor(half)), int(math.Ceil(half))
}

func mustDefaultFamily(t *testing.T) fonts.FontFamily {
	t.Helper()
	family, err := fonts.DefaultFamily()
	if err != nil {
		t.Fatalf("load default font family: %v", err)
	}
	return family
}

func mustDefaultSymbolFamily(t *testing.T) fonts.FontFamily {
	t.Helper()
	family, err := fonts.DefaultSymbolFamily()
	if err != nil {
		t.Fatalf("load default symbol font family: %v", err)
	}
	return family
}
