// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package draw //nolint:revive // name mirrors image/draw responsibilities

// IsSymbolRune reports whether r is symbol-like: a private use area codepoint
// (where Nerd Font icons live), or one of the symbol blocks that fall back to
// icon fonts. This is Ghostty's definition, which decides which glyphs may grow
// into the cell after them.
func IsSymbolRune(r rune) bool {
	switch {
	case r >= 0xE000 && r <= 0xF8FF, // Private Use Area
		r >= 0xF0000 && r <= 0xFFFFD,   // Supplementary Private Use Area-A
		r >= 0x100000 && r <= 0x10FFFD, // Supplementary Private Use Area-B
		r >= 0x2190 && r <= 0x21FF,     // Arrows
		r >= 0x2460 && r <= 0x24FF,     // Enclosed Alphanumerics
		r >= 0x2600 && r <= 0x26FF,     // Miscellaneous Symbols
		r >= 0x2700 && r <= 0x27BF,     // Dingbats
		r >= 0x1F100 && r <= 0x1F1FF,   // Enclosed Alphanumeric Supplement
		r >= 0x1F300 && r <= 0x1F5FF,   // Miscellaneous Symbols and Pictographs
		r >= 0x1F600 && r <= 0x1F64F,   // Emoticons
		r >= 0x1F680 && r <= 0x1F6FF:   // Transport and Map Symbols
		return true
	}
	return false
}

// nerdIconRanges are the codepoints of the bundled Nerd Fonts symbol font that
// Ghostty fits to a cell and lets grow to two: Pomicons, Font Awesome and its
// extension, Weather, Seti-UI, Devicons, Codicons, Font Logos, Octicons, and
// Material Design. Powerline, the trigram, and the heavy angle brackets are
// left out because Ghostty never gives them more than one cell, as are the
// IEC power symbols, which are not symbol-like ([IsSymbolRune]) there.
var nerdIconRanges = [...][2]rune{
	{0x2665, 0x2665}, // Octicons heart.
	{0x26A1, 0x26A1}, // Octicons zap.
	{0xE000, 0xE00A},
	{0xE200, 0xE2A9},
	{0xE300, 0xE3E3},
	{0xE5FA, 0xE6B8},
	{0xE700, 0xE8EF},
	{0xEA60, 0xEA88},
	{0xEA8A, 0xEA8C},
	{0xEA8F, 0xEAC7},
	{0xEAC9, 0xEAC9},
	{0xEACC, 0xEB09},
	{0xEB0B, 0xEB4E},
	{0xEB50, 0xEC1E},
	{0xED00, 0xEFCE},
	{0xF000, 0xF381},
	{0xF400, 0xF533},
	{0xF0001, 0xF1AF0},
}

// IsNerdIconRune reports whether r is a Nerd Font icon that may be fitted to
// two cells, as opposed to any other symbol, which keeps its natural size.
func IsNerdIconRune(r rune) bool {
	if r < 0x2665 {
		return false
	}
	for _, rg := range nerdIconRanges {
		if r >= rg[0] && r <= rg[1] {
			return true
		}
	}
	return false
}

// IsGraphicsElement reports whether r is drawn as terminal graphics (box
// drawing, block elements, legacy computing, or Powerline) rather than as an
// icon.
func IsGraphicsElement(r rune) bool {
	return r >= 0x2500 && r <= 0x259F ||
		r >= 0x1FB00 && r <= 0x1FBFF ||
		r >= 0x1CC00 && r <= 0x1CEBF ||
		r >= 0xE0B0 && r <= 0xE0D7
}
