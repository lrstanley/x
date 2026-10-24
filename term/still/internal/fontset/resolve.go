// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fontset

import (
	"github.com/lrstanley/x/term/still/fonts"
	xfont "golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

// resolve picks the face that draws r with the combining marks over it. A
// face must cover the marks too; when none does, r is drawn by the face that
// covers it alone, and the draw skips the marks that face lacks. When no face
// has r, the primary face is returned and draws nothing.
func (fs *Set) resolve(style fontStyle, r rune, marks string) xfont.Face {
	if face := fs.resolveCovering(style, r, marks); face != nil {
		return face
	}
	if marks != "" {
		if face := fs.resolveCovering(style, r, ""); face != nil {
			return face
		}
	}
	return fs.faceForStyle(style)
}

// resolveCovering returns the face of the first chain font that has glyphs
// for r and marks, else an installed font that does.
func (fs *Set) resolveCovering(style fontStyle, r rune, marks string) xfont.Face {
	if face := fs.resolveChain(style, r, marks); face != nil {
		return face
	}
	return fs.resolveSystem(style, r, marks)
}

// resolveChain returns the face of the first chain entry covering r whose
// font has a glyph for it. Codepoint map ranges keep precedence, as in
// Ghostty's font-codepoint-map, but a range whose font lacks the glyph falls
// through instead of drawing .notdef. The font must have the marks too.
func (fs *Set) resolveChain(style fontStyle, r rune, marks string) xfont.Face {
	for _, cp := range fs.chain {
		if r < cp.First || r > cp.Last {
			continue
		}
		if src, _, _ := styleSource(cp.family, style); hasCluster(src, r, marks) {
			return cp.faces[style]
		}
	}
	return nil
}

func (fs *Set) resolveSystem(style fontStyle, r rune, marks string) xfont.Face {
	if fs.systemFallback == nil {
		return nil
	}
	font, ok := fs.systemFallback(r)
	if !ok || !hasCluster(font, r, marks) {
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

// hasCluster reports whether f has glyphs for r and every rune of marks.
func hasCluster(f *opentype.Font, r rune, marks string) bool {
	if !hasGlyph(f, r) {
		return false
	}
	for _, m := range marks {
		if !hasGlyph(f, m) {
			return false
		}
	}
	return true
}
