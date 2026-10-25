// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fonts

import (
	"bytes"
	"slices"
	"sync"
)

// bundledColorFile is the embedded color emoji font: a subset of Noto Color
// Emoji, see README.md. It is not a family fonts can be looked up by, so the
// family index skips it.
const bundledColorFile = "NotoColorEmoji-Subset.ttf.gz"

var bundledColorGlyphs colorGlyphCache

// BundledColorGlyph returns the glyph of the bundled color emoji font for a
// grapheme cluster, under the same rules as [SystemColorGlyph]. The font has
// emoji, flags, keycaps and ZWJ sequences, but no skin tone variants: a
// sequence with a skin tone is drawn in the default (yellow) skin tone instead,
// which beats drawing nothing. The font is decompressed on first use.
func BundledColorGlyph(cluster string) (*ColorGlyph, bool) {
	return lookupColorCached(&bundledColorGlyphs, findBundledColor, cluster)
}

func findBundledColor(cluster []rune) *ColorGlyph {
	if g, ok := lookupColor(bundledColorIndex(), cluster); ok {
		return g
	}
	if !slices.ContainsFunc(cluster, isSkinTone) {
		return nil
	}
	g, _ := lookupColor(bundledColorIndex(), slices.DeleteFunc(slices.Clone(cluster), isSkinTone))
	return g
}

// isSkinTone reports whether r is an emoji skin tone modifier.
func isSkinTone(r rune) bool { return r >= 0x1F3FB && r <= 0x1F3FF }

// bundledColorIndex holds the bundled face, already opened. The font bytes stay
// in memory for as long as its glyphs are drawn from.
var bundledColorIndex = sync.OnceValue(func() []*colorFace {
	const src = "embed:data/" + bundledColorFile
	raw, err := readSource(src)
	if err != nil {
		return nil
	}
	r := bytes.NewReader(raw)
	var out []*colorFace
	for _, f := range colorFaces(r, src) {
		of := openColorReader(r, f.index)
		out = append(out, &colorFace{face: f, open: func() *openColorFace { return of }})
	}
	return out
})
