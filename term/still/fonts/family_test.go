// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fonts

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeEmbeddedAs decompresses the named embedded fonts into dir under
// deliberately misleading filenames, to prove names come from the font data.
func writeEmbeddedAs(t *testing.T, dir string, stems ...string) []string {
	t.Helper()
	var paths []string
	for i, stem := range stems {
		raw, err := readSource("embed:data/" + stem + ".ttf.gz")
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, "unrelated-"+string(rune('a'+i))+".ttf")
		if err := os.WriteFile(p, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	return paths
}

// buildCollection packs fonts into a single TTC, rewriting each table
// directory so offsets stay absolute within the collection.
func buildCollection(t *testing.T, fonts [][]byte) []byte {
	t.Helper()
	headerLen := 12 + 4*len(fonts)
	out := make([]byte, headerLen)
	copy(out, "ttcf")
	binary.BigEndian.PutUint32(out[4:], 0x00010000)
	binary.BigEndian.PutUint32(out[8:], uint32(len(fonts)))

	for i, raw := range fonts {
		for len(out)%4 != 0 {
			out = append(out, 0)
		}
		shift := uint32(len(out))
		binary.BigEndian.PutUint32(out[12+4*i:], shift)

		patched := append([]byte(nil), raw...)
		n := int(binary.BigEndian.Uint16(patched[4:]))
		for j := range n {
			rec := patched[12+16*j:]
			binary.BigEndian.PutUint32(rec[8:], binary.BigEndian.Uint32(rec[8:])+shift)
		}
		out = append(out, patched...)
	}
	return out
}

func TestFamilyIndex_NamesComeFromFontData(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeEmbeddedAs(t, dir, "JetBrainsMono-Regular", "JetBrainsMono-Bold", "JetBrainsMono-Italic", "JetBrainsMono-BoldItalic")

	ix := buildIndex(systemFontFiles([]string{dir}))
	faces := ix.faces[normalizeFamily("  jetbrains   MONO ")]
	if len(faces) != 4 {
		t.Fatalf("got %d faces for JetBrains Mono, want 4: %+v", len(faces), ix.names())
	}

	for _, tc := range []struct {
		style Style
		want  face
	}{
		{StyleRegular, face{weight: 400}},
		{StyleBold, face{weight: 700}},
		{StyleItalic, face{weight: 400, italic: true}},
		{StyleBoldItalic, face{weight: 700, italic: true}},
	} {
		got, ok := exactFace(faces, tc.style)
		if !ok || got.weight != tc.want.weight || got.italic != tc.want.italic {
			t.Errorf("%v: got weight=%d italic=%v ok=%v, want weight=%d italic=%v",
				tc.style, got.weight, got.italic, ok, tc.want.weight, tc.want.italic)
		}
	}
}

func TestFamilyIndex_Collection(t *testing.T) {
	t.Parallel()

	var fonts [][]byte
	for _, stem := range []string{"JetBrainsMono-Regular", "JetBrainsMono-Bold"} {
		raw, err := readSource("embed:data/" + stem + ".ttf.gz")
		if err != nil {
			t.Fatal(err)
		}
		fonts = append(fonts, raw)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "pack.ttc")
	if err := os.WriteFile(path, buildCollection(t, fonts), 0o600); err != nil {
		t.Fatal(err)
	}

	faces, err := readSourceFaces("file:" + path)
	if err != nil {
		t.Fatal(err)
	}
	if len(faces) != 2 || faces[0].weight != 400 || faces[1].weight != 700 || faces[1].index != 1 {
		t.Fatalf("unexpected collection faces: %+v", faces)
	}

	// The second face must be loadable by collection index.
	font, err := loadFace(faces[1])
	if err != nil {
		t.Fatal(err)
	}
	if font == nil {
		t.Fatal("nil font")
	}
}

func TestFamilyIndex_SkipsNonFonts(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.ttf"), []byte("not a font at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "empty.otf"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	writeEmbeddedAs(t, dir, "JetBrainsMono-Regular")

	ix := buildIndex(systemFontFiles([]string{dir}))
	if got := ix.names(); len(got) != 1 || got[0] != "JetBrains Mono" {
		t.Fatalf("names = %v, want [JetBrains Mono]", got)
	}
}

func TestBestFace(t *testing.T) {
	t.Parallel()

	faces := []face{
		{src: "light", weight: 300},
		{src: "regular", weight: 400},
		{src: "medium", weight: 500},
		{src: "bold", weight: 700},
		{src: "black-italic", weight: 900, italic: true},
	}
	for _, tc := range []struct {
		name  string
		faces []face
		want  Style
		exact bool
		src   string // Empty means no match.
	}{
		{"regular exact", faces, StyleRegular, false, "regular"},
		{"bold exact", faces, StyleBold, true, "bold"},
		{"italic exact has none", faces, StyleItalic, true, ""},
		{"bold italic exact", faces, StyleBoldItalic, true, "black-italic"},
		{"italic falls back to closest italic", faces, StyleItalic, false, "black-italic"},
		{"regular prefers 500 over 300 when 400 missing", faces[:1:1], StyleRegular, false, "light"},
		{"400 missing: 500 before lighter", []face{{src: "l", weight: 300}, {src: "m", weight: 500}}, StyleRegular, false, "m"},
		{"500 missing: 400 before lighter and heavier", []face{{src: "r", weight: 400}, {src: "b", weight: 700}, {src: "l", weight: 300}}, Style{Weight: 500}, false, "r"},
		{"300 wanted: lighter before heavier", []face{{src: "t", weight: 200}, {src: "r", weight: 400}}, Style{Weight: 300}, false, "t"},
		{"bold wanted: heavier before lighter", []face{{src: "l", weight: 300}, {src: "x", weight: 800}}, StyleBold, false, "x"},
		{"bold not matched by regular-only family", faces[:2], StyleBold, true, ""},
		{"ties go to the earlier face", []face{{src: "first", weight: 400}, {src: "second", weight: 400}}, StyleRegular, false, "first"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			find := bestFace
			if tc.exact {
				find = exactFace
			}
			got, ok := find(tc.faces, tc.want)
			if (tc.src == "") == ok || got.src != tc.src {
				t.Fatalf("got src=%q ok=%v, want src=%q", got.src, ok, tc.src)
			}
		})
	}
}

func TestWeightItalicFromStyle(t *testing.T) {
	t.Parallel()

	for style, want := range map[string]struct {
		weight int
		italic bool
	}{
		"Regular":        {400, false},
		"Bold Italic":    {700, true},
		"SemiBold":       {600, false},
		"Extra Light":    {200, false},
		"Medium Oblique": {500, true},
		"ExtraBold":      {800, false},
		"Black Italic":   {900, true},
		"Something Else": {400, false},
		"":               {400, false},
		"Light":          {300, false},
		"Thin":           {100, false},
	} {
		w, i := weightItalicFromStyle(style)
		if w != want.weight || i != want.italic {
			t.Errorf("%q: got (%d, %v), want (%d, %v)", style, w, i, want.weight, want.italic)
		}
	}
}

func TestLoadFamily_Embedded(t *testing.T) {
	t.Parallel()

	family, err := LoadFamily("jetbrains mono")
	if err != nil {
		t.Fatal(err)
	}
	if family.Regular == nil || family.Bold == nil || family.Italic == nil || family.BoldItalic == nil {
		t.Fatalf("expected all four variants: %+v", family)
	}
	if family.Regular == family.Bold || family.Regular == family.Italic || family.Bold == family.BoldItalic {
		t.Fatal("variants must be distinct fonts")
	}

}

func TestLoadFamily_OnlyRegularLeavesOthersNil(t *testing.T) {
	t.Parallel()

	family, err := LoadFamily("Symbols Nerd Font Mono")
	if err != nil {
		t.Fatal(err)
	}
	if family.Regular == nil {
		t.Fatal("regular must be set")
	}
	if family.Bold != nil || family.Italic != nil || family.BoldItalic != nil {
		t.Fatalf("variants the family lacks must be nil: %+v", family)
	}
}

func TestLoadFace_Fallback(t *testing.T) {
	t.Parallel()

	// Symbols has only a regular face; any style resolves to it, like a browser.
	bold, err := LoadFace("Symbols Nerd Font Mono", StyleBold)
	if err != nil {
		t.Fatal(err)
	}
	regular, err := LoadFace("Symbols Nerd Font Mono", StyleRegular)
	if err != nil {
		t.Fatal(err)
	}
	if bold != regular {
		t.Fatal("expected fallback to the only face")
	}
}

func TestLoadFamily_NotFound(t *testing.T) {
	t.Parallel()

	_, err := LoadFamily("Definitely Not A Font")
	if !errors.Is(err, ErrFamilyNotFound) {
		t.Fatalf("err = %v, want ErrFamilyNotFound", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, `"Definitely Not A Font"`) {
		t.Errorf("error should name the family: %s", msg)
	}
	for _, dir := range Dirs() {
		if !strings.Contains(msg, dir) {
			t.Errorf("error should list searched dir %q: %s", dir, msg)
		}
	}

	if _, err := LoadFamily("  "); err == nil || errors.Is(err, ErrFamilyNotFound) {
		t.Errorf("empty name should be a distinct error, got %v", err)
	}
}

func TestLoadFamily_SimilarSuggestion(t *testing.T) {
	t.Parallel()

	_, err := LoadFamily("JetBrains")
	if err == nil || !strings.Contains(err.Error(), `"JetBrains Mono"`) {
		t.Fatalf("expected a suggestion for JetBrains Mono, got %v", err)
	}
}

func TestFamilies(t *testing.T) {
	t.Parallel()

	var found bool
	for _, name := range Families() {
		if name == "JetBrains Mono" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Families() missing embedded JetBrains Mono: %v", Families())
	}
}
