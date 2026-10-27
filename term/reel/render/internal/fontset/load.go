// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fontset

import (
	"errors"
	"fmt"

	"github.com/lrstanley/x/term/reel/render/fonts"
	"github.com/lrstanley/x/term/reel/render/internal/config"
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

// styleSource returns the parsed font that draws style in family, and the
// synthetic transforms to apply when the family has no real face for it.
func styleSource(family fonts.FontFamily, style fontStyle) (src *opentype.Font, bold, italic bool) {
	switch {
	case style == fontStyleRegular:
		return family.Regular, false, false
	case style == fontStyleBold && family.Bold != nil:
		return family.Bold, false, false
	case style == fontStyleItalic && family.Italic != nil:
		return family.Italic, false, false
	case style == fontStyleBoldItalic && family.BoldItalic != nil:
		return family.BoldItalic, false, false
	case family.DisableSynthetic:
		return family.Regular, false, false
	case style == fontStyleBoldItalic && family.Bold != nil:
		return family.Bold, false, true
	case style == fontStyleBoldItalic && family.Italic != nil:
		return family.Italic, true, false
	}
	return family.Regular, style == fontStyleBold || style == fontStyleBoldItalic, style == fontStyleItalic || style == fontStyleBoldItalic
}

func (fs *Set) loadFamilyFace(family fonts.FontFamily, style fontStyle) (xfont.Face, error) {
	if err := validateFontFamily("font", family); err != nil {
		return nil, err
	}
	src, bold, italic := styleSource(family, style)
	base, err := fs.loadParsedFace(src)
	if err != nil || (!bold && !italic) {
		return base, err
	}
	key := syntheticFaceKey{base: base, bold: bold, italic: italic}
	if f := fs.synthetics[key]; f != nil {
		return f, nil
	}
	face := fonts.SyntheticFace(base, bold, italic, fs.faceOpts.Size)
	fs.synthetics[key] = face
	return face, nil
}

func (fs *Set) loadParsedFace(parsed *opentype.Font) (xfont.Face, error) {
	if parsed == nil {
		return nil, errors.New("font must not be nil")
	}
	if f := fs.faces[parsed]; f != nil {
		return f, nil
	}
	face, err := fonts.NewFace(parsed, fs.faceOpts)
	if err != nil {
		return nil, err
	}
	fs.faces[parsed] = face
	return face, nil
}
