// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import (
	"math"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/charm/still/fonts"
	imetrics "github.com/lrstanley/x/charm/still/internal/metrics"
	"github.com/lrstanley/x/charm/still/internal/testutil"
	"github.com/lrstanley/x/charm/still/units"
)

func TestDefaultMetricsUseBundledFonts(t *testing.T) {
	t.Parallel()

	d := MustNew()
	m := d.Metrics()

	if m.DPI != units.DPI(96) {
		t.Fatalf("DPI = %v, want %v", m.DPI, units.DPI(96))
	}
	if m.FontSize != units.Pt(11) {
		t.Fatalf("FontSize = %v, want %v", m.FontSize, units.Pt(11))
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

	powerline := &uv.Cell{Content: "\ue0b0"}
	defaultFamily := mustDefaultFamily(t)
	symbolFamily := mustDefaultSymbolFamily(t)

	disabled := MustNew(WithCodepointMap(nil))
	if got := disabled.fonts.FaceForCell(powerline); got != disabled.fonts.Regular() {
		t.Fatalf("nil codepoint map face = %p, want regular %p", got, disabled.fonts.Regular())
	}

	defaulted := MustNew(WithCodepointMap(map[string]fonts.FontFamily{}))
	defaultedRegular := defaulted.fonts.FaceForCell(powerline)
	if defaultedRegular == defaulted.fonts.Regular() {
		t.Fatalf("empty codepoint map resolved to primary regular face, want default symbol face")
	}
	for name, style := range map[string]uv.Style{
		"bold":        {Attrs: uv.AttrBold},
		"italic":      {Attrs: uv.AttrItalic},
		"bold italic": {Attrs: uv.AttrBold | uv.AttrItalic},
	} {
		got := defaulted.fonts.FaceForCell(&uv.Cell{Content: "\ue0b0", Style: style})
		if got == defaultedRegular {
			t.Fatalf("default symbol %s face = regular symbol face %p, want synthetic", name, defaultedRegular)
		}
		testutil.AssertSyntheticFaceDiff(t, defaultedRegular, got, '\ue0b0')
	}

	overridden := MustNew(WithCodepointMap(map[string]fonts.FontFamily{
		"U+E000-U+E0FF": defaultFamily,
	}))
	if got := overridden.fonts.FaceForCell(powerline); got != overridden.fonts.Regular() {
		t.Fatalf("user override face = %p, want regular %p", got, overridden.fonts.Regular())
	}
	if got := overridden.fonts.FaceForCell(&uv.Cell{Content: "\ue0b0", Style: uv.Style{Attrs: uv.AttrBold}}); got != overridden.fonts.Bold() {
		t.Fatalf("user override bold face = %p, want primary bold %p", got, overridden.fonts.Bold())
	}

	regularOnly := MustNew(WithCodepointMap(map[string]fonts.FontFamily{
		"U+E000-U+E0FF": {Regular: symbolFamily.Regular},
	}))
	regularOnlyFace := regularOnly.fonts.FaceForCell(powerline)
	regularOnlyItalic := regularOnly.fonts.FaceForCell(&uv.Cell{Content: "\ue0b0", Style: uv.Style{Attrs: uv.AttrItalic}})
	if regularOnlyItalic == regularOnlyFace {
		t.Fatalf("regular-only codepoint italic face = regular face %p, want synthetic", regularOnlyFace)
	}
	testutil.AssertSyntheticFaceDiff(t, regularOnlyFace, regularOnlyItalic, '\ue0b0')

	syntheticDisabled := MustNew(WithCodepointMap(map[string]fonts.FontFamily{
		"U+E000-U+E0FF": {Regular: symbolFamily.Regular, DisableSynthetic: true},
	}))
	disabledFace := syntheticDisabled.fonts.FaceForCell(powerline)
	if got := syntheticDisabled.fonts.FaceForCell(&uv.Cell{Content: "\ue0b0", Style: uv.Style{Attrs: uv.AttrItalic}}); got != disabledFace {
		t.Fatalf("synthetic-disabled codepoint italic face = %p, want regular face %p", got, disabledFace)
	}
}

func TestDefaultCodepointMapKeepsBoxDrawingOnRegularFace(t *testing.T) {
	t.Parallel()

	d := MustNew()
	for _, glyph := range []string{"┌", "─", "┐", "│", "└", "┘", "█"} {
		if got := d.fonts.FaceForCell(&uv.Cell{Content: glyph}); got != d.fonts.Regular() {
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
	if got := defaults.fonts.FaceForCell(&uv.Cell{Style: uv.Style{Attrs: uv.AttrBold}}); got == defaults.fonts.Regular() {
		t.Fatalf("default bold face resolved to regular")
	}
	if got := defaults.fonts.FaceForCell(&uv.Cell{Style: uv.Style{Attrs: uv.AttrItalic}}); got == defaults.fonts.Regular() {
		t.Fatalf("default italic face resolved to regular")
	}
	if got := defaults.fonts.FaceForCell(&uv.Cell{Style: uv.Style{Attrs: uv.AttrBold | uv.AttrItalic}}); got == defaults.fonts.Regular() {
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
