// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package vt

import (
	"image/color"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
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

func underlineFromGhostty(u int32) ansi.Underline {
	switch u {
	case 0:
		return ansi.UnderlineNone
	case 1:
		return ansi.UnderlineSingle
	case 2:
		return ansi.UnderlineDouble
	case 3:
		return ansi.UnderlineCurly
	case 4:
		return ansi.UnderlineDotted
	case 5:
		return ansi.UnderlineDashed
	default:
		return ansi.UnderlineNone
	}
}

func styleFromGhostty(gs *ghosttyStyle) uv.Style {
	var s uv.Style
	s.Fg = styleColorToColor(gs.Fg)
	s.Bg = styleColorToColor(gs.Bg)
	s.UnderlineColor = styleColorToColor(gs.UnderlineColor)
	s.Underline = underlineFromGhostty(gs.Underline)
	if gs.Bold {
		s.Attrs |= uv.AttrBold
	}
	if gs.Italic {
		s.Attrs |= uv.AttrItalic
	}
	if gs.Faint {
		s.Attrs |= uv.AttrFaint
	}
	if gs.Blink {
		s.Attrs |= uv.AttrBlink
	}
	if gs.Inverse {
		s.Attrs |= uv.AttrReverse
	}
	if gs.Invisible {
		s.Attrs |= uv.AttrConceal
	}
	if gs.Strikethrough {
		s.Attrs |= uv.AttrStrikethrough
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
