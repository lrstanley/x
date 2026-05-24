// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fontset

import (
	"fmt"
	"slices"

	"github.com/lrstanley/x/charm/still/fonts"
	"github.com/lrstanley/x/charm/still/internal/codepoints"
	"github.com/lrstanley/x/charm/still/internal/config"
	xfont "golang.org/x/image/font"
)

type codepointRange struct {
	codepoints.Range
	family fonts.FontFamily
	faces  [4]xfont.Face
}

func (cp codepointRange) faceForStyle(style fontStyle) xfont.Face {
	if int(style) < len(cp.faces) && cp.faces[style] != nil {
		return cp.faces[style]
	}
	return cp.faces[fontStyleRegular]
}

func parseCodepointMap(opts config.Options) ([]codepointRange, error) {
	if opts.CodepointMapSet && opts.CodepointMap == nil {
		return nil, nil
	}

	var user []codepointRange
	for spec, family := range opts.CodepointMap {
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

var defaultCodepointRanges = []codepoints.Range{
	{First: '\u2300', Last: '\u23ff'},         // Miscellaneous Technical.
	{First: '\u25a0', Last: '\u25ff'},         // Geometric Shapes.
	{First: '\u2600', Last: '\u27bf'},         // Miscellaneous Symbols and Dingbats.
	{First: '\u2b00', Last: '\u2bff'},         // Miscellaneous Symbols and Arrows.
	{First: '\ue000', Last: '\uf8ff'},         // Private Use Area (Nerd Fonts, Powerline).
	{First: '\U000f0000', Last: '\U000ffffd'}, // Supplementary Private Use Area-A.
	{First: '\U00100000', Last: '\U0010fffd'}, // Supplementary Private Use Area-B.
}
