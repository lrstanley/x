// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package fontset builds and caches [golang.org/x/image/font.Face] sets from
// configured families, including per-codepoint overrides and synthetic styles.
package fontset

import (
	"errors"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/charm/still/internal/config"
	"github.com/lrstanley/x/charm/still/internal/metrics"
	xfont "golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

type fontStyle uint8

const (
	fontStyleRegular fontStyle = iota
	fontStyleBold
	fontStyleItalic
	fontStyleBoldItalic
)

// Set holds concurrently reusable [xfont.Face] values for primary styles and
// optional per-range overrides.
type Set struct {
	regular    xfont.Face
	bold       xfont.Face
	italic     xfont.Face
	boldItalic xfont.Face

	gridMetrics metrics.FaceMetrics
	gridFaces   map[xfont.Face]struct{}

	faces      map[*opentype.Font]xfont.Face
	synthetics map[syntheticFaceKey]xfont.Face
	codepoints []codepointRange
}

type syntheticFaceKey struct {
	base   xfont.Face
	bold   bool
	italic bool
}

// Build loads primary style faces plus codepoint map faces.
func Build(opts config.Options) (*Set, error) {
	faceOpts := &opentype.FaceOptions{
		Size:    float64(opts.FontSize),
		DPI:     float64(opts.DPI),
		Hinting: xfont.HintingFull,
	}
	fs := &Set{
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
	fs.gridMetrics = metrics.MeasureFace(fs.regular)
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

// Regular returns the primary regular grid face.
func (fs *Set) Regular() xfont.Face {
	if fs == nil {
		return nil
	}
	return fs.regular
}

// GridMetrics returns face metrics measured from the regular grid face.
func (fs *Set) GridMetrics() metrics.FaceMetrics {
	if fs == nil {
		return metrics.FaceMetrics{}
	}
	return fs.gridMetrics
}

// UsesGridLayout reports whether face is a primary grid monospace variant.
func (fs *Set) UsesGridLayout(face xfont.Face) bool {
	if fs == nil || face == nil {
		return false
	}
	_, ok := fs.gridFaces[face]
	return ok
}

// Bold returns the primary bold grid face.
func (fs *Set) Bold() xfont.Face {
	if fs == nil {
		return nil
	}
	return fs.bold
}

// Italic returns the primary italic grid face.
func (fs *Set) Italic() xfont.Face {
	if fs == nil {
		return nil
	}
	return fs.italic
}

// BoldItalic returns the primary bold-italic grid face.
func (fs *Set) BoldItalic() xfont.Face {
	if fs == nil {
		return nil
	}
	return fs.boldItalic
}

// Close releases every cached face implementing [io.Closer].
func (fs *Set) Close() error {
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

// FaceForCell combines cell attributes and grapheme routing for glyphs.
func (fs *Set) FaceForCell(cell *uv.Cell) xfont.Face {
	if fs == nil {
		return nil
	}
	return fs.faceForGlyph(cellStyle(cell), cellGlyphRune(cell))
}

func (fs *Set) faceForGlyph(style fontStyle, r rune) xfont.Face {
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

func (fs *Set) faceForStyle(style fontStyle) xfont.Face {
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
