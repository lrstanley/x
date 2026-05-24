// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fontset

import (
	"errors"
	"fmt"

	"github.com/lrstanley/x/charm/still/fonts"
	"github.com/lrstanley/x/charm/still/internal/config"
	xfont "golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

func configuredFontFamily(opts config.Options) (fonts.FontFamily, error) {
	if opts.FontFamilySet {
		if err := validateFontFamily("primary", opts.FontFamily); err != nil {
			return fonts.FontFamily{}, err
		}
		return opts.FontFamily, nil
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

func (fs *Set) loadFamilyFace(family fonts.FontFamily, style fontStyle, opts *opentype.FaceOptions) (xfont.Face, error) {
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

func (fs *Set) loadSyntheticFace(base xfont.Face, bold, italic bool, opts *opentype.FaceOptions) xfont.Face {
	key := syntheticFaceKey{base: base, bold: bold, italic: italic}
	if f := fs.synthetics[key]; f != nil {
		return f
	}
	face := fonts.SyntheticFace(base, bold, italic, opts.Size)
	fs.synthetics[key] = face
	return face
}

func (fs *Set) loadParsedFace(parsed *opentype.Font, opts *opentype.FaceOptions) (xfont.Face, error) {
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
