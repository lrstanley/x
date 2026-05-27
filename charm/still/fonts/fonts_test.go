// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fonts

import (
	"io"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/image/font/opentype"
)

const embeddedFixture = "JetBrainsMono-Regular"

func TestList_includesEmbedded(t *testing.T) {
	list := List()
	if !slices.ContainsFunc(list, func(s string) bool {
		return strings.EqualFold(s, embeddedFixture)
	}) {
		t.Fatalf("expected embedded font %q in list: %v", embeddedFixture, list)
	}
}

func TestDirs_absolute(t *testing.T) {
	for _, d := range Dirs() {
		if !filepath.IsAbs(d) {
			t.Errorf("dir not absolute: %q", d)
		}
	}
}

func TestLoad(t *testing.T) {
	t.Parallel()

	t.Run("embedded case insensitive", func(t *testing.T) {
		tf, err := Load(strings.ToUpper(embeddedFixture) + ".TTF")
		if err != nil {
			t.Fatal(err)
		}
		if tf == nil {
			t.Fatal("nil font")
		}
	})

	t.Run("returns cached parsed font", func(t *testing.T) {
		resetFontCachesForTest()
		t.Cleanup(resetFontCachesForTest)

		a, err := Load(embeddedFixture)
		if err != nil {
			t.Fatal(err)
		}
		b, err := Load(strings.ToLower(embeddedFixture))
		if err != nil {
			t.Fatal(err)
		}
		if a != b {
			t.Fatal("expected Load to return cached parsed font")
		}

		c := MustLoad(strings.ToUpper(embeddedFixture) + ".TTF")
		if a != c {
			t.Fatal("expected MustLoad to return cached parsed font")
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, err := Load("not-a-real-font-name-xyz")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("concurrent same embedded font", func(t *testing.T) {
		t.Parallel()

		const workers = 48
		const iters = 32

		var wg sync.WaitGroup
		wg.Add(workers)
		for range workers {
			go func() {
				defer wg.Done()
				for range iters {
					if _, err := Load(embeddedFixture); err != nil {
						t.Error(err)
						return
					}
				}
			}()
		}
		wg.Wait()
	})

	t.Run("concurrent distinct embedded fonts", func(t *testing.T) {
		t.Parallel()

		stubs := readEmbeddedFonts()
		if len(stubs) < 2 {
			t.Skip("need at least 2 embedded fonts")
		}
		names := []string{stubs[0].stem, stubs[1].stem}
		if len(stubs) > 2 {
			names = append(names, stubs[2].stem)
		}

		var wg sync.WaitGroup
		for _, n := range names {
			for range 16 {
				wg.Add(1)
				go func(name string) {
					defer wg.Done()
					for range 24 {
						if _, err := Load(name); err != nil {
							t.Error(err)
							return
						}
					}
				}(n)
			}
		}
		wg.Wait()
	})
}

func TestNewFace_differentOptionsDifferentFaces(t *testing.T) {
	tf, err := Load(embeddedFixture)
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

func TestLoadFamily_requiresRegularAndLeavesOmittedVariantsNil(t *testing.T) {
	if _, err := LoadFamily(FontFamilyNames{}); err == nil {
		t.Fatal("expected missing regular font error")
	}

	family, err := LoadFamily(FontFamilyNames{
		Regular:          embeddedFixture,
		Italic:           "JetBrainsMono-Italic",
		DisableSynthetic: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if family.Regular == nil {
		t.Fatal("regular font was not loaded")
	}
	if family.Bold != nil {
		t.Fatal("omitted bold variant should remain nil")
	}
	if family.Italic == nil {
		t.Fatal("italic font was not loaded")
	}
	if family.BoldItalic != nil {
		t.Fatal("omitted bold italic variant should remain nil")
	}
	if !family.DisableSynthetic {
		t.Fatal("DisableSynthetic was not preserved")
	}
}

func TestNormalizeFontName(t *testing.T) {
	cases := map[string]string{
		"  Foo  ":                        "foo",
		"Bar.ttf":                        "bar",
		"Bar.TTF":                        "bar",
		"Baz.ttf.gz":                     "baz",
		"Qux.otf":                        "qux",
		"Qux.OTF":                        "qux",
		"Wiz.otf.gz":                     "wiz",
		embeddedFixture:                  strings.ToLower(embeddedFixture),
		strings.ToUpper(embeddedFixture): strings.ToLower(embeddedFixture),
	}
	for in, want := range cases {
		if got := normalizeFontName(in); got != want {
			t.Fatalf("normalizeFontName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGzipFontStem(t *testing.T) {
	stem, ok := gzipFontStem("JetBrainsMono-Regular.ttf.gz")
	if !ok || stem != "JetBrainsMono-Regular" {
		t.Fatalf("got %q %v", stem, ok)
	}
	stem, ok = gzipFontStem("Example.otf.gz")
	if !ok || stem != "Example" {
		t.Fatalf("got %q %v", stem, ok)
	}
	_, ok = gzipFontStem("readme.txt")
	if ok {
		t.Fatal("expected false")
	}
}

func TestFontStem(t *testing.T) {
	stem, ok := fontStem("JetBrainsMono-Regular.ttf")
	if !ok || stem != "JetBrainsMono-Regular" {
		t.Fatalf("got %q %v", stem, ok)
	}
	stem, ok = fontStem("Example.otf")
	if !ok || stem != "Example" {
		t.Fatalf("got %q %v", stem, ok)
	}
	_, ok = fontStem("JetBrainsMono-Regular.ttf.gz")
	if ok {
		t.Fatal("expected false for gzip filename")
	}
}

