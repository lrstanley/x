// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package fontset builds and caches [golang.org/x/image/font.Face] sets from
// configured families, including per-codepoint overrides and synthetic styles.
package fontset

import (
	"errors"
	"fmt"
	"sync"
	"unicode"

	"github.com/lrstanley/x/term/still/fonts"
	"github.com/lrstanley/x/term/still/internal/codepoints"
	"github.com/lrstanley/x/term/still/internal/config"
	idraw "github.com/lrstanley/x/term/still/internal/draw"
	"github.com/lrstanley/x/term/still/internal/metrics"
	"github.com/lrstanley/x/term/vt/screen"
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
// optional per-range overrides, and resolves which face draws each rune.
type Set struct {
	regular    xfont.Face
	bold       xfont.Face
	italic     xfont.Face
	boldItalic xfont.Face

	gridMetrics metrics.FaceMetrics
	gridFaces   map[xfont.Face]struct{}

	faceOpts *opentype.FaceOptions

	// chain lists the faces tried for each rune, in order: codepoint map
	// ranges, then the primary family and the bundled symbol fallback, which
	// both cover every rune. The first whose font has the glyph draws it.
	chain []codepointRange

	// systemFallback finds an installed font for runes the chain misses. It is
	// nil when system fonts are disabled.
	systemFallback func(rune) (*opentype.Font, bool)

	// ascii holds the resolved face for printable ASCII, filled by Build and
	// read without locking.
	ascii [4][0x7f]xfont.Face

	// mu guards the maps below. faces and synthetics only grow after Build
	// when a system font is first used.
	mu         sync.RWMutex
	faces      map[*opentype.Font]xfont.Face
	synthetics map[syntheticFaceKey]xfont.Face
	resolved   map[resolveKey]xfont.Face
}

type resolveKey struct {
	style fontStyle
	r     rune

	// marks holds the combining marks drawn over r, so a face is picked for the
	// whole cluster. It is empty for single rune cells.
	marks string
}

type syntheticFaceKey struct {
	base   xfont.Face
	bold   bool
	italic bool
}

// Build loads primary style faces plus codepoint map faces.
func Build(opts config.Options) (*Set, error) {
	var system func(rune) (*opentype.Font, bool)
	if opts.SystemFontFallback {
		system = fonts.SystemFallback
	}
	return build(opts, system)
}

func build(opts config.Options, system func(rune) (*opentype.Font, bool)) (*Set, error) {
	fs := &Set{
		faceOpts: &opentype.FaceOptions{
			Size:    opts.FontSizePx(),
			DPI:     72, // 1pt per px, so Size is in pixels.
			Hinting: xfont.HintingFull,
		},
		systemFallback: system,
		faces:          map[*opentype.Font]xfont.Face{},
		synthetics:     map[syntheticFaceKey]xfont.Face{},
		gridFaces:      map[xfont.Face]struct{}{},
		resolved:       map[resolveKey]xfont.Face{},
	}

	primaryFamily, err := configuredFontFamily(opts)
	if err != nil {
		return nil, err
	}
	fallbackFamily, err := fonts.DefaultFallbackFamily()
	if err != nil {
		return nil, fmt.Errorf("load default fallback font family: %w", err)
	}
	chain, err := parseCodepointMap(opts)
	if err != nil {
		return nil, err
	}
	everyRune := codepoints.Range{First: 0, Last: unicode.MaxRune}
	primaryAt := len(chain)
	chain = append(chain,
		codepointRange{Range: everyRune, family: primaryFamily},
		codepointRange{Range: everyRune, family: fallbackFamily},
	)
	for i := range chain {
		for style := range chain[i].faces {
			if chain[i].faces[style], err = fs.loadFamilyFace(chain[i].family, fontStyle(style)); err != nil {
				return nil, err
			}
		}
	}
	fs.chain = chain

	primary := chain[primaryAt].faces
	fs.regular, fs.bold, fs.italic, fs.boldItalic = primary[0], primary[1], primary[2], primary[3]
	fs.gridMetrics = metrics.MeasureFace(fs.regular)
	for _, face := range primary {
		fs.gridFaces[face] = struct{}{}
	}

	for style := range fs.ascii {
		for r := rune(' '); r < 0x7f; r++ {
			fs.ascii[style][r] = fs.resolveChain(fontStyle(style), r, "")
		}
	}
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

// FaceForCell returns the face that draws cluster, the cluster of cell, in the
// style of cell.
func (fs *Set) FaceForCell(cell *screen.Cell, cluster idraw.Cluster) xfont.Face {
	if fs == nil {
		return nil
	}
	return fs.faceForGlyph(cellStyle(cell), cluster.Base, cluster.Marks)
}

// faceForGlyph returns the face that draws r with the combining marks over it.
func (fs *Set) faceForGlyph(style fontStyle, r rune, marks string) xfont.Face {
	if fs == nil {
		return nil
	}
	if marks == "" && r >= 0 && r < 0x7f {
		if face := fs.ascii[style][r]; face != nil {
			return face
		}
	}

	key := resolveKey{style: style, r: r, marks: marks}
	fs.mu.RLock()
	face, ok := fs.resolved[key]
	fs.mu.RUnlock()
	if ok {
		return face
	}

	// Resolve without holding the lock: the first system fallback reads every
	// installed font. Concurrent misses for the same rune resolve the same face.
	face = fs.resolve(style, r, marks)
	fs.mu.Lock()
	fs.resolved[key] = face
	fs.mu.Unlock()
	return face
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

func cellStyle(cell *screen.Cell) fontStyle {
	if cell == nil {
		return fontStyleRegular
	}
	bold := cell.Style.Attrs&screen.AttrBold != 0
	italic := cell.Style.Attrs&screen.AttrItalic != 0
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
