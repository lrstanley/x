// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package raster

// legacySpecFromRune returns the spec for the rectangular Symbols for Legacy
// Computing: block sextants, block octants, eighth blocks, and half-cell
// shades. Shades, including the "inverse" ones, are a uniform 50% fill like ▒.
func legacySpecFromRune(r rune) (blockSpec, bool) {
	switch {
	case r >= 0x1CD00 && r <= 0x1CDE5: // Block octants.
		return blockSpec{rows: 4, cells: octantPatterns[r-0x1CD00]}, true
	case r >= 0x1FB00 && r <= 0x1FB3B: // Block sextants.
		// Patterns run 1..62 in order, skipping 21 (▌) and 42 (▐).
		p := int(r-0x1FB00) + 1
		if p >= 21 {
			p++
		}
		if p >= 42 {
			p++
		}
		return blockSpec{rows: 3, cells: uint8(p)}, true
	case r >= 0x1FB70 && r <= 0x1FB75: // Vertical one eighth block-2..7.
		x := int(r - 0x1FB70 + 1)
		return rectSpec(solid(x, 0, x+1, 8)), true
	case r >= 0x1FB76 && r <= 0x1FB7B: // Horizontal one eighth block-2..7.
		y := int(r - 0x1FB76 + 1)
		return rectSpec(solid(0, y, 8, y+1)), true
	case r == 0x1FB7C: // Left and lower one eighth.
		return rectSpec(solid(0, 0, 1, 8), solid(0, 7, 8, 8)), true
	case r == 0x1FB7D: // Left and upper one eighth.
		return rectSpec(solid(0, 0, 1, 8), solid(0, 0, 8, 1)), true
	case r == 0x1FB7E: // Right and upper one eighth.
		return rectSpec(solid(7, 0, 8, 8), solid(0, 0, 8, 1)), true
	case r == 0x1FB7F: // Right and lower one eighth.
		return rectSpec(solid(7, 0, 8, 8), solid(0, 7, 8, 8)), true
	case r == 0x1FB80: // Upper and lower one eighth.
		return rectSpec(solid(0, 0, 8, 1), solid(0, 7, 8, 8)), true
	case r == 0x1FB81: // Horizontal one eighth block-1358.
		return rectSpec(solid(0, 0, 8, 1), solid(0, 2, 8, 3), solid(0, 4, 8, 5), solid(0, 7, 8, 8)), true
	case r >= 0x1FB82 && r <= 0x1FB86: // Upper 1/4, 3/8, 5/8, 3/4, 7/8.
		return rectSpec(solid(0, 0, 8, legacyEighths[r-0x1FB82])), true
	case r >= 0x1FB87 && r <= 0x1FB8B: // Right 1/4, 3/8, 5/8, 3/4, 7/8.
		return rectSpec(solid(8-legacyEighths[r-0x1FB87], 0, 8, 8)), true
	case r == 0x1FB8C: // Left half medium shade.
		return rectSpec(blockRect{x1: 4, y1: 8, alpha: shadeMedium}), true
	case r == 0x1FB8D: // Right half medium shade.
		return rectSpec(blockRect{x0: 4, x1: 8, y1: 8, alpha: shadeMedium}), true
	case r == 0x1FB8E: // Upper half medium shade.
		return rectSpec(blockRect{x1: 8, y1: 4, alpha: shadeMedium}), true
	case r == 0x1FB8F: // Lower half medium shade.
		return rectSpec(blockRect{y0: 4, x1: 8, y1: 8, alpha: shadeMedium}), true
	case r == 0x1FB90: // Inverse medium shade.
		return rectSpec(blockRect{x1: 8, y1: 8, alpha: shadeMedium}), true
	case r == 0x1FB91: // Upper half block and lower half inverse medium shade.
		return rectSpec(solid(0, 0, 8, 4), blockRect{y0: 4, x1: 8, y1: 8, alpha: shadeMedium}), true
	case r == 0x1FB92: // Upper half inverse medium shade and lower half block.
		return rectSpec(blockRect{x1: 8, y1: 4, alpha: shadeMedium}, solid(0, 4, 8, 8)), true
	case r == 0x1FB94: // Left half inverse medium shade and right half block.
		return rectSpec(blockRect{x1: 4, y1: 8, alpha: shadeMedium}, solid(4, 0, 8, 8)), true
	}
	return blockSpec{}, false
}

// legacyEighths is the extent, in eighths, of U+1FB82..U+1FB86 and
// U+1FB87..U+1FB8B.
var legacyEighths = [...]int{2, 3, 5, 6, 7}

// octantPatterns is indexed by rune - U+1CD00 and holds the filled cells of
// each block octant in row-major order (bit 0 top left, bit 7 bottom right).
// Generated from the "BLOCK OCTANT-n..." character names in Unicode 16.0
// UnicodeData.txt (https://www.unicode.org/Public/16.0.0/ucd/UnicodeData.txt).
// The 26 patterns missing here are encoded elsewhere (space, █, half blocks,
// quadrants, ▂▆🮂🮅, and a few single-cell quarter blocks).
var octantPatterns = [...]uint8{
	0x04, 0x06, 0x07, 0x08, 0x09, 0x0B, 0x0C, 0x0D, // U+1CD00
	0x0E, 0x10, 0x11, 0x12, 0x13, 0x15, 0x16, 0x17, // U+1CD08
	0x18, 0x19, 0x1A, 0x1B, 0x1C, 0x1D, 0x1E, 0x1F, // U+1CD10
	0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, // U+1CD18
	0x29, 0x2A, 0x2B, 0x2C, 0x2D, 0x2E, 0x2F, 0x30, // U+1CD20
	0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, // U+1CD28
	0x39, 0x3A, 0x3B, 0x3C, 0x3D, 0x3E, 0x41, 0x42, // U+1CD30
	0x43, 0x44, 0x45, 0x46, 0x47, 0x48, 0x49, 0x4A, // U+1CD38
	0x4B, 0x4C, 0x4D, 0x4E, 0x4F, 0x51, 0x52, 0x53, // U+1CD40
	0x54, 0x56, 0x57, 0x58, 0x59, 0x5B, 0x5C, 0x5D, // U+1CD48
	0x5E, 0x60, 0x61, 0x62, 0x63, 0x64, 0x65, 0x66, // U+1CD50
	0x67, 0x68, 0x69, 0x6A, 0x6B, 0x6C, 0x6D, 0x6E, // U+1CD58
	0x6F, 0x70, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, // U+1CD60
	0x77, 0x78, 0x79, 0x7A, 0x7B, 0x7C, 0x7D, 0x7E, // U+1CD68
	0x7F, 0x81, 0x82, 0x83, 0x84, 0x85, 0x86, 0x87, // U+1CD70
	0x88, 0x89, 0x8A, 0x8B, 0x8C, 0x8D, 0x8E, 0x8F, // U+1CD78
	0x90, 0x91, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97, // U+1CD80
	0x98, 0x99, 0x9A, 0x9B, 0x9C, 0x9D, 0x9E, 0x9F, // U+1CD88
	0xA1, 0xA2, 0xA3, 0xA4, 0xA6, 0xA7, 0xA8, 0xA9, // U+1CD90
	0xAB, 0xAC, 0xAD, 0xAE, 0xB0, 0xB1, 0xB2, 0xB3, // U+1CD98
	0xB4, 0xB5, 0xB6, 0xB7, 0xB8, 0xB9, 0xBA, 0xBB, // U+1CDA0
	0xBC, 0xBD, 0xBE, 0xBF, 0xC1, 0xC2, 0xC3, 0xC4, // U+1CDA8
	0xC5, 0xC6, 0xC7, 0xC8, 0xC9, 0xCA, 0xCB, 0xCC, // U+1CDB0
	0xCD, 0xCE, 0xCF, 0xD0, 0xD1, 0xD2, 0xD3, 0xD4, // U+1CDB8
	0xD5, 0xD6, 0xD7, 0xD8, 0xD9, 0xDA, 0xDB, 0xDC, // U+1CDC0
	0xDD, 0xDE, 0xDF, 0xE0, 0xE1, 0xE2, 0xE3, 0xE4, // U+1CDC8
	0xE5, 0xE6, 0xE7, 0xE8, 0xE9, 0xEA, 0xEB, 0xEC, // U+1CDD0
	0xED, 0xEE, 0xEF, 0xF1, 0xF2, 0xF3, 0xF4, 0xF6, // U+1CDD8
	0xF7, 0xF8, 0xF9, 0xFB, 0xFD, 0xFE, // U+1CDE0
}
