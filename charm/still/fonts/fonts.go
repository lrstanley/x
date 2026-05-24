// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fonts

import (
	"compress/gzip"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

//go:embed data/*
var embeddedFonts embed.FS

// gzipTTFStem returns the font base name for a data/*.ttf.gz filename.
func gzipTTFStem(name string) (string, bool) {
	ln := strings.ToLower(name)
	if !strings.HasSuffix(ln, ".ttf.gz") {
		return "", false
	}
	stem := name[:len(name)-len(".ttf.gz")]
	if stem == "" {
		return "", false
	}
	return stem, true
}

type embedFont struct {
	stem string
	rel  string // Slash-separated path under the embedded root.
}

var readEmbeddedFonts = sync.OnceValue(func() []embedFont {
	entries, err := fs.ReadDir(embeddedFonts, "data")
	if err != nil {
		return nil
	}
	var out []embedFont
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		stem, ok := gzipTTFStem(e.Name())
		if !ok {
			continue
		}
		rel := filepath.ToSlash(filepath.Join("data", e.Name()))
		out = append(out, embedFont{stem: stem, rel: rel})
	}
	return out
})

// ttfStem returns the base name for a filesystem .ttf file.
func ttfStem(filename string) (string, bool) {
	if strings.ToLower(filepath.Ext(filename)) != ".ttf" {
		return "", false
	}
	return strings.TrimSuffix(filename, filepath.Ext(filename)), true
}

// Dirs returns absolute directories that may contain system TrueType fonts,
// in the same order as the platform font search (earlier entries are tried
// before later ones, e.g. user locations before system paths on Unix).
// Absolute paths are deduplicated; the first occurrence wins.
func Dirs() []string {
	seen := map[string]struct{}{}
	var out []string
	for _, dir := range fontDirectories() {
		d := expandUser(dir)
		abs, err := filepath.Abs(d)
		if err != nil {
			continue
		}
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}
		out = append(out, abs)
	}
	return out
}

// List returns available TrueType font base names (no extension, including
// gzipped embedded assets) from embedded data and system directories. Names are
// unique when compared case-insensitively. Embedded stems appear first in data
// directory iteration order, then system fonts in discovery order (the same as
// directory precedence in [Dirs] and [Load]).
func List() []string {
	seen := map[string]struct{}{}
	var out []string

	for _, ef := range readEmbeddedFonts() {
		k := strings.ToLower(ef.stem)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, ef.stem)
	}

	for _, s := range systemFontStems() {
		k := strings.ToLower(s)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, s)
	}
	return out
}

// FontFamily contains parsed font variants for a terminal font family.
//
// Regular is required for loaded families. Missing style variants intentionally
// remain nil so callers can decide whether to synthesize or alias them.
type FontFamily struct {
	Regular          *opentype.Font
	Bold             *opentype.Font
	Italic           *opentype.Font
	BoldItalic       *opentype.Font
	DisableSynthetic bool
}

// FontFamilyNames identifies font names to load into a [FontFamily].
//
// Regular is required. Bold, Italic, and BoldItalic are optional and are not
// inferred from Regular.
type FontFamilyNames struct {
	Regular          string
	Bold             string
	Italic           string
	BoldItalic       string
	DisableSynthetic bool
}

// LoadFamily resolves a structured family of parsed TrueType fonts. Regular is
// required; omitted variants remain nil.
func LoadFamily(names FontFamilyNames) (FontFamily, error) {
	regularName := strings.TrimSpace(names.Regular)
	if regularName == "" {
		return FontFamily{}, errors.New("fonts: regular font name must not be empty")
	}

	regular, err := Load(regularName)
	if err != nil {
		return FontFamily{}, fmt.Errorf("fonts: load regular font %q: %w", names.Regular, err)
	}

	family := FontFamily{
		Regular:          regular,
		DisableSynthetic: names.DisableSynthetic,
	}
	if names.Bold != "" {
		if family.Bold, err = loadOptionalFamilyFont(names.Bold, "bold"); err != nil {
			return FontFamily{}, err
		}
	}
	if names.Italic != "" {
		if family.Italic, err = loadOptionalFamilyFont(names.Italic, "italic"); err != nil {
			return FontFamily{}, err
		}
	}
	if names.BoldItalic != "" {
		if family.BoldItalic, err = loadOptionalFamilyFont(names.BoldItalic, "bold italic"); err != nil {
			return FontFamily{}, err
		}
	}
	return family, nil
}

// MustLoadFamily is like [LoadFamily] but panics if the family cannot be loaded.
func MustLoadFamily(names FontFamilyNames) FontFamily {
	family, err := LoadFamily(names)
	if err != nil {
		panic(err)
	}
	return family
}

// DefaultFamily loads the bundled JetBrains Mono family variants.
func DefaultFamily() (FontFamily, error) {
	return LoadFamily(FontFamilyNames{
		Regular:    "JetBrainsMono-Regular",
		Bold:       "JetBrainsMono-Bold",
		Italic:     "JetBrainsMono-Italic",
		BoldItalic: "JetBrainsMono-BoldItalic",
	})
}

// DefaultSymbolFamily loads the bundled symbol font family. Only Regular is set.
func DefaultSymbolFamily() (FontFamily, error) {
	return LoadFamily(FontFamilyNames{Regular: "SymbolsNerdFontMono-Regular"})
}

func loadOptionalFamilyFont(name, role string) (*opentype.Font, error) {
	f, err := Load(strings.TrimSpace(name))
	if err != nil {
		return nil, fmt.Errorf("fonts: load %s font %q: %w", role, name, err)
	}
	return f, nil
}

func forEachSystemFont(fn func(path, stem string) bool) {
	for _, dir := range fontDirectories() {
		d := expandUser(dir)
		_ = filepath.WalkDir(d, func(path string, de fs.DirEntry, err error) error {
			if err != nil || de.IsDir() {
				return nil //nolint:nilerr // Ignore errors and continue searching.
			}
			base, ok := ttfStem(de.Name())
			if !ok {
				return nil
			}
			if !fn(path, base) {
				return fs.SkipAll
			}
			return nil
		})
	}
}

func systemFontStems() []string {
	var stems []string
	seen := map[string]struct{}{}
	forEachSystemFont(func(_ string, stem string) bool {
		k := strings.ToLower(stem)
		if _, dup := seen[k]; dup {
			return true
		}
		seen[k] = struct{}{}
		stems = append(stems, stem)
		return true
	})
	return stems
}

// MustLoad resolves a TrueType font by base name (optional .ttf / .ttf.gz suffixes
// are ignored; matching is case-insensitive). Embedded gzip-compressed assets
// are preferred, then system font directories.
//
// Panics if the font cannot be loaded.
func MustLoad(name string) *opentype.Font {
	f, err := Load(name)
	if err != nil {
		panic(err)
	}
	return f
}

// Load resolves a TrueType font by base name (optional .ttf / .ttf.gz suffixes
// are ignored; matching is case-insensitive). Embedded gzip-compressed assets
// are preferred, then system font directories.
func Load(name string) (*opentype.Font, error) {
	norm := normalizeFontName(name)
	if norm == "" {
		return nil, errors.New("fonts: empty font name")
	}
	src, err := locate(norm)
	if err != nil {
		return nil, fmt.Errorf("fonts: load %q: %w", name, err)
	}

	parsedMu.RLock()
	tf, ok := parsedFonts[norm]
	parsedMu.RUnlock()
	if ok {
		return tf, nil
	}

	parsedMu.Lock()
	defer parsedMu.Unlock()
	if tf, ok = parsedFonts[norm]; ok {
		return tf, nil
	}
	tf, err = parseFont(src)
	if err != nil {
		return nil, fmt.Errorf("fonts: parse %q: %w", name, err)
	}
	if parsedFonts == nil {
		parsedFonts = make(map[string]*opentype.Font)
	}
	parsedFonts[norm] = tf
	return tf, nil
}

// NewFace creates a drawable face from a parsed font and face options.
func NewFace(f *opentype.Font, opts *opentype.FaceOptions) (font.Face, error) {
	if f == nil {
		return nil, errors.New("fonts: nil font")
	}
	face, err := opentype.NewFace(f, opts)
	if err != nil {
		return nil, fmt.Errorf("fonts: create face: %w", err)
	}
	return face, nil
}

var (
	parsedMu    sync.RWMutex
	parsedFonts map[string]*opentype.Font // keyed by [normalizeFontName]

	locateMu    sync.RWMutex
	locateCache map[string]string // keyed by norm; successful [locateMiss] only
)

// resetFontCachesForTest clears locate and parsed-font caches. It is only for
// tests and benchmarks in this package.
func resetFontCachesForTest() {
	locateMu.Lock()
	locateCache = nil
	locateMu.Unlock()

	parsedMu.Lock()
	parsedFonts = nil
	parsedMu.Unlock()
}

func normalizeFontName(name string) string {
	s := strings.TrimSpace(name)
	if s == "" {
		return ""
	}
	lower := strings.ToLower(s)
	switch {
	case strings.HasSuffix(lower, ".ttf.gz"):
		s = s[:len(s)-len(".ttf.gz")]
	case strings.HasSuffix(lower, ".ttf"):
		s = s[:len(s)-len(".ttf")]
	}
	return strings.ToLower(strings.TrimSpace(s))
}

func locate(norm string) (string, error) {
	locateMu.RLock()
	src, ok := locateCache[norm]
	locateMu.RUnlock()
	if ok {
		return src, nil
	}

	src, err := locateMiss(norm)
	if err != nil {
		return "", err
	}

	locateMu.Lock()
	if prev, dup := locateCache[norm]; dup {
		locateMu.Unlock()
		return prev, nil
	}
	if locateCache == nil {
		locateCache = make(map[string]string)
	}
	locateCache[norm] = src
	locateMu.Unlock()
	return src, nil
}

func locateMiss(norm string) (string, error) {
	for _, ef := range readEmbeddedFonts() {
		if strings.EqualFold(ef.stem, norm) {
			return "embed:" + ef.rel, nil
		}
	}
	path, err := findSystemFont(norm)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return "file:" + abs, nil
}

func findSystemFont(norm string) (string, error) {
	var found string
	forEachSystemFont(func(path, stem string) bool {
		if strings.EqualFold(stem, norm) {
			found = path
			return false
		}
		return true
	})
	if found != "" {
		return found, nil
	}
	return "", errors.New("truetype font not found")
}

func parseFont(src string) (*opentype.Font, error) {
	if after, ok := strings.CutPrefix(src, "embed:"); ok {
		rc, err := embeddedFonts.Open(after)
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		gr, err := gzip.NewReader(rc)
		if err != nil {
			return nil, err
		}
		defer gr.Close()
		raw, err := io.ReadAll(gr)
		if err != nil {
			return nil, err
		}
		return opentype.Parse(raw)
	}
	if after, ok := strings.CutPrefix(src, "file:"); ok {
		raw, err := os.ReadFile(after)
		if err != nil {
			return nil, err
		}
		return opentype.Parse(raw)
	}
	return nil, fmt.Errorf("unknown font source %q", src)
}
