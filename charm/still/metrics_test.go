// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"math"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/charm/still/fonts"
	"github.com/lrstanley/x/charm/still/internal/fonttest"
)

func TestDefaultMetricsUseBundledFonts(t *testing.T) {
	t.Parallel()

	d := MustNew()
	m := d.Metrics()

	if m.DPI != DefaultDPI {
		t.Fatalf("DPI = %v, want %v", m.DPI, DefaultDPI)
	}
	if m.FontSize != DefaultFontSize {
		t.Fatalf("FontSize = %v, want %v", m.FontSize, DefaultFontSize)
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
	if d.fonts.gridMetrics.cellWidth <= 0 {
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

func TestIconHeightScalesMetricsWithoutChangingGrid(t *testing.T) {
	t.Parallel()

	base := MustNew().Metrics()
	scaled := MustNew(WithIconHeight(0.5)).Metrics()
	single := MustNew(WithIconHeight(0.5), WithIconHeightSingle(0.25)).Metrics()

	if scaled.CellWidth != base.CellWidth || scaled.CellHeight != base.CellHeight {
		t.Fatalf("WithIconHeight changed cell size: %dx%d -> %dx%d", base.CellWidth, base.CellHeight, scaled.CellWidth, scaled.CellHeight)
	}
	if got, want := scaled.IconHeight.Int(), scaleMetric(base.IconHeight, 0.5).Int(); got != want {
		t.Fatalf("IconHeight = %d, want %d", got, want)
	}
	if got, want := scaled.IconHeightSingle.Int(), scaleMetric(base.IconHeightSingle, 0.5).Int(); got != want {
		t.Fatalf("IconHeightSingle = %d, want %d", got, want)
	}
	if got, want := single.IconHeightSingle.Int(), scaleMetric(base.IconHeightSingle, 0.25).Int(); got != want {
		t.Fatalf("IconHeightSingle override = %d, want %d", got, want)
	}
}

func TestIconHeightRejectsNegativeScale(t *testing.T) {
	t.Parallel()

	assertPanic(t, "WithIconHeight negative scale", func() {
		_ = MustNew(WithIconHeight(-0.5))
	})
	assertPanic(t, "WithIconHeightSingle negative scale", func() {
		_ = MustNew(WithIconHeightSingle(-0.5))
	})
}

func TestCodepointMapDefaultsNilEmptyAndOverride(t *testing.T) {
	t.Parallel()

	powerline := &uv.Cell{Content: "\ue0b0"}
	defaultFamily := mustDefaultFamily(t)
	symbolFamily := mustDefaultSymbolFamily(t)

	disabled := MustNew(WithCodepointMap(nil))
	if got := disabled.fonts.faceForCell(powerline); got != disabled.fonts.regular {
		t.Fatalf("nil codepoint map face = %p, want regular %p", got, disabled.fonts.regular)
	}

	defaulted := MustNew(WithCodepointMap(map[string]fonts.FontFamily{}))
	defaultedRegular := defaulted.fonts.faceForCell(powerline)
	if defaultedRegular == defaulted.fonts.regular {
		t.Fatalf("empty codepoint map resolved to primary regular face, want default symbol face")
	}
	for name, style := range map[string]uv.Style{
		"bold":        {Attrs: uv.AttrBold},
		"italic":      {Attrs: uv.AttrItalic},
		"bold italic": {Attrs: uv.AttrBold | uv.AttrItalic},
	} {
		got := defaulted.fonts.faceForCell(&uv.Cell{Content: "\ue0b0", Style: style})
		if got == defaultedRegular {
			t.Fatalf("default symbol %s face = regular symbol face %p, want synthetic", name, defaultedRegular)
		}
		fonttest.AssertSyntheticFaceDiff(t, defaultedRegular, got, '\ue0b0')
	}

	overridden := MustNew(WithCodepointMap(map[string]fonts.FontFamily{
		"U+E000-U+E0FF": defaultFamily,
	}))
	if got := overridden.fonts.faceForCell(powerline); got != overridden.fonts.regular {
		t.Fatalf("user override face = %p, want regular %p", got, overridden.fonts.regular)
	}
	if got := overridden.fonts.faceForCell(&uv.Cell{Content: "\ue0b0", Style: uv.Style{Attrs: uv.AttrBold}}); got != overridden.fonts.bold {
		t.Fatalf("user override bold face = %p, want primary bold %p", got, overridden.fonts.bold)
	}

	regularOnly := MustNew(WithCodepointMap(map[string]fonts.FontFamily{
		"U+E000-U+E0FF": {Regular: symbolFamily.Regular},
	}))
	regularOnlyFace := regularOnly.fonts.faceForCell(powerline)
	regularOnlyItalic := regularOnly.fonts.faceForCell(&uv.Cell{Content: "\ue0b0", Style: uv.Style{Attrs: uv.AttrItalic}})
	if regularOnlyItalic == regularOnlyFace {
		t.Fatalf("regular-only codepoint italic face = regular face %p, want synthetic", regularOnlyFace)
	}
	fonttest.AssertSyntheticFaceDiff(t, regularOnlyFace, regularOnlyItalic, '\ue0b0')

	syntheticDisabled := MustNew(WithCodepointMap(map[string]fonts.FontFamily{
		"U+E000-U+E0FF": {Regular: symbolFamily.Regular, DisableSynthetic: true},
	}))
	disabledFace := syntheticDisabled.fonts.faceForCell(powerline)
	if got := syntheticDisabled.fonts.faceForCell(&uv.Cell{Content: "\ue0b0", Style: uv.Style{Attrs: uv.AttrItalic}}); got != disabledFace {
		t.Fatalf("synthetic-disabled codepoint italic face = %p, want regular face %p", got, disabledFace)
	}
}

func TestDefaultCodepointMapKeepsBoxDrawingOnRegularFace(t *testing.T) {
	t.Parallel()

	d := MustNew()
	for _, glyph := range []string{"┌", "─", "┐", "│", "└", "┘", "█"} {
		if got := d.fonts.faceForCell(&uv.Cell{Content: glyph}); got != d.fonts.regular {
			t.Fatalf("glyph %q face = %p, want regular %p", glyph, got, d.fonts.regular)
		}
		for _, r := range glyph {
			if _, _, ok := d.fonts.regular.GlyphBounds(r); !ok {
				t.Fatalf("regular face has no bounds for glyph %q", glyph)
			}
			if _, ok := d.fonts.regular.GlyphAdvance(r); !ok {
				t.Fatalf("regular face has no advance for glyph %q", glyph)
			}
		}
	}
}

func TestCodepointMapOverlapAndMalformedError(t *testing.T) {
	t.Parallel()

	defaultFamily := mustDefaultFamily(t)
	symbolFamily := mustDefaultSymbolFamily(t)

	tests := map[string]Option{
		"overlap": WithCodepointMap(map[string]fonts.FontFamily{
			"U+E000-U+E010": defaultFamily,
			"U+E010-U+E020": symbolFamily,
		}),
		"malformed": WithCodepointMap(map[string]fonts.FontFamily{
			"E000": defaultFamily,
		}),
		"nil regular": WithCodepointMap(map[string]fonts.FontFamily{
			"U+E000": {},
		}),
	}

	for name, opt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := New(opt); err == nil {
				t.Fatalf("%s: New() error = nil, want error", name)
			}
		})
	}
}

func TestStyleFaceSelectionDefaultsFallbackAndError(t *testing.T) {
	t.Parallel()

	defaults := MustNew()
	if got := defaults.fonts.faceForCell(&uv.Cell{Style: uv.Style{Attrs: uv.AttrBold}}); got == defaults.fonts.regular {
		t.Fatalf("default bold face resolved to regular")
	}
	if got := defaults.fonts.faceForCell(&uv.Cell{Style: uv.Style{Attrs: uv.AttrItalic}}); got == defaults.fonts.regular {
		t.Fatalf("default italic face resolved to regular")
	}
	if got := defaults.fonts.faceForCell(&uv.Cell{Style: uv.Style{Attrs: uv.AttrBold | uv.AttrItalic}}); got == defaults.fonts.regular {
		t.Fatalf("default bold italic face resolved to regular")
	}

	defaultFamily := mustDefaultFamily(t)
	customRegularOnly := MustNew(WithFontFamily(fonts.FontFamily{Regular: defaultFamily.Regular}))
	if customRegularOnly.fonts.bold == customRegularOnly.fonts.regular {
		t.Fatal("custom regular without bold variant did not synthesize bold")
	}
	fonttest.AssertSyntheticFaceDiff(t, customRegularOnly.fonts.regular, customRegularOnly.fonts.bold, 'H')
	if customRegularOnly.fonts.italic == customRegularOnly.fonts.regular {
		t.Fatal("custom regular without italic variant did not synthesize italic")
	}
	fonttest.AssertSyntheticFaceDiff(t, customRegularOnly.fonts.regular, customRegularOnly.fonts.italic, 'H')
	if customRegularOnly.fonts.boldItalic == customRegularOnly.fonts.regular {
		t.Fatal("custom regular without bold italic variant did not synthesize bold italic")
	}
	fonttest.AssertSyntheticFaceDiff(t, customRegularOnly.fonts.regular, customRegularOnly.fonts.boldItalic, 'H')

	syntheticDisabled := MustNew(WithFontFamily(fonts.FontFamily{
		Regular:          defaultFamily.Regular,
		DisableSynthetic: true,
	}))
	if syntheticDisabled.fonts.bold != syntheticDisabled.fonts.regular ||
		syntheticDisabled.fonts.italic != syntheticDisabled.fonts.regular ||
		syntheticDisabled.fonts.boldItalic != syntheticDisabled.fonts.regular {
		t.Fatal("DisableSynthetic did not preserve regular fallback for missing variants")
	}

	if _, err := New(WithFontFamily(fonts.FontFamily{})); err == nil {
		t.Fatal("nil primary regular font: New() error = nil, want error")
	}
}

func TestFontSizeRebuildsFontsAndMetrics(t *testing.T) {
	t.Parallel()

	d := MustNew()
	fontVersion := d.fontVersion
	metricsVersion := d.metricsVersion

	if err := d.Apply(WithFontSizePt(12)); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if d.fontVersion == fontVersion {
		t.Fatalf("fontVersion unchanged after font size Apply: %d", fontVersion)
	}
	if d.metricsVersion == metricsVersion {
		t.Fatalf("metricsVersion unchanged after font size Apply: %d", metricsVersion)
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
