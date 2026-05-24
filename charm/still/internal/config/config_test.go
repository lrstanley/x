// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package config_test

import (
	"image/color"
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

func TestPositivePx(t *testing.T) {
	t.Parallel()

	if got := config.PositivePx(5, "test").Int(); got != 5 {
		t.Fatalf("PositivePx(5) = %d, want 5", got)
	}
	assertPanic(t, "negative", func() {
		_ = config.PositivePx(-1, "test")
	})
}

func TestRequiredPx(t *testing.T) {
	t.Parallel()

	if got := config.RequiredPx(3, "test").Int(); got != 3 {
		t.Fatalf("RequiredPx(3) = %d, want 3", got)
	}
	assertPanic(t, "zero", func() {
		_ = config.RequiredPx(0, "test")
	})
	assertPanic(t, "negative", func() {
		_ = config.RequiredPx(-1, "test")
	})
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
	if got := cloned.Indexed[1].(color.NRGBA).R; got != 2 {
		t.Fatalf("cloned color R = %d, want 2", got)
	}
}

func TestNormalizeIconHeightScale(t *testing.T) {
	t.Parallel()

	if got := config.NormalizeIconHeightScale(1.5); got != 1.5 {
		t.Fatalf("NormalizeIconHeightScale(1.5) = %v, want 1.5", got)
	}
	assertPanic(t, "negative scale", func() {
		_ = config.NormalizeIconHeightScale(-0.1)
	})
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

func assertPanic(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatalf("%s did not panic", name)
		}
	}()
	fn()
}
