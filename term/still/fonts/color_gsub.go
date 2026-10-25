// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fonts

import (
	"slices"
	"sort"
)

// gsubFeatures are the features that compose emoji sequences (flags, skin
// tones, ZWJ families, keycaps) into one glyph. Emoji fonts don't vary them by
// script or language, so every script's lookups for these features apply.
var gsubFeatures = [...]string{"ccmp", "rlig", "liga", "clig"}

// gsub is the part of a GSUB table needed to compose an emoji sequence: single
// and ligature substitutions, directly or through an extension lookup.
// Contextual lookups are not applied. Noto uses them only to clean up malformed
// sequences, so a well formed cluster is composed without them.
type gsub struct {
	tbl     be
	lookups []int // Offsets of the lookups to apply, in lookup order.
}

func parseGSUB(tbl be) *gsub {
	g := &gsub{tbl: tbl}
	features, lookupList := tbl.u16(6), tbl.u16(8)
	if features == 0 || lookupList == 0 {
		return g
	}

	var indexes []int
	for i := range tbl.u16(features) {
		rec := features + 2 + 6*i
		if !tbl.has(rec, 6) || !slices.Contains(gsubFeatures[:], string(tbl[rec:rec+4])) {
			continue
		}
		feature := features + tbl.u16(rec+4)
		for j := range tbl.u16(feature + 2) {
			indexes = append(indexes, tbl.u16(feature+4+2*j))
		}
	}
	slices.Sort(indexes)
	indexes = slices.Compact(indexes)

	count := tbl.u16(lookupList)
	for _, idx := range indexes {
		if idx < count {
			g.lookups = append(g.lookups, lookupList+tbl.u16(lookupList+2+2*idx))
		}
	}
	return g
}

// shape composes sequences in gids, returning the resulting glyphs.
func (g *gsub) shape(gids []uint16) []uint16 {
	for _, lookup := range g.lookups {
		gids = g.apply(lookup, gids)
	}
	return gids
}

func (g *gsub) apply(lookup int, gids []uint16) []uint16 {
	typ, subtables := g.tbl.u16(lookup), g.tbl.u16(lookup+4)
	for pos := 0; pos < len(gids); pos++ {
		for i := range subtables {
			st, kind := lookup+g.tbl.u16(lookup+6+2*i), typ
			if kind == 7 { // Extension: the real subtable is elsewhere.
				kind = g.tbl.u16(st + 2)
				st += g.tbl.u32(st + 4)
			}
			var n int // Glyphs replaced at pos; 0 when the subtable doesn't apply.
			switch kind {
			case 1:
				n = g.single(st, gids, pos)
			case 4:
				n = g.ligature(st, gids, pos)
			}
			if n > 0 {
				gids = slices.Delete(gids, pos+1, pos+n)
				break
			}
		}
	}
	return gids
}

// single applies a single substitution to gids[pos].
func (g *gsub) single(st int, gids []uint16, pos int) int {
	idx := g.coverageIndex(st+g.tbl.u16(st+2), gids[pos])
	if idx < 0 {
		return 0
	}
	switch g.tbl.u16(st) {
	case 1:
		gids[pos] = uint16((int(gids[pos]) + g.tbl.i16(st+4)) & 0xFFFF) //nolint:gosec // Masked to 16 bits.
	case 2:
		if idx >= g.tbl.u16(st+4) {
			return 0
		}
		gids[pos] = uint16(g.tbl.u16(st + 6 + 2*idx)) //nolint:gosec // 16 bits read.
	default:
		return 0
	}
	return 1
}

// ligature replaces gids[pos:] by the first ligature that matches there. It
// returns how many glyphs the ligature took.
func (g *gsub) ligature(st int, gids []uint16, pos int) int {
	idx := g.coverageIndex(st+g.tbl.u16(st+2), gids[pos])
	if idx < 0 || g.tbl.u16(st) != 1 || idx >= g.tbl.u16(st+4) {
		return 0
	}
	set := st + g.tbl.u16(st+6+2*idx)
	for i := range g.tbl.u16(set) {
		lig := set + g.tbl.u16(set+2+2*i)
		n := g.tbl.u16(lig + 2) // Components, including the first.
		if n < 2 || pos+n > len(gids) {
			continue
		}
		match := true
		for k := 1; k < n && match; k++ {
			match = g.tbl.u16(lig+4+2*(k-1)) == int(gids[pos+k])
		}
		if match {
			gids[pos] = uint16(g.tbl.u16(lig)) //nolint:gosec // 16 bits read.
			return n
		}
	}
	return 0
}

// coverageIndex returns the position of glyph in the coverage table at off, or
// -1 when it isn't covered.
func (g *gsub) coverageIndex(off int, glyph uint16) int {
	t := g.tbl
	switch t.u16(off) {
	case 1:
		n := min(t.u16(off+2), (len(t)-off-4)/2)
		i := sort.Search(n, func(i int) bool { return t.u16(off+4+2*i) >= int(glyph) })
		if i < n && t.u16(off+4+2*i) == int(glyph) {
			return i
		}
	case 2:
		n := min(t.u16(off+2), (len(t)-off-4)/6)
		i := sort.Search(n, func(i int) bool { return t.u16(off+4+6*i+2) >= int(glyph) })
		if i < n && t.u16(off+4+6*i) <= int(glyph) {
			return t.u16(off+4+6*i+4) + int(glyph) - t.u16(off+4+6*i)
		}
	}
	return -1
}
