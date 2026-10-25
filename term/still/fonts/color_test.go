// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fonts

import (
	"bytes"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/font/sfnt"
)

// fixtureIndex writes f to a directory and indexes it like system fonts.
func fixtureIndex(t *testing.T, f fixture) []*colorFace {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fixture.ttf"), f.build(), 0o600); err != nil {
		t.Fatal(err)
	}
	ix := buildColorIndex(systemFontFiles([]string{dir}))
	if len(ix) != 1 {
		t.Fatalf("indexed %d color faces, want 1", len(ix))
	}
	return ix
}

// rgba is c's channels as they are: a color in the fixtures is opaque or solid.
func rgba(c color.NRGBA) color.RGBA { return color.RGBA(c) }

// opaquePixels counts the pixels of img that are fully opaque.
func opaquePixels(img *image.RGBA) (n int) {
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] == 255 {
			n++
		}
	}
	return n
}

func TestColorGlyphFixtures(t *testing.T) {
	t.Parallel()

	type variant struct {
		name   string
		format string
		ext    bool
		ranges bool
	}
	for _, v := range []variant{
		{"cbdt index 1", "cbdt1", false, false},
		{"cbdt index 3, extension lookup, range coverage", "cbdt3", true, true},
		{"sbix", "sbix", false, false},
		{"sbix, extension lookup, range coverage", "sbix", true, true},
	} {
		t.Run(v.name, func(t *testing.T) {
			t.Parallel()

			fx := standardFixture(v.format)
			fx.extension, fx.rangeCoverage = v.ext, v.ranges
			ix := fixtureIndex(t, fx)

			for _, tc := range []struct {
				name    string
				cluster string
				want    *color.NRGBA // Nil when no glyph is expected.
				only    string       // Format the case applies to.
			}{
				{"emoji", "😀", &fxRed, ""},
				{"another emoji", "👍", &fxGreen, ""},
				{"sequence uses the ligature", "👨‍👩", &fxMagenta, ""},
				{"flag", "🇺🇸", &fxCyan, ""},
				{"selector without bitmap is ignored", "❤️", &fxPink, ""},
				{"single substitution", "★", &fxOrange, ""},
				{"sbix dupe", "😁", &fxRed, "sbix"},
				{"sbix flip", "😂", &fxRed, "sbix"},
				{"mapped without a bitmap", "A", nil, ""},
				{"unmapped", "Z", nil, ""},
				{"two emoji the font cannot compose", "👨👩", nil, ""},
				{"half a flag", "🇺", nil, ""},
			} {
				if tc.only != "" && tc.only != v.format {
					continue
				}
				g, ok := lookupColor(ix, []rune(tc.cluster))
				if ok != (tc.want != nil) {
					t.Errorf("%s (%+q): resolved = %v, want %v", tc.name, tc.cluster, ok, tc.want != nil)
					continue
				}
				if !ok {
					continue
				}
				// Every glyph is a solid color, so the center shows which one
				// was picked.
				img := g.Image(32, 16)
				if img == nil {
					t.Errorf("%s: no image", tc.name)
					continue
				}
				if got := img.RGBAAt(16, 8); got != rgba(*tc.want) {
					t.Errorf("%s (%+q): center = %v, want %v", tc.name, tc.cluster, got, rgba(*tc.want))
				}
			}
		})
	}
}

func TestColorGlyphWithoutMappedSelector(t *testing.T) {
	t.Parallel()

	fx := standardFixture("cbdt1")
	delete(fx.cmap, 0xFE0F)
	ix := fixtureIndex(t, fx)
	if _, ok := lookupColor(ix, []rune("😀️")); !ok {
		t.Fatal("emoji followed by a selector the font doesn't map did not resolve")
	}
}

func TestColorGlyphImage(t *testing.T) {
	t.Parallel()

	ix := fixtureIndex(t, standardFixture("cbdt1"))
	square, _ := lookupColor(ix, []rune("😀"))
	wide, _ := lookupColor(ix, []rune("👨‍👩"))

	// A square glyph fits the height of a wide box, centered, leaving the
	// sides empty. The box is 20×10, so the glyph covers x 5..15.
	img := square.Image(20, 10)
	if got := img.Bounds(); got != image.Rect(0, 0, 20, 10) {
		t.Fatalf("image bounds = %v, want the box", got)
	}
	for _, tc := range []struct {
		x, y int
		want color.RGBA
	}{
		{10, 5, rgba(fxRed)},
		{5, 0, rgba(fxRed)},
		{14, 9, rgba(fxRed)},
		{2, 5, color.RGBA{}},
		{17, 5, color.RGBA{}},
	} {
		if got := img.RGBAAt(tc.x, tc.y); got != tc.want {
			t.Errorf("square: pixel (%d,%d) = %v, want %v", tc.x, tc.y, got, tc.want)
		}
	}

	// A glyph twice as wide as it is tall fills that box.
	img = wide.Image(20, 10)
	for _, p := range []image.Point{{0, 0}, {19, 9}, {10, 5}} {
		if got := img.RGBAAt(p.X, p.Y); got != rgba(fxMagenta) {
			t.Errorf("wide: pixel %v = %v, want %v", p, got, rgba(fxMagenta))
		}
	}

	// The last size is kept; a different size is drawn again.
	if a, b := square.Image(20, 10), square.Image(20, 10); a != b {
		t.Error("the same size was drawn twice")
	}
	if a, b := square.Image(20, 10), square.Image(10, 10); a == b || b.Bounds().Dx() != 10 {
		t.Error("a different size reused the image of another")
	}
	if got := square.Image(0, 10); got != nil {
		t.Errorf("Image(0, 10) = %v, want nil", got)
	}
}

// TestColorGlyphKeepsSeveralSizes makes sure a glyph drawn at two sizes in turn,
// as by renderers at different scales, is not drawn again each time.
func TestColorGlyphKeepsSeveralSizes(t *testing.T) {
	t.Parallel()

	g, ok := lookupColor(fixtureIndex(t, standardFixture("cbdt1")), []rune("😀"))
	if !ok {
		t.Fatal("no glyph for an emoji")
	}
	large, small := g.Image(16, 16), g.Image(8, 8)
	if large == nil || small == nil || g.Image(16, 16) != large || g.Image(8, 8) != small {
		t.Error("a glyph drawn at two sizes in turn was drawn again")
	}

	// A size the glyph can't be drawn at is remembered too, and sizes are bounded.
	for range 2 {
		if g.Image(0, 10) != nil {
			t.Error("an empty box has an image")
		}
	}
	for i := range 2 * maxGlyphSizes {
		g.Image(10+i, 10)
	}
	if len(g.imgs) > maxGlyphSizes {
		t.Errorf("glyph holds %d sizes, want at most %d", len(g.imgs), maxGlyphSizes)
	}
}

func TestColorGlyphCache(t *testing.T) {
	t.Parallel()

	ix := fixtureIndex(t, standardFixture("cbdt1"))
	var cache colorGlyphCache
	var finds int
	find := func(cluster []rune) *ColorGlyph {
		finds++
		g, _ := lookupColor(ix, cluster)
		return g
	}

	g1, ok := lookupColorCached(&cache, find, "😀")
	if !ok {
		t.Fatal("no glyph for an emoji")
	}
	if g2, _ := lookupColorCached(&cache, find, "😀"); g1 != g2 || finds != 1 {
		t.Errorf("lookup was not cached: %d lookups", finds)
	}
	// Text that cannot be emoji is never looked up.
	if g, found := lookupColorCached(&cache, func([]rune) *ColorGlyph { t.Error("lookup for CJK"); return nil }, "你"); found || g != nil {
		t.Errorf("CJK resolved to %v", g)
	}
	if _, found := lookupColorCached(&cache, find, "\xff"); found {
		t.Error("invalid UTF-8 resolved")
	}
	if _, found := lookupColorCached(&cache, find, ""); found {
		t.Error("empty cluster resolved")
	}

	// Failures are remembered too, and the cache stays bounded.
	finds = 0
	for range 2 {
		lookupColorCached(&cache, find, "🙈")
	}
	if finds > 1 {
		t.Errorf("a cluster no font has was looked up %d times", finds)
	}
	for i := range maxColorGlyphs + 10 {
		lookupColorCached(&cache, find, string(rune(0x1F000+i%0x600)))
	}
	if len(cache.m) > maxColorGlyphs {
		t.Errorf("cache holds %d clusters, want at most %d", len(cache.m), maxColorGlyphs)
	}
}

func TestMayBeEmoji(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		cluster string
		want    bool
	}{
		{"😀", true},
		{"🇺🇸", true},
		{"❤", true},
		{"⭐", true},
		{"©", true},
		{"〰", true},
		{"1️⃣", true},
		{"\U0001F3F4\U000E0067", true},
		{"a", false},
		{"你", false},
		{"한", false},
		{"é", false},
		{"\U00020000", false},
	} {
		if got := mayBeEmoji(tc.cluster); got != tc.want {
			t.Errorf("mayBeEmoji(%+q) = %v, want %v", tc.cluster, got, tc.want)
		}
	}
}

// TestColorFontsIgnoreCorruption reads truncated and damaged copies of each
// kind of font, which must never panic or hang. They come off the user's disk.
func TestColorFontsIgnoreCorruption(t *testing.T) {
	t.Parallel()

	for _, format := range []string{"cbdt1", "cbdt3", "sbix"} {
		t.Run(format, func(t *testing.T) {
			t.Parallel()

			fx := standardFixture(format)
			fx.extension = true
			raw := fx.build()
			try := func(name string, b []byte, at int) {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("%s at %d: panic: %v", name, at, r)
					}
				}()
				of := openColorReader(bytes.NewReader(b), 0)
				if of == nil {
					return
				}
				for _, c := range []string{"😀", "👨‍👩", "🇺🇸", "★", "😁"} {
					if layers, ok := of.glyph([]rune(c)); ok {
						_ = (&ColorGlyph{layers: layers}).Image(20, 10)
					}
				}
			}
			for n := range raw {
				try("truncated", raw[:n], n)
			}
			for i := range raw {
				for _, v := range []byte{0x00, 0xFF, 0x80} {
					damaged := bytes.Clone(raw)
					damaged[i] = v
					try("damaged", damaged, i)
				}
			}
		})
	}
}

func TestGlyphLookupFormat4(t *testing.T) {
	t.Parallel()

	// 'A'..'C' map through a glyph id array ('B' to nothing), 'a'..'b' through
	// a delta.
	var sub wbuf
	sub.u16(4, 14+8*3+2+2*3, 0, 6, 0, 0, 0) // Format, length, language, 3 segments x 2.
	sub.u16(0x43, 0x62, 0xFFFF)             // End codes.
	sub.u16(0)                              // Reserved.
	sub.u16(0x41, 0x61, 0xFFFF)             // Start codes.
	sub.u16(0, 20-0x61, 1)                  // Deltas.
	sub.u16(6, 0, 0)                        // Range offsets: the id array is 6 bytes on.
	sub.u16(10, 0, 12)                      // Glyph id array.
	var cmap wbuf
	cmap.u16(0, 1, 3, 1)
	cmap.u32(12)
	cmap.Write(sub.Bytes())

	lookup, err := newGlyphLookup(cmap.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		r    rune
		want uint16
	}{
		{'A', 10}, {'B', 0}, {'C', 12}, {'D', 0}, {'`', 0}, {'a', 20}, {'b', 21}, {'c', 0}, {0x10000, 0}, {0x1F600, 0},
	} {
		if got := lookup(tc.r); got != tc.want {
			t.Errorf("lookup(%q) = %d, want %d", tc.r, got, tc.want)
		}
	}
}

// TestGlyphLookupMatchesSfnt cross-checks the cmap lookup against sfnt.
func TestGlyphLookupMatchesSfnt(t *testing.T) {
	t.Parallel()

	for _, stem := range []string{"JetBrainsMono-Regular", "SymbolsNerdFontMono-Regular", "DejaVuSansMonoSymbols-Regular"} {
		t.Run(stem, func(t *testing.T) {
			t.Parallel()

			raw, err := readSource("embed:data/" + stem + ".ttf.gz")
			if err != nil {
				t.Fatal(err)
			}
			dir, err := readTableDir(bytes.NewReader(raw), 0)
			if err != nil {
				t.Fatal(err)
			}
			lookup, err := newGlyphLookup(readTable(bytes.NewReader(raw), dir, "cmap"))
			if err != nil {
				t.Fatal(err)
			}
			font, err := sfnt.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			var buf sfnt.Buffer
			for r := rune(0); r <= 0x2FFFF; r++ {
				idx, idxErr := font.GlyphIndex(&buf, r)
				if idxErr != nil {
					t.Fatalf("GlyphIndex(U+%04X): %v", r, idxErr)
				}
				if got := lookup(r); got != uint16(idx) { //nolint:gosec // Glyph indexes are 16 bits.
					t.Fatalf("lookup(U+%04X) = %d, sfnt has %d", r, got, idx)
				}
			}
		})
	}
}

func TestCOLRLayers(t *testing.T) {
	t.Parallel()

	// Base glyphs 3 (layers 0-1) and 9 (layer 2), sorted by glyph id.
	var colr wbuf
	colr.u16(0, 2)
	colr.u32(14, 14+12)
	colr.u16(3)
	colr.u16(3, 0, 2)
	colr.u16(9, 2, 1)
	colr.u16(5, 0, 6, 1, 7, 0xFFFF)
	s := &colrSource{colr: colr.Bytes()}

	for _, tc := range []struct {
		gid     uint16
		first   int
		n       int
		wantHas bool
	}{
		{3, 0, 2, true}, {9, 2, 1, true}, {1, 0, 0, false}, {4, 0, 0, false}, {10, 0, 0, false},
	} {
		first, n, ok := s.layers(tc.gid)
		if ok != tc.wantHas || (ok && (first != tc.first || n != tc.n)) {
			t.Errorf("layers(%d) = %d, %d, %v, want %d, %d, %v", tc.gid, first, n, ok, tc.first, tc.n, tc.wantHas)
		}
		if s.has(tc.gid) != tc.wantHas {
			t.Errorf("has(%d) = %v, want %v", tc.gid, !tc.wantHas, tc.wantHas)
		}
	}
}
