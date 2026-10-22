// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fontset

import (
	"github.com/lrstanley/x/term/still/fonts"
	xfont "golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

// resolve picks the face that draws r: the first face in the chain whose font
// has the glyph, else an installed font. When none has it, the primary face is
// returned and draws nothing.
func (fs *Set) resolve(style fontStyle, r rune) xfont.Face {
	if face := fs.resolveChain(style, r); face != nil {
		return face
	}
	if face := fs.resolveSystem(style, r); face != nil {
		return face
	}
	return fs.faceForStyle(style)
}

// resolveChain returns the face of the first chain entry covering r whose
// font has a glyph for it. Codepoint map ranges keep precedence, as in
// Ghostty's font-codepoint-map, but a range whose font lacks the glyph falls
// through instead of drawing .notdef.
func (fs *Set) resolveChain(style fontStyle, r rune) xfont.Face {
	for _, cp := range fs.chain {
		if r < cp.First || r > cp.Last {
			continue
		}
		if src, _, _ := styleSource(cp.family, style); hasGlyph(src, r) {
			return cp.faces[style]
		}
	}
	return nil
}

func (fs *Set) resolveSystem(style fontStyle, r rune) xfont.Face {
	if fs.systemFallback == nil {
		return nil
	}
	font, ok := fs.systemFallback(r)
	if !ok {
		return nil
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	face, err := fs.loadFamilyFace(fonts.FontFamily{Regular: font}, style)
	if err != nil {
		return nil
	}
	return face
}

// hasGlyph reports whether f maps r to a glyph other than .notdef.
func hasGlyph(f *opentype.Font, r rune) bool {
	if f == nil {
		return false
	}
	idx, err := f.GlyphIndex(nil, r)
	return err == nil && idx != 0
}
