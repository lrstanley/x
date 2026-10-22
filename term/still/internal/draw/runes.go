// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package draw //nolint:revive // name mirrors image/draw responsibilities

import "unicode/utf8"

// FirstRune decodes the first rune of a cell's grapheme. ok is false for an
// empty string or invalid UTF-8; a literal U+FFFD is a valid rune.
func FirstRune(s string) (r rune, ok bool) {
	r, n := utf8.DecodeRuneInString(s)
	return r, r != utf8.RuneError || n > 1
}
