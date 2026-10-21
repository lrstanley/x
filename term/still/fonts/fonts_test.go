// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fonts

import (
	"io"
	"path/filepath"
	"sync"
	"testing"

	"golang.org/x/image/font/opentype"
)

const embeddedFixture = "JetBrains Mono"

func TestDirs_absolute(t *testing.T) {
	for _, d := range Dirs() {
		if !filepath.IsAbs(d) {
			t.Errorf("dir not absolute: %q", d)
		}
	}
}

func TestLoadFace_cachesParsedFonts(t *testing.T) {
	resetFontCachesForTest()
	t.Cleanup(resetFontCachesForTest)

	a, err := LoadFace(embeddedFixture, StyleRegular)
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadFace("jetbrains  mono", StyleRegular)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("expected LoadFace to return cached parsed font")
	}
}

func TestLoadFace_concurrent(t *testing.T) {
	t.Parallel()

	var wg sync.WaitGroup
	for _, style := range []Style{StyleRegular, StyleBold, StyleItalic, StyleBoldItalic} {
		for range 16 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range 24 {
					if _, err := LoadFace(embeddedFixture, style); err != nil {
						t.Error(err)
						return
					}
				}
			}()
		}
	}
	wg.Wait()
}

func TestNewFace_differentOptionsDifferentFaces(t *testing.T) {
	tf, err := LoadFace(embeddedFixture, StyleRegular)
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewFace(tf, &opentype.FaceOptions{Size: 10, DPI: 72})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if c, ok := a.(io.Closer); ok {
			_ = c.Close()
		}
	})
	b, err := NewFace(tf, &opentype.FaceOptions{Size: 11, DPI: 72})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if c, ok := b.(io.Closer); ok {
			_ = c.Close()
		}
	})
	if a == b {
		t.Fatal("expected different options to produce distinct faces")
	}
}

func TestDefaultFamilies(t *testing.T) {
	family, err := DefaultFamily()
	if err != nil {
		t.Fatal(err)
	}
	if family.Regular == nil || family.Bold == nil || family.Italic == nil || family.BoldItalic == nil {
		t.Fatalf("default family missing variants: %#v", family)
	}
	if family.DisableSynthetic {
		t.Fatal("default family should allow synthetic styles")
	}

	symbols, err := DefaultSymbolFamily()
	if err != nil {
		t.Fatal(err)
	}
	if symbols.Regular == nil {
		t.Fatal("default symbol family missing regular font")
	}
	if symbols.Bold != nil || symbols.Italic != nil || symbols.BoldItalic != nil {
		t.Fatalf("default symbol family should only load regular: %#v", symbols)
	}
	if symbols.DisableSynthetic {
		t.Fatal("default symbol family should allow synthetic styles")
	}
}
