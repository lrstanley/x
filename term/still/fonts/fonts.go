// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package fonts resolves OpenType fonts (TTF, OTF, and collections) by the
// family and style names recorded in the font files themselves, from embedded
// assets and system directories, builds [FontFamily] values with style
// variants, and provides synthetic bold and italic faces when a family omits
// them.
//
// To use a specific font file, parse it yourself (for example with
// [opentype.Parse]) and set the faces of a [FontFamily] directly.
package fonts

import (
	"compress/gzip"
	"embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

//go:embed data/*
var embeddedFonts embed.FS

// Dirs returns absolute directories that may contain system OpenType fonts,
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

// MustLoadFamily is like [LoadFamily] but panics if the family cannot be loaded.
func MustLoadFamily(name string) FontFamily {
	family, err := LoadFamily(name)
	if err != nil {
		panic(err)
	}
	return family
}

// DefaultFamily loads the bundled JetBrains Mono family variants.
func DefaultFamily() (FontFamily, error) {
	return LoadFamily("JetBrains Mono")
}

// DefaultSymbolFamily loads the bundled symbol font family. Only Regular is set.
func DefaultSymbolFamily() (FontFamily, error) {
	return LoadFamily("Symbols Nerd Font Mono")
}

// DefaultFallbackFamily loads the bundled symbol fallback family, a subset of
// DejaVu Sans Mono covering punctuation, letterlike, arrow, math, technical,
// geometric, and dingbat symbols. Only Regular is set.
func DefaultFallbackFamily() (FontFamily, error) {
	return LoadFamily("DejaVu Sans Mono Symbols")
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

// readSource returns the decompressed bytes for an "embed:" or "file:" source.
func readSource(src string) ([]byte, error) {
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
		return io.ReadAll(gr)
	}
	if after, ok := strings.CutPrefix(src, "file:"); ok {
		return os.ReadFile(after)
	}
	return nil, fmt.Errorf("unknown font source %q", src)
}
