// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package metrics_test

import (
	"testing"

	"github.com/lrstanley/x/term/still/fonts"
	"github.com/lrstanley/x/term/still/internal/config"
	imetrics "github.com/lrstanley/x/term/still/internal/metrics"
	"github.com/lrstanley/x/term/still/units"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

func mustFace(t *testing.T) font.Face {
	t.Helper()
	tf, err := fonts.LoadFace("JetBrains Mono", fonts.StyleRegular)
	if err != nil {
		t.Fatalf("load font: %v", err)
	}
	face, err := fonts.NewFace(tf, &opentype.FaceOptions{
		Size: config.BaseFontSizePx,
		DPI:  72,
	})
	if err != nil {
		t.Fatalf("new face: %v", err)
	}
	return face
}

func TestDerive(t *testing.T) {
	t.Parallel()

	opts := config.DefaultOptions()
	fm := imetrics.MeasureFace(mustFace(t))
	m := imetrics.Derive(opts, fm)

	if m.CellWidth.Int() < 1 || m.CellHeight.Int() < 1 {
		t.Fatalf("cell size = %dx%d, want positive", m.CellWidth, m.CellHeight)
	}
	if m.UnderlineThickness.Int() < 1 {
		t.Fatalf("UnderlineThickness = %d, want >= 1", m.UnderlineThickness)
	}
	if m.IconHeightSingle.Int() < 1 {
		t.Fatalf("IconHeightSingle = %d, want >= 1", m.IconHeightSingle)
	}
}

func TestScaleMetric(t *testing.T) {
	t.Parallel()

	if got := imetrics.ScaleMetric(units.Px(10), 0.5).Int(); got != 5 {
		t.Fatalf("ScaleMetric(10, 0.5) = %d, want 5", got)
	}
	if got := imetrics.ScaleMetric(units.Px(1), 0.1).Int(); got != 1 {
		t.Fatalf("ScaleMetric minimum = %d, want 1", got)
	}
}

func TestDeriveAppliesAdjustments(t *testing.T) {
	t.Parallel()

	opts := config.DefaultOptions()
	opts.CellWidth = units.NewAdjustment(0.2)
	opts.UnderlineThickness = units.Px(3)

	fm := imetrics.MeasureFace(mustFace(t))
	base := imetrics.Derive(config.DefaultOptions(), fm)
	adjusted := imetrics.Derive(opts, fm)

	if adjusted.CellWidth.Int() <= base.CellWidth.Int() {
		t.Fatalf("CellWidth adjustment did not increase: %d vs %d", adjusted.CellWidth, base.CellWidth)
	}
	if adjusted.UnderlineThickness.Int() != 3 {
		t.Fatalf("UnderlineThickness = %d, want 3", adjusted.UnderlineThickness)
	}
}
