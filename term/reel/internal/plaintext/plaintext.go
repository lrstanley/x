// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package plaintext makes untrusted text, such as what a program sends for
// a title or notification, safe to draw.
package plaintext

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Line makes untrusted text safe to draw as one plain line, such as a
// window title. Controls (C0 and C1, which covers ESC, so no escape sequence
// survives) and white space, which includes line and paragraph separators,
// become spaces, and runs of them collapse, with none at either end. Format
// characters (Unicode Cf: every bidi embedding, override, and isolate, and
// zero-width characters, the soft hyphen, and the byte order mark), other
// default ignorable code points, noncharacters, and invalid UTF-8 are
// dropped. Nothing is read as markup, and cells are laid out left to right
// in the order received, so "\u202egnp.exe" reads "gnp.exe", not "exe.png".
// At most maxRunes runes are read. Text that needs no change is returned as
// is.
func Line(s string, maxRunes int) string {
	var (
		b     strings.Builder
		built bool // b holds the text so far; until then it is s[:i]
		space bool // a space is owed before the next rune kept
		runes int
	)
	for i, r := range s {
		runes++
		if runes > maxRunes {
			if !built {
				return strings.TrimSuffix(s[:i], " ")
			}
			break
		}
		blank, drop := r == ' ', false
		if r < ' ' || r > '~' { // printable ASCII is kept as is
			blank = unicode.IsControl(r) || unicode.IsSpace(r)
			drop = !blank && (r == utf8.RuneError ||
				unicode.In(r, unicode.Cf, unicode.Other_Default_Ignorable_Code_Point, unicode.Noncharacter_Code_Point))
		}
		if !built {
			switch {
			case !blank && !drop:
				space = false
				continue
			case r == ' ' && i > 0 && !space:
				space = true // one space between words is kept as is
				continue
			}
			// The first rune that changes: copy the text before it, less
			// the space it owes.
			built = true
			b.Grow(len(s))
			b.WriteString(strings.TrimSuffix(s[:i], " "))
		}
		switch {
		case blank:
			space = b.Len() > 0
		case drop:
		default:
			if space {
				b.WriteByte(' ')
				space = false
			}
			b.WriteRune(r)
		}
	}
	if !built {
		return strings.TrimSuffix(s, " ")
	}
	return b.String()
}
