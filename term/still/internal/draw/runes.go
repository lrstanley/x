// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package draw //nolint:revive // name mirrors image/draw responsibilities

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// FirstRune decodes the first rune of a cell's grapheme. ok is false for an
// empty string or invalid UTF-8; a literal U+FFFD is a valid rune.
func FirstRune(s string) (r rune, ok bool) {
	r, n := utf8.DecodeRuneInString(s)
	return r, r != utf8.RuneError || n > 1
}

// Cluster is the grapheme cluster of a cell, prepared for drawing: composed,
// and split into the rune that picks the face and the marks drawn over it.
type Cluster struct {
	// Text is the composed cluster. An empty cell is a space.
	Text string

	// Base is the first rune of Text.
	Base rune

	// Marks are the combining marks of Text drawn over Base. Anything else
	// after Base (joiners, variation selectors, further emoji of a ZWJ
	// sequence) has no glyph of its own and is not drawn.
	Marks string
}

// NewCluster prepares the content of a cell for drawing. ok is false when it
// has no first rune (invalid UTF-8).
func NewCluster(content string) (c Cluster, ok bool) {
	if content == "" {
		content = " "
	}
	c.Text = normalizeCluster(content)
	if c.Base, ok = FirstRune(c.Text); !ok {
		return Cluster{}, false
	}
	c.Marks = clusterMarks(c.Text)
	return c, true
}

// normalizeCluster composes a grapheme cluster (NFC), so a base letter and its
// combining accent draw as the precomposed rune when the font has one. Single
// rune clusters are returned as is.
func normalizeCluster(s string) string {
	if _, n := utf8.DecodeRuneInString(s); n >= len(s) {
		return s
	}
	return norm.NFC.String(s)
}

// maxClusterMarks bounds how many marks of one cluster are drawn, so stacked
// "zalgo" text cannot grow the face and mask caches without limit.
const maxClusterMarks = 4

// isClusterMark reports whether r is a mark drawn over the base rune of its
// cluster: a nonspacing or enclosing mark, but not a variation selector, which
// only picks a presentation and has no glyph of its own.
func isClusterMark(r rune) bool {
	return r >= 0x300 && !isVariationSelector(r) && unicode.In(r, unicode.Mn, unicode.Me)
}

func isVariationSelector(r rune) bool {
	return r >= 0x180b && r <= 0x180f || // Mongolian free variation selectors.
		r >= 0xfe00 && r <= 0xfe0f ||
		r >= 0xe0100 && r <= 0xe01ef
}

// clusterMarks returns up to maxClusterMarks of the runes of cluster, after
// its first, that are drawn over the base rune. Everything else after the base
// (joiners, variation selectors, further emoji of a ZWJ sequence) is left out.
// It returns "" for the common single rune cluster, and a substring of cluster
// when the marks directly follow the base, without allocating.
func clusterMarks(cluster string) string {
	_, n := utf8.DecodeRuneInString(cluster)
	rest := cluster[n:]
	end, count := 0, 0
	for end < len(rest) && count < maxClusterMarks {
		r, size := utf8.DecodeRuneInString(rest[end:])
		if !isClusterMark(r) {
			break
		}
		end += size
		count++
	}
	if count == maxClusterMarks || end == len(rest) {
		return rest[:end]
	}

	// A selector or joiner sits between marks: gather the marks after it.
	var b strings.Builder
	b.WriteString(rest[:end])
	for _, r := range rest[end:] {
		if count == maxClusterMarks {
			break
		}
		if isClusterMark(r) {
			b.WriteRune(r)
			count++
		}
	}
	return b.String()
}
