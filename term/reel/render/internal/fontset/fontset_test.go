// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fontset_test

import (
	"testing"

	"github.com/lrstanley/x/term/reel/render/fonts"
	"github.com/lrstanley/x/term/reel/render/internal/config"
	idraw "github.com/lrstanley/x/term/reel/render/internal/draw"
	"github.com/lrstanley/x/term/reel/render/internal/fontset"
	"github.com/lrstanley/x/term/reel/vt/screen"
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
	if fs.FaceForCell(&screen.Cell{Content: "A"}, idraw.Cluster{Text: "A", Base: 'A'}) != nil {
		t.Fatal("nil set FaceForCell should return nil")
	}
	if fs.UsesGridLayout(nil) {
		t.Fatal("nil set UsesGridLayout should be false")
	}
	if fs.Close() != nil {
		t.Fatal("nil set Close should return nil")
	}
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
