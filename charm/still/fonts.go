// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

// Font loading and face selection intentionally mirror semantics described in Ghostty's
// font configuration reference (bundled defaults, bold/italic family selection,
// freetype/load-flag concepts, codepoint overrides).
//
// https://github.com/ghostty-org/ghostty/blob/main/src/font/Metrics.zig
// https://github.com/ghostty-org/ghostty/blob/main/src/font/CodepointResolver.zig

import (
	"errors"
	"fmt"
	"slices"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/charm/still/fonts"
	"github.com/lrstanley/x/charm/still/internal/codepoints"
	xfont "golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

// fontStyle classifies terminal text style for resolving an [opentype.Face].
type fontStyle uint8

const (
	fontStyleRegular fontStyle = iota
	fontStyleBold
	fontStyleItalic
	fontStyleBoldItalic
)

// fontSet holds concurrently reusable [xfont.Face] values for primary styles and
// optional per-range overrides.
type fontSet struct {
	regular    xfont.Face
	bold       xfont.Face
	italic     xfont.Face
	boldItalic xfont.Face

	gridMetrics faceMetrics
	gridFaces   map[xfont.Face]struct{}

	// faces are keyed by parsed font pointer and shared across styles and
	// codepoint maps within this font set.
	faces      map[*opentype.Font]xfont.Face
	synthetics map[syntheticFaceKey]xfont.Face
	// codepoints lists user mappings first then defaults; [fontSet.faceForGlyph]
	// walks in order (compare Ghostty CodepointResolver + font-codepoint-map).
	codepoints []codepointRange
}

// codepointRange pairs a unicode range with a font family, analogous to repeating
// font-codepoint-map entries in Ghostty.
type codepointRange struct {
	codepoints.Range
	family fonts.FontFamily
	faces  [4]xfont.Face
}

type syntheticFaceKey struct {
	base   xfont.Face
	bold   bool
	italic bool
}

// buildFontSet loads primary style faces plus any faces referenced by explicit
// or default codepoint maps using the same DPI and fractional point size as the
// renderer metrics path.
//
// Hinting uses [xfont.HintingFull] for deterministic grid rendering.
func buildFontSet(opts rendererOptions) (*fontSet, error) {
	faceOpts := &opentype.FaceOptions{
		Size:    float64(opts.fontSize),
		DPI:     float64(opts.dpi),
		Hinting: xfont.HintingFull,
	}
	fs := &fontSet{
		faces:      map[*opentype.Font]xfont.Face{},
		synthetics: map[syntheticFaceKey]xfont.Face{},
		gridFaces:  map[xfont.Face]struct{}{},
	}

	primaryFamily, err := configuredFontFamily(opts)
	if err != nil {
		return nil, err
	}
	if fs.regular, err = fs.loadFamilyFace(primaryFamily, fontStyleRegular, faceOpts); err != nil {
		return nil, err
	}
	fs.gridMetrics = measureFace(fs.regular)
	if fs.bold, err = fs.loadFamilyFace(primaryFamily, fontStyleBold, faceOpts); err != nil {
		return nil, err
	}
	if fs.italic, err = fs.loadFamilyFace(primaryFamily, fontStyleItalic, faceOpts); err != nil {
		return nil, err
	}
	if fs.boldItalic, err = fs.loadFamilyFace(primaryFamily, fontStyleBoldItalic, faceOpts); err != nil {
		return nil, err
	}
	for _, face := range []xfont.Face{fs.regular, fs.bold, fs.italic, fs.boldItalic} {
		if face != nil {
			fs.gridFaces[face] = struct{}{}
		}
	}

	ranges, err := parseCodepointMap(opts)
	if err != nil {
		return nil, err
	}
	for i := range ranges {
		for _, style := range []fontStyle{fontStyleRegular, fontStyleBold, fontStyleItalic, fontStyleBoldItalic} {
			if ranges[i].faces[style], err = fs.loadFamilyFace(ranges[i].family, style, faceOpts); err != nil {
				return nil, err
			}
		}
	}
	fs.codepoints = ranges

	return fs, nil
}

func configuredFontFamily(opts rendererOptions) (fonts.FontFamily, error) {
	if opts.fontFamilySet {
		if err := validateFontFamily("primary", opts.fontFamily); err != nil {
			return fonts.FontFamily{}, err
		}
		return opts.fontFamily, nil
	}
	family, err := fonts.DefaultFamily()
	if err != nil {
		return fonts.FontFamily{}, fmt.Errorf("load default font family: %w", err)
	}
	if validateErr := validateFontFamily("primary", family); validateErr != nil {
		return fonts.FontFamily{}, validateErr
	}
	return family, nil
}

func validateFontFamily(name string, family fonts.FontFamily) error {
	if family.Regular == nil {
		return fmt.Errorf("%s font family regular font must not be nil", name)
	}
	return nil
}

// loadFamilyFace creates or reuses a real or synthetic face for style.
func (fs *fontSet) loadFamilyFace(family fonts.FontFamily, style fontStyle, opts *opentype.FaceOptions) (xfont.Face, error) {
	if err := validateFontFamily("font", family); err != nil {
		return nil, err
	}
	regular, err := fs.loadParsedFace(family.Regular, opts)
	if err != nil {
		return nil, err
	}
	switch style {
	case fontStyleRegular:
		return regular, nil
	case fontStyleBold:
		if family.Bold != nil {
			return fs.loadParsedFace(family.Bold, opts)
		}
		if family.DisableSynthetic {
			return regular, nil
		}
		return fs.loadSyntheticFace(regular, true, false, opts), nil
	case fontStyleItalic:
		if family.Italic != nil {
			return fs.loadParsedFace(family.Italic, opts)
		}
		if family.DisableSynthetic {
			return regular, nil
		}
		return fs.loadSyntheticFace(regular, false, true, opts), nil
	case fontStyleBoldItalic:
		if family.BoldItalic != nil {
			return fs.loadParsedFace(family.BoldItalic, opts)
		}
		if family.DisableSynthetic {
			return regular, nil
		}
		type boldItalicFallback struct {
			parsed      *opentype.Font
			synthBold   bool
			synthItalic bool
		}
		fallbacks := make([]boldItalicFallback, 0, 3)
		if family.Bold != nil {
			fallbacks = append(fallbacks, boldItalicFallback{parsed: family.Bold, synthBold: false, synthItalic: true})
		}
		if family.Italic != nil {
			fallbacks = append(fallbacks, boldItalicFallback{parsed: family.Italic, synthBold: true, synthItalic: false})
		}
		fallbacks = append(fallbacks, boldItalicFallback{synthBold: true, synthItalic: true})
		fb := fallbacks[0]
		base := regular
		if fb.parsed != nil {
			var loadErr error
			base, loadErr = fs.loadParsedFace(fb.parsed, opts)
			if loadErr != nil {
				return nil, loadErr
			}
		}
		return fs.loadSyntheticFace(base, fb.synthBold, fb.synthItalic, opts), nil
	}
	return regular, nil
}

func (fs *fontSet) loadSyntheticFace(base xfont.Face, bold, italic bool, opts *opentype.FaceOptions) xfont.Face {
	key := syntheticFaceKey{base: base, bold: bold, italic: italic}
	if f := fs.synthetics[key]; f != nil {
		return f
	}
	var face xfont.Face
	switch {
	case bold && italic:
		face = fonts.SyntheticBoldItalicFace(base, opts.Size)
	case bold:
		face = fonts.SyntheticBoldFace(base, opts.Size)
	case italic:
		face = fonts.SyntheticItalicFace(base)
	default:
		face = base
	}
	fs.synthetics[key] = face
	return face
}

// loadParsedFace creates one face per parsed font pointer.
//
// https://github.com/ghostty-org/ghostty/blob/main/src/font/Metrics.zig
func (fs *fontSet) loadParsedFace(parsed *opentype.Font, opts *opentype.FaceOptions) (xfont.Face, error) {
	if parsed == nil {
		return nil, errors.New("font must not be nil")
	}
	if f := fs.faces[parsed]; f != nil {
		return f, nil
	}
	face, err := fonts.NewFace(parsed, opts)
	if err != nil {
		return nil, err
	}
	fs.faces[parsed] = face
	return face, nil
}

// usesGridLayout reports whether face is one of the primary grid monospace variants.
func (fs *fontSet) usesGridLayout(face xfont.Face) bool {
	if fs == nil || face == nil {
		return false
	}
	_, ok := fs.gridFaces[face]
	return ok
}

// close releases every cached face implementing [io.Closer].
func (fs *fontSet) close() error {
	if fs == nil {
		return nil
	}
	var err error
	for _, face := range fs.faces {
		if closer, ok := face.(interface{ Close() error }); ok {
			err = errors.Join(err, closer.Close())
		}
	}
	return err
}

// faceForCell combines cell attributes and grapheme routing for glyphs.
func (fs *fontSet) faceForCell(cell *uv.Cell) xfont.Face {
	if fs == nil {
		return nil
	}
	return fs.faceForGlyph(cellStyle(cell), cellGlyphRune(cell))
}

// faceForGlyph resolves a face: codepoint map wins over style-derived primary faces.
func (fs *fontSet) faceForGlyph(style fontStyle, r rune) xfont.Face {
	if fs == nil {
		return nil
	}
	for _, cp := range fs.codepoints {
		if r >= cp.First && r <= cp.Last {
			return cp.faceForStyle(style)
		}
	}
	return fs.faceForStyle(style)
}

func (cp codepointRange) faceForStyle(style fontStyle) xfont.Face {
	if int(style) < len(cp.faces) && cp.faces[style] != nil {
		return cp.faces[style]
	}
	return cp.faces[fontStyleRegular]
}

// faceForStyle maps fontStyle back to bundled primary faces loaded in buildFontSet.
func (fs *fontSet) faceForStyle(style fontStyle) xfont.Face {
	switch style {
	case fontStyleRegular:
		return fs.regular
	case fontStyleBold:
		return fs.bold
	case fontStyleItalic:
		return fs.italic
	case fontStyleBoldItalic:
		return fs.boldItalic
	default:
		return fs.regular
	}
}

// cellStyle derives [fontStyle] from [uv.Cell.Style] and [uv.AttrBold] and
// [uv.AttrItalic].
func cellStyle(cell *uv.Cell) fontStyle {
	if cell == nil {
		return fontStyleRegular
	}
	bold := cell.Style.Attrs&uv.AttrBold != 0
	italic := cell.Style.Attrs&uv.AttrItalic != 0
	switch {
	case bold && italic:
		return fontStyleBoldItalic
	case bold:
		return fontStyleBold
	case italic:
		return fontStyleItalic
	default:
		return fontStyleRegular
	}
}

// cellGlyphRune returns the first rune of the cell content or ASCII space when
// empty/invalid, aligning single-codepoint rasterization until complex shaping
// exists (Ghostty configures run breaks separately; [ghostty-config],
// font-shaping-break).
//
// [ghostty-config]: https://ghostty.org/docs/config/reference
func cellGlyphRune(cell *uv.Cell) rune {
	if cell == nil || cell.Content == "" {
		return ' '
	}
	r, _ := utf8.DecodeRuneInString(cell.Content)
	if r == utf8.RuneError {
		return ' '
	}
	return r
}

// parseCodepointMap merges user mappings (U+ notation via codepoints.ParseRanges)
// with default symbol ranges using the same semantics as repeating
// font-codepoint-map in Ghostty: [ghostty-config].
//
// WithCodepointMap(nil) disables all mapping; a non-nil empty map retains defaults.
// Ghostty merges config-loaded maps with resolver/runtime state differently in Zig ([ghostty-cpr-zig]).
//
// [ghostty-config]: https://ghostty.org/docs/config/reference
// [ghostty-cpr-zig]: https://github.com/ghostty-org/ghostty/blob/main/src/font/CodepointResolver.zig
func parseCodepointMap(opts rendererOptions) ([]codepointRange, error) {
	if opts.codepointMapSet && opts.codepointMap == nil {
		return nil, nil
	}

	var user []codepointRange
	for spec, family := range opts.codepointMap {
		if err := validateFontFamily(fmt.Sprintf("codepoint map %q", spec), family); err != nil {
			return nil, err
		}
		parsed, err := codepoints.ParseRanges(spec)
		if err != nil {
			return nil, fmt.Errorf("codepoint map %q: %w", spec, err)
		}
		for _, r := range parsed {
			user = append(user, codepointRange{Range: r, family: family})
		}
	}
	if err := checkUserCodepointOverlaps(user); err != nil {
		return nil, err
	}

	symbolFamily, err := fonts.DefaultSymbolFamily()
	if err != nil {
		return nil, fmt.Errorf("load default symbol font family: %w", err)
	}
	if validateErr := validateFontFamily("default symbol", symbolFamily); validateErr != nil {
		return nil, validateErr
	}

	out := make([]codepointRange, 0, len(user)+len(defaultCodepointRanges))
	out = append(out, user...)
	for _, r := range defaultCodepointRanges {
		out = append(out, codepointRange{Range: r, family: symbolFamily})
	}
	return out, nil
}

// checkUserCodepointOverlaps enforces disjoint user ranges after sorting.
func checkUserCodepointOverlaps(ranges []codepointRange) error {
	slices.SortFunc(ranges, func(a, b codepointRange) int {
		return codepoints.Compare(a.Range, b.Range)
	})
	for i := 1; i < len(ranges); i++ {
		if ranges[i].First <= ranges[i-1].Last {
			return fmt.Errorf("overlapping user codepoint ranges %s-%s and %s-%s",
				codepoints.Format(ranges[i-1].First), codepoints.Format(ranges[i-1].Last),
				codepoints.Format(ranges[i].First), codepoints.Format(ranges[i].Last))
		}
	}
	return nil
}

// defaultCodepointRanges approximates bundled Nerd Font / symbol coverage layered
// under user entries; compare optional font stacks and nerd-font fallbacks
// documented with Ghostty's font-family and symbol guidance.
//
// https://ghostty.org/docs/config/reference
var defaultCodepointRanges = []codepoints.Range{
	{First: '\u2300', Last: '\u23ff'},
	{First: '\u25a0', Last: '\u25ff'},
	{First: '\u2600', Last: '\u27bf'},
	{First: '\u2b00', Last: '\u2bff'},
	{First: '\ue000', Last: '\uf8ff'},
	{First: '\U000f0000', Last: '\U000ffffd'},
	{First: '\U00100000', Last: '\U0010fffd'},
}
