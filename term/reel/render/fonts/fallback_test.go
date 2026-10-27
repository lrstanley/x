// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fonts

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/image/font/sfnt"
)

// assertCoverage checks cov against the glyphs font maps in lo..hi.
func assertCoverage(t *testing.T, name string, cov coverage, font *sfnt.Font, lo, hi rune) {
	t.Helper()
	var buf sfnt.Buffer
	for r := lo; r <= hi; r++ {
		idx, err := font.GlyphIndex(&buf, r)
		if err != nil {
			t.Fatalf("%s: GlyphIndex(U+%04X): %v", name, r, err)
		}
		if want := idx != 0; cov.has(r) != want {
			t.Fatalf("%s: coverage of U+%04X = %v, want %v", name, r, !want, want)
		}
	}
}

func TestReadCoverageMatchesGlyphIndex(t *testing.T) {
	t.Parallel()

	for _, stem := range []string{"JetBrainsMono-Regular", "SymbolsNerdFontMono-Regular", "DejaVuSansMonoSymbols-Regular"} {
		t.Run(stem, func(t *testing.T) {
			t.Parallel()

			raw, err := readSource("embed:data/" + stem + ".ttf.gz")
			if err != nil {
				t.Fatal(err)
			}
			faces, err := readFaces(bytes.NewReader(raw), "test")
			if err != nil {
				t.Fatal(err)
			}
			cov, err := readCoverage(bytes.NewReader(raw), faces[0].tables)
			if err != nil {
				t.Fatal(err)
			}
			font, err := sfnt.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			assertCoverage(t, stem, cov, font, 0, 0x2FFFF)
			assertCoverage(t, stem, cov, font, 0xF0000, 0x10FFFF)
		})
	}
}

// TestReadCoverageMatchesSystemFonts cross-checks the cmap parser against sfnt
// on the BMP of a few installed fonts, when there are any.
func TestReadCoverageMatchesSystemFonts(t *testing.T) {
	t.Parallel()

	srcs := systemFontFiles(Dirs())
	if len(srcs) == 0 {
		t.Skip("no system fonts installed")
	}
	for _, f := range buildFallbackIndex(srcs[:min(len(srcs), 10)]) {
		font, err := loadFallbackFont(f.face)
		if err != nil {
			continue
		}
		assertCoverage(t, f.src, f.cov, font, 0, 0xFFFF)
	}
}

func TestCoverageNormalized(t *testing.T) {
	t.Parallel()

	cov := coverage{{10, 12}, {1, 3}, {4, 5}, {11, 20}, {30, 30}}.normalized()
	want := coverage{{1, 5}, {10, 20}, {30, 30}}
	if !slices.Equal(cov, want) {
		t.Fatalf("normalized() = %v, want %v", cov, want)
	}
}

func TestBuildFallbackIndex(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	paths := writeEmbeddedAs(t, dir, "JetBrainsMono-Bold", "JetBrainsMono-Italic", "DejaVuSansMonoSymbols-Regular", "JetBrainsMono-Regular")

	// A copy of the regular face that claims to be a color font is skipped.
	raw, err := os.ReadFile(paths[3])
	if err != nil {
		t.Fatal(err)
	}
	if i := bytes.Index(raw[:12+16*64], []byte("GPOS")); i < 0 {
		t.Fatal("GPOS table not found")
	} else {
		copy(raw[i:], "COLR")
	}
	if err = os.WriteFile(filepath.Join(dir, "unrelated-z.ttf"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	ix := buildFallbackIndex(systemFontFiles([]string{dir}))
	if len(ix) != 4 {
		t.Fatalf("got %d fallback faces, want 4 (color face skipped)", len(ix))
	}
	for _, f := range ix {
		if f.tables.color {
			t.Fatalf("color face %s was indexed", f.src)
		}
	}
	// Upright faces come first, then nearest to regular weight; the italic face
	// is last.
	if got := ix[len(ix)-1]; !got.italic {
		t.Fatalf("last face = %s, want the italic face", got.src)
	}
	if got := ix[0]; got.weight != 400 || got.italic {
		t.Fatalf("first face = %s (weight %d), want an upright regular face", got.src, got.weight)
	}

	for _, tc := range []struct {
		r      rune
		family string
		ok     bool
	}{
		{'A', "JetBrains Mono", true},
		{'★', "DejaVu Sans Mono Symbols", true},
		{'⣿', "", false},
		{'你', "", false},
		{' ', "", false}, // Mapped by every face, but has no ink.
	} {
		font, ok := lookupFallback(ix, tc.r)
		if ok != tc.ok {
			t.Fatalf("lookupFallback(%q) ok = %v, want %v", tc.r, ok, tc.ok)
		}
		if !ok {
			continue
		}
		name, nameErr := font.Name(nil, sfnt.NameIDFamily)
		if nameErr != nil {
			t.Fatal(nameErr)
		}
		if name != tc.family {
			t.Fatalf("lookupFallback(%q) family = %q, want %q", tc.r, name, tc.family)
		}
	}
}

func TestDefaultFallbackFamily(t *testing.T) {
	t.Parallel()

	family, err := DefaultFallbackFamily()
	if err != nil {
		t.Fatal(err)
	}
	if family.Regular == nil || family.Bold != nil {
		t.Fatalf("DefaultFallbackFamily() = %+v, want only Regular", family)
	}
	var buf sfnt.Buffer
	for _, r := range "★✔☀♪⚙◐◢⇄➤" {
		if idx, _ := family.Regular.GlyphIndex(&buf, r); idx == 0 {
			t.Errorf("fallback family has no glyph for %q", r)
		}
	}
}
