// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fontset_test

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/charm/still/fonts"
	"github.com/lrstanley/x/charm/still/internal/config"
	"github.com/lrstanley/x/charm/still/internal/fontset"
	"github.com/lrstanley/x/charm/still/internal/testutil"
)

func TestBuildDefault(t *testing.T) {
	t.Parallel()

	fs, err := fontset.Build(config.DefaultOptions())
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	t.Cleanup(func() { _ = fs.Close() })

	if fs.Regular() == nil {
		t.Fatal("Regular() is nil")
	}
	if fs.Bold() == nil {
		t.Fatal("Bold() is nil")
	}
	if !fs.UsesGridLayout(fs.Regular()) {
		t.Fatal("regular face should use grid layout")
	}
	if fs.GridMetrics().CellWidth.Float64() <= 0 {
		t.Fatal("GridMetrics().CellWidth should be positive")
	}
}

func TestNilSetSafe(t *testing.T) {
	t.Parallel()

	var fs *fontset.Set
	if fs.Regular() != nil || fs.Bold() != nil || fs.Italic() != nil || fs.BoldItalic() != nil {
		t.Fatal("nil set accessors should return nil faces")
	}
	if fs.FaceForCell(&uv.Cell{Content: "A"}) != nil {
		t.Fatal("nil set FaceForCell should return nil")
	}
	if fs.UsesGridLayout(nil) {
		t.Fatal("nil set UsesGridLayout should be false")
	}
	if fs.Close() != nil {
		t.Fatal("nil set Close should return nil")
	}
}

func TestFaceForCellCodepointRouting(t *testing.T) {
	t.Parallel()

	defaultFamily := mustDefaultFamily(t)

	fs, err := fontset.Build(config.Options{
		CodepointMapSet: true,
		CodepointMap: map[string]fonts.FontFamily{
			"U+E000-U+E0FF": defaultFamily,
		},
		FontSize: config.DefaultFontSize,
		DPI:      config.DefaultDPI,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	t.Cleanup(func() { _ = fs.Close() })

	powerline := &uv.Cell{Content: "\ue0b0"}
	if got := fs.FaceForCell(powerline); got != fs.Regular() {
		t.Fatalf("user override face = %p, want regular %p", got, fs.Regular())
	}

	defaulted, err := fontset.Build(config.Options{
		CodepointMapSet: true,
		CodepointMap:    map[string]fonts.FontFamily{},
		FontSize:        config.DefaultFontSize,
		DPI:             config.DefaultDPI,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	t.Cleanup(func() { _ = defaulted.Close() })

	defaultSymbol := defaulted.FaceForCell(powerline)
	if defaultSymbol == defaulted.Regular() {
		t.Fatal("empty codepoint map should route powerline to symbol face")
	}
	testutil.AssertSyntheticFaceDiff(t, defaultSymbol, defaulted.FaceForCell(&uv.Cell{
		Content: "\ue0b0",
		Style:   uv.Style{Attrs: uv.AttrBold},
	}), '\ue0b0')
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
