// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fonts

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/image/font/opentype"
)

// ErrFamilyNotFound is wrapped by errors returned when a font family cannot be
// found by name.
var ErrFamilyNotFound = errors.New("font family not found")

// Style selects a face within a font family, using CSS-style numeric weights.
type Style struct {
	// Weight is the CSS font weight, 1-1000. 400 is regular and 700 is bold.
	Weight int
	// Italic selects italic or oblique faces.
	Italic bool
}

// Common styles.
var (
	StyleRegular    = Style{Weight: 400}
	StyleBold       = Style{Weight: 700}
	StyleItalic     = Style{Weight: 400, Italic: true}
	StyleBoldItalic = Style{Weight: 700, Italic: true}
)

func (s Style) String() string {
	if s.Italic {
		return strconv.Itoa(s.Weight) + " italic"
	}
	return strconv.Itoa(s.Weight)
}

func (s Style) bold() bool { return s.Weight >= boldWeight }

// Families returns the sorted names of the font families available from
// embedded fonts and the system font directories, as written in the fonts
// themselves. The first call reads the name tables of all installed fonts.
func Families() []string {
	merged := maps.Clone(embeddedFamilyIndex().display)
	for key, name := range systemFamilyIndex().display {
		if _, ok := merged[key]; !ok {
			merged[key] = name
		}
	}
	return slices.SortedFunc(maps.Values(merged), compareFold)
}

// LoadFace resolves a font from a family name and style, the way browsers do:
// the family is the name recorded in the font file (for example "JetBrains
// Mono" or "FiraMono Nerd Font Mono"), compared case-insensitively. When the
// family has no exact match for style, the closest face is chosen using CSS
// font matching (italic preferred as requested, then nearest weight).
//
// Embedded fonts are searched first, then system fonts. If the family cannot be
// found, the returned error wraps [ErrFamilyNotFound] and names the family and
// the directories that were searched.
func LoadFace(family string, style Style) (*opentype.Font, error) {
	faces, err := lookupFamily(family)
	if err != nil {
		return nil, err
	}
	best, _ := bestFace(faces, style) // lookupFamily never returns an empty family.
	return loadFace(best)
}

// LoadFamily resolves a [FontFamily] from a family name, loading the
// Regular, Bold, Italic, and Bold Italic faces from the font files. See
// [LoadFace] for how names are matched.
//
// Regular always resolves. Bold, Italic, and BoldItalic are only set if the
// family has a real face for them; otherwise they stay nil so synthetic
// variants (see [FontFamily]) can fill in.
func LoadFamily(name string) (FontFamily, error) {
	faces, err := lookupFamily(name)
	if err != nil {
		return FontFamily{}, err
	}

	var family FontFamily
	regular, _ := bestFace(faces, StyleRegular)
	if family.Regular, err = loadFace(regular); err != nil {
		return FontFamily{}, fmt.Errorf("fonts: load regular face of family %q: %w", name, err)
	}
	for _, v := range []struct {
		style Style
		dst   **opentype.Font
	}{
		{StyleBold, &family.Bold},
		{StyleItalic, &family.Italic},
		{StyleBoldItalic, &family.BoldItalic},
	} {
		face, ok := exactFace(faces, v.style)
		if !ok {
			continue
		}
		if *v.dst, err = loadFace(face); err != nil {
			return FontFamily{}, fmt.Errorf("fonts: load %s face of family %q: %w", v.style, name, err)
		}
	}
	return family, nil
}

// lookupFamily returns the faces of a family, preferring embedded fonts.
func lookupFamily(name string) ([]face, error) {
	key := normalizeFamily(name)
	if key == "" {
		return nil, errors.New("fonts: family name must not be empty")
	}
	if faces := embeddedFamilyIndex().faces[key]; len(faces) > 0 {
		return faces, nil
	}
	sys := systemFamilyIndex()
	if faces := sys.faces[key]; len(faces) > 0 {
		return faces, nil
	}
	return nil, familyNotFound(name, key, sys)
}

func familyNotFound(name, key string, sys *familyIndex) error {
	msg := "searched embedded fonts"
	if dirs := Dirs(); len(dirs) > 0 {
		msg += " and " + strings.Join(dirs, ", ")
	}
	if similar := similarFamilies(key, embeddedFamilyIndex(), sys); len(similar) > 0 {
		msg += "; similar: " + strings.Join(similar, ", ")
	}
	return fmt.Errorf("fonts: family %q: %w (%s)", name, ErrFamilyNotFound, msg)
}

// similarFamilies returns up to five known family names that contain, or are
// contained by, key when spaces are ignored.
func similarFamilies(key string, indexes ...*familyIndex) []string {
	squash := func(s string) string { return strings.ReplaceAll(s, " ", "") }
	want := squash(key)

	var out []string
	seen := map[string]struct{}{}
	for _, ix := range indexes {
		for _, name := range ix.names() {
			got := squash(normalizeFamily(name))
			if _, ok := seen[got]; ok || (!strings.Contains(got, want) && !strings.Contains(want, got)) {
				continue
			}
			seen[got] = struct{}{}
			out = append(out, strconv.Quote(name))
		}
	}
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

// bestFace picks the face that best matches want: italic as requested first,
// then the nearest weight by [weightRank]. Earlier faces win ties. ok is false
// only for an empty slice.
func bestFace(faces []face, want Style) (best face, ok bool) {
	if len(faces) == 0 {
		return face{}, false
	}
	rank := func(f face) int {
		r := weightRank(want.Weight, f.weight)
		if f.italic != want.Italic {
			r += 1 << 20
		}
		return r
	}
	return slices.MinFunc(faces, func(a, b face) int { return cmp.Compare(rank(a), rank(b)) }), true
}

// exactFace is like [bestFace] but only considers faces that really are the
// requested kind: the same italic flag and the same bold-ness. ok is false if
// the family has no such face.
func exactFace(faces []face, want Style) (face, bool) {
	return bestFace(slices.DeleteFunc(slices.Clone(faces), func(f face) bool {
		return f.italic != want.Italic || f.bold() != want.bold()
	}), want)
}

// weightRank orders weights by preference (lower is better) following the CSS
// font matching algorithm. For desired weights below 400, lighter weights come
// first (nearest first), then heavier ones; above 500 the reverse. For 400 and
// 500, weights within the 400-500 band come first, then lighter, then heavier.
func weightRank(want, have int) int {
	var group int // 0 is the most preferred side, then 1, then 2.
	switch {
	case want < 400:
		if have > want {
			group = 1
		}
	case want > 500:
		if have < want {
			group = 1
		}
	case have >= 400 && have <= 500:
	case have < 400:
		group = 1
	default:
		group = 2
	}
	return group*1000 + max(have-want, want-have)
}

// faceCache maps a face's source and collection index to a lazily-run loader,
// so each font file is read and parsed once and concurrent loads of different
// faces do not block each other.
var faceCache sync.Map // string -> faceLoader

type faceLoader = func() (*opentype.Font, error)

func loadFace(f face) (*opentype.Font, error) {
	key := f.src + "#" + strconv.Itoa(f.index)

	load, ok := faceCache.Load(key)
	if !ok {
		load, _ = faceCache.LoadOrStore(key, sync.OnceValues(faceLoader(func() (*opentype.Font, error) {
			return parseFace(f)
		})))
	}
	font, err := load.(faceLoader)()
	if err != nil {
		faceCache.Delete(key) // Don't cache failures; a later attempt may succeed.
	}
	return font, err
}

func parseFace(f face) (*opentype.Font, error) {
	raw, err := readSource(f.src)
	if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(raw, []byte("ttcf")) {
		return opentype.Parse(raw)
	}
	col, err := opentype.ParseCollection(raw)
	if err != nil {
		return nil, err
	}
	return col.Font(f.index)
}
