// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package vt

import (
	"image/color"
	"unicode/utf8"
)

func styleColorToColor(sc ghosttyStyleColor) color.Color {
	switch sc.Tag {
	case ghosttyStyleColorRGB:
		return color.NRGBA{
			R: uint8(sc.Value),
			G: uint8(sc.Value >> 8),
			B: uint8(sc.Value >> 16),
			A: 0xff,
		}
	case ghosttyStyleColorPalette:
		// Without the live palette from the terminal, indexed colors are left unset.
		return nil
	default:
		return nil
	}
}

func underlineFromGhostty(u int32) Underline {
	switch u {
	case 0:
		return UnderlineNone
	case 1:
		return UnderlineSingle
	case 2:
		return UnderlineDouble
	case 3:
		return UnderlineCurly
	case 4:
		return UnderlineDotted
	case 5:
		return UnderlineDashed
	default:
		return UnderlineNone
	}
}

func styleFromGhostty(gs *ghosttyStyle) Style {
	var s Style
	s.Fg = styleColorToColor(gs.Fg)
	s.Bg = styleColorToColor(gs.Bg)
	s.UnderlineColor = styleColorToColor(gs.UnderlineColor)
	s.Underline = underlineFromGhostty(gs.Underline)
	if gs.Bold {
		s.Attrs |= AttrBold
	}
	if gs.Italic {
		s.Attrs |= AttrItalic
	}
	if gs.Faint {
		s.Attrs |= AttrFaint
	}
	if gs.Blink {
		s.Attrs |= AttrBlink
	}
	if gs.Inverse {
		s.Attrs |= AttrReverse
	}
	if gs.Invisible {
		s.Attrs |= AttrConceal
	}
	if gs.Strikethrough {
		s.Attrs |= AttrStrikethrough
	}
	_ = gs.Overline
	return s
}

func graphemesToString(codepoints []uint32) string {
	var buf [utf8.UTFMax]byte
	var b []byte
	for _, cp := range codepoints {
		if cp == 0 {
			continue
		}
		n := utf8.EncodeRune(buf[:], rune(cp))
		b = append(b, buf[:n]...)
	}
	if len(b) == 0 {
		return ""
	}
	return string(b)
}

func cellWidthFromGhostty(wide int32, graphemeWidth int) int {
	switch wide {
	case ghosttyCellWideWide:
		return 2
	case ghosttyCellWideSpacerTail, ghosttyCellWideSpacerHead:
		return 0
	default:
		if graphemeWidth > 0 {
			return graphemeWidth
		}
		return 1
	}
}
