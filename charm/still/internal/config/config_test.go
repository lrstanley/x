// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package config_test

import (
	"image/color"
	"strings"
	"testing"

	"github.com/lrstanley/x/charm/still/internal/config"
	"github.com/lrstanley/x/charm/still/units"
)

func TestDefaultOptions(t *testing.T) {
	t.Parallel()

	opts := config.DefaultOptions()
	if opts.DPI != config.DefaultDPI {
		t.Fatalf("DPI = %v, want %v", opts.DPI, config.DefaultDPI)
	}
	if opts.FontSize != config.DefaultFontSize {
		t.Fatalf("FontSize = %v, want %v", opts.FontSize, config.DefaultFontSize)
	}
	if opts.CursorThickness != 1 {
		t.Fatalf("CursorThickness = %v, want 1", opts.CursorThickness)
	}
	if opts.BackgroundOpacity != config.DefaultBgOpacity {
		t.Fatalf("BackgroundOpacity = %v, want %v", opts.BackgroundOpacity, config.DefaultBgOpacity)
	}
	if opts.Now == nil {
		t.Fatal("Now func is nil")
	}
}

func TestParsePositivePx(t *testing.T) {
	t.Parallel()

	got, err := config.ParsePositivePx(5, "test")
	if err != nil {
		t.Fatalf("ParsePositivePx() error = %v", err)
	}
	if got.Int() != 5 {
		t.Fatalf("ParsePositivePx(5) = %d, want 5", got.Int())
	}
	if _, err := config.ParsePositivePx(-1, "test"); err == nil {
		t.Fatal("ParsePositivePx(-1) error = nil, want error")
	}
}

func TestParseRequiredPx(t *testing.T) {
	t.Parallel()

	got, err := config.ParseRequiredPx(3, "test")
	if err != nil {
		t.Fatalf("ParseRequiredPx() error = %v", err)
	}
	if got.Int() != 3 {
		t.Fatalf("ParseRequiredPx(3) = %d, want 3", got.Int())
	}
	if _, err := config.ParseRequiredPx(0, "test"); err == nil {
		t.Fatal("ParseRequiredPx(0) error = nil, want error")
	}
	if _, err := config.ParseRequiredPx(-1, "test"); err == nil {
		t.Fatal("ParseRequiredPx(-1) error = nil, want error")
	}
}

func TestClonePalette(t *testing.T) {
	t.Parallel()

	orig := color.NRGBA{R: 1}
	p := config.ClonePalette(config.Options{}.Palette)
	p.Indexed = map[int]color.Color{1: orig}

	cloned := config.ClonePalette(p)
	cloned.Indexed[1] = color.NRGBA{R: 2}

	if orig.R != 1 {
		t.Fatalf("original color mutated: R=%d", orig.R)
	}
	c, ok := cloned.Indexed[1].(color.NRGBA)
	if !ok {
		t.Fatal("cloned indexed color is not NRGBA")
	}
	if c.R != 2 {
		t.Fatalf("cloned color R = %d, want 2", c.R)
	}
}

func TestParseIconHeightScale(t *testing.T) {
	t.Parallel()

	got, err := config.ParseIconHeightScale(1.5)
	if err != nil {
		t.Fatalf("ParseIconHeightScale() error = %v", err)
	}
	if got != 1.5 {
		t.Fatalf("ParseIconHeightScale(1.5) = %v, want 1.5", got)
	}
	if _, err := config.ParseIconHeightScale(-0.1); err == nil {
		t.Fatal("ParseIconHeightScale(-0.1) error = nil, want error")
	}
}

func TestBoxThicknessOverride(t *testing.T) {
	t.Parallel()

	if (config.Options{}).BoxThicknessOverride() {
		t.Fatal("zero BoxThickness should not override")
	}
	if !(config.Options{BoxThickness: units.Px(2)}).BoxThicknessOverride() {
		t.Fatal("positive BoxThickness should override")
	}
}

func TestParsePositivePxErrorPrefix(t *testing.T) {
	t.Parallel()

	_, err := config.ParsePositivePx(-1, "margin")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.HasPrefix(err.Error(), "still:") {
		t.Fatalf("error = %q, want still: prefix", err)
	}
}
