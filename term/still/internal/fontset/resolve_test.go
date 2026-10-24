// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fontset

import (
	"sync"
	"testing"

	"github.com/lrstanley/x/term/still/fonts"
	"github.com/lrstanley/x/term/still/internal/config"
	idraw "github.com/lrstanley/x/term/still/internal/draw"
	"github.com/lrstanley/x/term/vt/screen"
	xfont "golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

// buildTestSet builds a set with the given system fallback (nil disables it).
func buildTestSet(tb testing.TB, opts config.Options, system func(rune) (*opentype.Font, bool)) *Set {
	tb.Helper()
	opts.Scale = config.DefaultScale
	fs, err := build(opts, system)
	if err != nil {
		tb.Fatalf("build() error = %v", err)
	}
	tb.Cleanup(func() { _ = fs.Close() })
	return fs
}

func mustFamily(t *testing.T, load func() (fonts.FontFamily, error)) fonts.FontFamily {
	t.Helper()
	family, err := load()
	if err != nil {
		t.Fatal(err)
	}
	return family
}

// recordingFallback returns a system fallback that answers with font and
// records the runes it was asked for.
func recordingFallback(font *opentype.Font) (fallback func(rune) (*opentype.Font, bool), asked *[]rune) {
	asked = new([]rune)
	return func(r rune) (*opentype.Font, bool) {
		*asked = append(*asked, r)
		return font, font != nil
	}, asked
}

func faceFor(fs *Set, content string, attrs screen.Attr) xfont.Face {
	return faceForCell(fs, &screen.Cell{Content: content, Style: screen.Style{Attrs: attrs}})
}

func faceForCell(fs *Set, cell *screen.Cell) xfont.Face {
	cluster, _ := idraw.NewCluster(cell.Content)
	return fs.FaceForCell(cell, cluster)
}

func TestResolvePrimaryBeforeEmptySymbolRange(t *testing.T) {
	t.Parallel()

	fs := buildTestSet(t, config.Options{}, nil)

	// Geometric shapes and dingbats sit in the default symbol font's ranges,
	// but that font has no glyphs for these, so the primary face draws them.
	for _, s := range []string{"■", "●", "▶", "✓", "⌘", "⚠"} {
		if got := faceFor(fs, s, 0); got != fs.Regular() {
			t.Errorf("%s: face = %p, want primary regular %p", s, got, fs.Regular())
		}
	}
	// Nerd Font glyphs still come from the default symbol font.
	symbols := mustFamily(t, fonts.DefaultSymbolFamily)
	if got, want := faceFor(fs, "\ue0b4", 0), fs.faces[symbols.Regular]; got != want || got == nil {
		t.Errorf("U+E0B4: face = %p, want symbol face %p", got, want)
	}
}

func TestResolveBundledFallback(t *testing.T) {
	t.Parallel()

	system, asked := recordingFallback(nil)
	fs := buildTestSet(t, config.Options{}, system)
	fallback := mustFamily(t, fonts.DefaultFallbackFamily)

	for _, s := range []string{"★", "✔", "☀", "♪", "⚙", "◐", "◢", "⇄", "➤", "☐"} {
		got := faceFor(fs, s, 0)
		if want := fs.faces[fallback.Regular]; got != want || got == nil {
			t.Errorf("%s: face = %p, want bundled fallback %p", s, got, want)
		}
	}
	if len(*asked) != 0 {
		t.Fatalf("system fallback consulted for %q, want bundled fallback only", string(*asked))
	}

	regular, bold := faceFor(fs, "★", 0), faceFor(fs, "★", screen.AttrBold)
	if regular == bold {
		t.Fatal("bold fallback face should be synthesized from the regular face")
	}
}

func TestResolveSystemFallback(t *testing.T) {
	t.Parallel()

	// With the codepoint map disabled, the default symbol font is no longer
	// configured, which makes it a stand-in for an installed font.
	symbols := mustFamily(t, fonts.DefaultSymbolFamily)
	system, asked := recordingFallback(symbols.Regular)
	fs := buildTestSet(t, config.Options{CodepointMapSet: true}, system)

	got := faceFor(fs, "\ue0b4", 0)
	if got == fs.Regular() || got == nil {
		t.Fatalf("face = %p, want a system fallback face", got)
	}
	if b, _, ok := got.GlyphBounds('\ue0b4'); !ok || b.Empty() {
		t.Fatal("system fallback face has no ink for U+E0B4")
	}
	if faceFor(fs, "\ue0b4", screen.AttrBold) == got {
		t.Fatal("bold system fallback face should be synthesized from the regular face")
	}
	// Resolution is cached per style and rune.
	faceFor(fs, "\ue0b4", 0)
	if string(*asked) != "\ue0b4\ue0b4" {
		t.Fatalf("system fallback asked for %+q, want U+E0B4 once per style", string(*asked))
	}

	disabled := buildTestSet(t, config.Options{CodepointMapSet: true}, nil)
	if face := faceFor(disabled, "\ue0b4", 0); face != disabled.Regular() {
		t.Fatalf("disabled: face = %p, want primary regular %p", face, disabled.Regular())
	}
}

func TestResolveCodepointMapFallsThrough(t *testing.T) {
	t.Parallel()

	symbols := mustFamily(t, fonts.DefaultSymbolFamily)
	fallback := mustFamily(t, fonts.DefaultFallbackFamily)
	fs := buildTestSet(t, config.Options{
		CodepointMapSet: true,
		CodepointMap: map[string]fonts.FontFamily{
			"U+2600-U+26FF": symbols,
		},
	}, nil)

	// The mapped font has no ★, so the next face that does draws it.
	if got, want := faceFor(fs, "★", 0), fs.faces[fallback.Regular]; got != want {
		t.Fatalf("★: face = %p, want bundled fallback %p", got, want)
	}
}

func TestResolveReplacementCharacter(t *testing.T) {
	t.Parallel()

	fs := buildTestSet(t, config.Options{}, nil)
	if got := faceFor(fs, "\ufffd", 0); got != fs.Regular() {
		t.Fatalf("U+FFFD: face = %p, want primary regular %p", got, fs.Regular())
	}
	if c, ok := idraw.NewCluster("\ufffd"); !ok || c.Base != '\ufffd' {
		t.Fatalf("NewCluster(U+FFFD) = %+v, %v, want base U+FFFD", c, ok)
	}
}

func TestFaceForCellConcurrent(t *testing.T) {
	t.Parallel()

	fs := buildTestSet(t, config.Options{}, nil)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			for _, s := range []string{"A", "■", "★", "\ue0b0", "é", "⣿", "€"} {
				faceFor(fs, s, screen.Attr(i%2)*screen.AttrBold)
			}
		})
	}
	wg.Wait()
}

// BenchmarkFaceForCell resolves a mix of ASCII, Latin-1, primary symbols,
// bundled fallback symbols, Nerd Font icons, and sprite runes, after the first
// lookup of each has been cached.
func BenchmarkFaceForCell(b *testing.B) {
	fs := buildTestSet(b, config.Options{}, nil)

	var cells []*screen.Cell
	for _, s := range []string{"A", "z", "é", "■", "★", "\ue0b0", "\uf015", "▀", "─"} {
		cells = append(cells,
			&screen.Cell{Content: s},
			&screen.Cell{Content: s, Style: screen.Style{Attrs: screen.AttrBold}},
		)
	}
	for _, c := range cells {
		faceForCell(fs, c)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		faceForCell(fs, cells[i%len(cells)])
	}
}
