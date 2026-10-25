// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fonts

import (
	"slices"
	"sort"
)

// Apple's emoji font composes flags, skin tones and ZWJ sequences with an AAT
// extended glyph metamorphosis table (morx) rather than GSUB.

const (
	morxVertical     = 0x80000000 // Applies to vertical text only, unless all directions is set.
	morxBackwards    = 0x40000000 // Glyphs are processed last to first.
	morxAllDirection = 0x20000000

	morxNoncontextual = 4
	morxContextual    = 1
	morxLigature      = 2

	morxDeleted = 0xFFFF // The glyph of a deleted position, removed after shaping.

	// State machine classes with a fixed meaning.
	morxClassEndOfText   = 0
	morxClassOutOfBounds = 1
	morxClassDeleted     = 2

	// Entry flags.
	morxSetMark      = 0x8000 // Contextual: remember this glyph. Ligature: push it as a component.
	morxDontAdvance  = 0x4000
	morxPerformLigat = 0x2000

	// Ligature actions.
	morxLigLast   = 0x80000000
	morxLigStore  = 0x40000000
	morxLigOffset = 0x3FFFFFFF

	morxMaxStack = 64 // Components a ligature can span, as in other shapers.
)

// morx is the part of a morx table needed to compose an emoji sequence:
// noncontextual, contextual and ligature subtables of the default features, as
// applied to left to right horizontal text. Rearrangement and insertion
// subtables are not applied; Apple's emoji font leaves them off by default.
type morx struct {
	tbl       be
	numGlyphs int
	subtables []morxSubtable // In the order they apply.
}

type morxSubtable struct {
	off      int // The subtable header, in tbl.
	typ      int
	backward bool
}

func parseMorx(tbl be, numGlyphs int) *morx {
	if v := tbl.u16(0); (v != 2 && v != 3) || !tbl.has(0, 8) {
		return nil
	}
	m := &morx{tbl: tbl, numGlyphs: numGlyphs}
	chain := 8
	for range min(tbl.u32(4), len(tbl)/16) {
		if !tbl.has(chain, 16) {
			break
		}
		enabled, chainLen, features, count := tbl.u32(chain), tbl.u32(chain+4), tbl.u32(chain+8), tbl.u32(chain+12)
		if chainLen < 16 {
			break
		}
		st := chain + 16 + 12*features
		for range min(count, len(tbl)/12) {
			length := tbl.u32(st)
			if length < 12 || !tbl.has(st, length) {
				break
			}
			cov, flags := uint32(tbl.u32(st+4)), uint32(tbl.u32(st+8)) //nolint:gosec // 32 bits read.
			typ := int(cov & 0xFF)
			vertical := cov&morxVertical != 0 && cov&morxAllDirection == 0
			if !vertical && flags&uint32(enabled) != 0 && //nolint:gosec // 32 bits read.
				(typ == morxNoncontextual || typ == morxContextual || typ == morxLigature) {
				m.subtables = append(m.subtables, morxSubtable{off: st, typ: typ, backward: cov&morxBackwards != 0})
			}
			st += length
		}
		chain += chainLen
	}
	if len(m.subtables) == 0 {
		return nil // Nothing to apply, so a font's GSUB is not shadowed by this table.
	}
	return m
}

// shape applies the subtables to gids and returns the resulting glyphs.
func (m *morx) shape(gids []uint16) []uint16 {
	for _, st := range m.subtables {
		if st.backward {
			slices.Reverse(gids)
		}
		switch st.typ {
		case morxNoncontextual:
			m.noncontextual(st.off, gids)
		case morxContextual:
			m.contextual(st.off, gids)
		case morxLigature:
			m.ligature(st.off, gids)
		}
		if st.backward {
			slices.Reverse(gids)
		}
	}
	return slices.DeleteFunc(gids, func(g uint16) bool { return g == morxDeleted })
}

func (m *morx) noncontextual(st int, gids []uint16) {
	for i, g := range gids {
		if v, ok := aatLookup(m.tbl, st+12, g, m.numGlyphs); ok {
			gids[i] = uint16(v) //nolint:gosec // 16 bits read.
		}
	}
}

// stateMachine reads the extended state table that starts at hdr.
type stateMachine struct {
	tbl                      be
	numGlyphs                int
	nClasses                 int
	classes, states, entries int // Offsets in tbl.
	entrySize                int
}

func (m *morx) machine(hdr, entrySize int) stateMachine {
	return stateMachine{
		tbl:       m.tbl,
		numGlyphs: m.numGlyphs,
		nClasses:  m.tbl.u32(hdr),
		classes:   hdr + m.tbl.u32(hdr+4),
		states:    hdr + m.tbl.u32(hdr+8),
		entries:   hdr + m.tbl.u32(hdr+12),
		entrySize: entrySize,
	}
}

func (s stateMachine) class(g uint16, endOfText bool) int {
	switch {
	case endOfText:
		return morxClassEndOfText
	case g == morxDeleted:
		return morxClassDeleted
	}
	if c, ok := aatLookup(s.tbl, s.classes, g, s.numGlyphs); ok {
		return c
	}
	return morxClassOutOfBounds
}

// entry returns the offset of the entry for state and class, and its next state
// and flags.
func (s stateMachine) entry(state, class int) (at, next, flags int) {
	if class >= s.nClasses {
		class = morxClassOutOfBounds
	}
	idx := s.tbl.u16(s.states + 2*(state*s.nClasses+class))
	at = s.entries + s.entrySize*idx
	return at, s.tbl.u16(at), s.tbl.u16(at + 2)
}

// run drives the machine over gids, then over the end of text. For each glyph,
// and for the end of text, it calls visit with the position, the offset of the
// entry that applies and its flags. The glyph stays in place while an entry says
// not to advance, though only so many times, so a corrupt table cannot loop
// forever.
func (s stateMachine) run(gids []uint16, visit func(i, entry, flags int, end bool)) {
	state, left := 0, 16*(len(gids)+1)+64
	for i := 0; ; {
		end := i >= len(gids)
		var g uint16
		if !end {
			g = gids[i]
		}
		at, next, flags := s.entry(state, s.class(g, end))
		visit(i, at, flags, end)

		state = next
		if end {
			return
		}
		left--
		if flags&morxDontAdvance == 0 || left <= 0 {
			i++
		}
	}
}

func (m *morx) ligature(st int, gids []uint16) {
	hdr := st + 12
	actions, components, ligatures := hdr+m.tbl.u32(hdr+16), hdr+m.tbl.u32(hdr+20), hdr+m.tbl.u32(hdr+24)

	var stack []int // Positions of the components seen so far.
	m.machine(hdr, 6).run(gids, func(i, at, flags int, end bool) {
		if flags&morxSetMark != 0 {
			if len(stack) > 0 && stack[len(stack)-1] == i { // Never push a position twice.
				stack = stack[:len(stack)-1]
			}
			if len(stack) == morxMaxStack {
				stack = slices.Delete(stack, 0, 1)
			}
			stack = append(stack, i)
		}
		if flags&morxPerformLigat != 0 && len(stack) > 0 && !end {
			stack = m.performLigature(gids, stack, m.tbl.u16(at+4), actions, components, ligatures)
		}
	})
}

// performLigature pops components off stack, from the last one back, following
// the ligature actions from index action, and stores the ligatures they make.
// It returns the stack that is left.
func (m *morx) performLigature(gids []uint16, stack []int, action, actions, components, ligatures int) []int {
	cursor, lig := len(stack), 0
	for {
		if cursor == 0 { // Ran out of components: the table is malformed.
			return stack[:0]
		}
		cursor--
		pos := stack[cursor]

		act := uint32(m.tbl.u32(actions + 4*action)) //nolint:gosec // 32 bits read.
		offset := int(act & morxLigOffset)
		if offset&0x20000000 != 0 { // A 30 bit signed offset.
			offset -= 1 << 30
		}
		lig += m.tbl.u16(components + 2*(int(gids[pos])+offset))

		last := act&morxLigLast != 0
		if last || act&morxLigStore != 0 {
			gids[pos] = uint16(m.tbl.u16(ligatures + 2*lig)) //nolint:gosec // 16 bits read.
			for len(stack)-1 > cursor {                      // The later components are now part of it.
				gids[stack[len(stack)-1]] = morxDeleted
				stack = stack[:len(stack)-1]
			}
		}
		if last {
			return stack
		}
		action++
	}
}

func (m *morx) contextual(st int, gids []uint16) {
	hdr := st + 12
	subs := hdr + m.tbl.u32(hdr+16) // Offsets, from here, of a lookup per substitution.

	replace := func(pos, index int) {
		if index == 0xFFFF || pos < 0 || pos >= len(gids) {
			return
		}
		lookup := subs + m.tbl.u32(subs+4*index)
		if v, ok := aatLookup(m.tbl, lookup, gids[pos], m.numGlyphs); ok {
			gids[pos] = uint16(v) //nolint:gosec // 16 bits read.
		}
	}

	var markSet bool
	var mark int
	m.machine(hdr, 8).run(gids, func(i, at, flags int, end bool) {
		if !end || markSet { // At the end of text nothing is substituted without a mark.
			if markSet {
				replace(mark, m.tbl.u16(at+4))
			}
			replace(min(i, len(gids)-1), m.tbl.u16(at+6))
		}
		if flags&morxSetMark != 0 {
			markSet, mark = true, i
		}
	})
}

// aatLookup returns the value an AAT lookup table at off holds for glyph.
func aatLookup(t be, off int, glyph uint16, numGlyphs int) (int, bool) {
	g := int(glyph)
	switch t.u16(off) {
	case 0: // An array with a value for every glyph.
		if at := off + 2 + 2*g; g < numGlyphs && t.has(at, 2) {
			return t.u16(at), true
		}
	case 2, 4, 6: // Binary searched units, by their last (or only) glyph.
		format, size, count := t.u16(off), t.u16(off+2), t.u16(off+4)
		units := off + 12
		if size < 4 || units > len(t) {
			return 0, false
		}
		count = min(count, (len(t)-units)/size)
		i := sort.Search(count, func(i int) bool { return t.u16(units+size*i) >= g })
		if i == count {
			return 0, false
		}
		unit := units + size*i
		switch format {
		case 6:
			if t.u16(unit) == g {
				return t.u16(unit + 2), true
			}
		case 2, 4:
			first := t.u16(unit + 2)
			if g < first {
				return 0, false
			}
			if format == 2 {
				return t.u16(unit + 4), true
			}
			if at := off + t.u16(unit+4) + 2*(g-first); t.has(at, 2) {
				return t.u16(at), true
			}
		}
	case 8: // An array for the glyphs from the first one.
		first, count := t.u16(off+2), t.u16(off+4)
		if at := off + 6 + 2*(g-first); g >= first && g < first+count && t.has(at, 2) {
			return t.u16(at), true
		}
	case 10: // Like 8, with values of any size.
		size, first, count := t.u16(off+2), t.u16(off+4), t.u16(off+6)
		at := off + 8 + size*(g-first)
		if g < first || g >= first+count || !t.has(at, size) {
			return 0, false
		}
		switch size {
		case 1:
			return t.u8(at), true
		case 2:
			return t.u16(at), true
		case 4:
			return t.u32(at), true
		}
	}
	return 0, false
}
