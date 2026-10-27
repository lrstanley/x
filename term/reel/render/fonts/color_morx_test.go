// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fonts

import (
	"encoding/binary"
	"slices"
	"testing"
)

// Glyphs of the morx fixtures.
const (
	mxX     = 10
	mxY     = 11
	mxLig   = 20 // X followed by Y.
	mxSubY  = 12 // What Y becomes after X.
	mxSubX  = 13 // What X becomes when a Y follows.
	mxOther = 5
)

// lookup8 writes an AAT lookup table of format 8 for the glyphs from first.
func lookup8(first int, values ...int) []byte {
	var w wbuf
	w.u16(8, first, len(values))
	w.u16(values...)
	return w.Bytes()
}

// ligatureSubtable composes X Y into mxLig.
func ligatureSubtable() []byte {
	// Classes: 0 end of text, 1 out of bounds, 2 deleted, 3 end of line, 4 X, 5 Y.
	const classes = 6
	// Entries: 0 nothing, 1 push X, 2 push Y and compose.
	entries := [][3]int{
		{0, 0, 0},
		{1, morxSetMark, 0},
		{0, morxSetMark | morxPerformLigat, 0},
	}
	states := [][classes]int{
		{0, 0, 0, 0, 1, 0}, // Start: X pushes.
		{0, 0, 0, 0, 1, 2}, // After X: another X pushes, Y composes.
	}

	class := lookup8(mxX, 4, 5)
	var st wbuf
	for _, s := range states {
		for _, e := range s {
			st.u16(e)
		}
	}
	var en wbuf
	for _, e := range entries {
		en.u16(e[0], e[1], e[2])
	}
	// Actions pop Y (adds component 0) then X (adds component 1, stores).
	var actions wbuf
	for _, act := range []uint32{(-mxY) & morxLigOffset, morxLigLast | morxLigStore | ((1 - mxX) & morxLigOffset)} {
		_ = binary.Write(&actions, binary.BigEndian, act)
	}
	var comps, ligs wbuf
	comps.u16(0, 0)
	ligs.u16(mxLig)

	const header = 28
	classAt := header
	stateAt := classAt + len(class)
	entryAt := stateAt + st.Len()
	actionAt := entryAt + en.Len()
	compAt := actionAt + actions.Len()
	ligAt := compAt + comps.Len()

	var w wbuf
	w.u32(classes, classAt, stateAt, entryAt, actionAt, compAt, ligAt)
	for _, part := range [][]byte{class, st.Bytes(), en.Bytes(), actions.Bytes(), comps.Bytes(), ligs.Bytes()} {
		w.Write(part)
	}
	return w.Bytes()
}

// contextualSubtable turns Y after X into mxSubY and that X into mxSubX.
func contextualSubtable() []byte {
	const classes = 6
	// Entries: new state, flags, mark substitution, current substitution.
	entries := [][4]int{
		{0, 0, 0xFFFF, 0xFFFF},
		{1, morxSetMark, 0xFFFF, 0xFFFF},
		{0, 0, 1, 0},
	}
	states := [][classes]int{
		{0, 0, 0, 0, 1, 0},
		{0, 0, 0, 0, 1, 2},
	}

	class := lookup8(mxX, 4, 5)
	var st wbuf
	for _, s := range states {
		for _, e := range s {
			st.u16(e)
		}
	}
	var en wbuf
	for _, e := range entries {
		en.u16(e[0], e[1], e[2], e[3])
	}
	// Substitution lookups: Y to mxSubY, X to mxSubX.
	subY, subX := lookup8(mxY, mxSubY), lookup8(mxX, mxSubX)
	var subs wbuf
	subs.u32(8, 8+len(subY))
	subs.Write(subY)
	subs.Write(subX)

	const header = 20
	classAt := header
	stateAt := classAt + len(class)
	entryAt := stateAt + st.Len()
	subsAt := entryAt + en.Len()

	var w wbuf
	w.u32(classes, classAt, stateAt, entryAt, subsAt)
	for _, part := range [][]byte{class, st.Bytes(), en.Bytes(), subs.Bytes()} {
		w.Write(part)
	}
	return w.Bytes()
}

type mxSub struct {
	coverage uint32
	flags    int
	body     []byte
}

// morxTable wraps subtables in a one chain table whose default flags are
// enabled.
func morxTable(enabled int, subtables ...mxSub) []byte {
	var subs wbuf
	for _, s := range subtables {
		subs.u32(12 + len(s.body))
		_ = binary.Write(&subs, binary.BigEndian, s.coverage)
		subs.u32(s.flags)
		subs.Write(s.body)
	}
	var w wbuf
	w.u16(2, 0)
	w.u32(1)
	w.u32(enabled, 16+subs.Len(), 0, len(subtables))
	w.Write(subs.Bytes())
	return w.Bytes()
}

const mxCoverage uint32 = 0x30000000 // Logical order and all directions.

// shapeMorx shapes gids with tbl, or leaves them as they are for a table that
// has nothing to apply.
func shapeMorx(tbl []byte, gids ...uint16) []uint16 {
	m := parseMorx(tbl, 100)
	if m == nil {
		return slices.Clone(gids)
	}
	return m.shape(slices.Clone(gids))
}

func TestMorxLigature(t *testing.T) {
	t.Parallel()

	tbl := morxTable(1, mxSub{mxCoverage | morxLigature, 1, ligatureSubtable()})
	for _, tc := range []struct {
		name string
		in   []uint16
		want []uint16
	}{
		{"composes", []uint16{mxX, mxY}, []uint16{mxLig}},
		{"keeps what is around", []uint16{mxOther, mxX, mxY, mxOther}, []uint16{mxOther, mxLig, mxOther}},
		{"composes after a lone X", []uint16{mxX, mxX, mxY}, []uint16{mxX, mxLig}},
		{"twice", []uint16{mxX, mxY, mxX, mxY}, []uint16{mxLig, mxLig}},
		{"lone X", []uint16{mxX}, []uint16{mxX}},
		{"lone Y", []uint16{mxY}, []uint16{mxY}},
		{"wrong order", []uint16{mxY, mxX}, []uint16{mxY, mxX}},
		{"nothing", nil, []uint16{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := shapeMorx(tbl, tc.in...); !slices.Equal(got, tc.want) {
				t.Fatalf("shape(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestMorxContextual(t *testing.T) {
	t.Parallel()

	tbl := morxTable(1, mxSub{mxCoverage | morxContextual, 1, contextualSubtable()})
	for _, tc := range []struct {
		name string
		in   []uint16
		want []uint16
	}{
		{"marked and current", []uint16{mxX, mxY}, []uint16{mxSubX, mxSubY}},
		{"later mark", []uint16{mxX, mxX, mxY}, []uint16{mxX, mxSubX, mxSubY}},
		{"lone Y", []uint16{mxY}, []uint16{mxY}},
		{"lone X", []uint16{mxX}, []uint16{mxX}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := shapeMorx(tbl, tc.in...); !slices.Equal(got, tc.want) {
				t.Fatalf("shape(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestMorxNoncontextualAndSubtableOrder(t *testing.T) {
	t.Parallel()

	// Y becomes X, then X Y is composed: subtables apply in order.
	var sub wbuf
	sub.u16(6, 4, 1, 4, 0, 0) // Format 6, one unit of four bytes.
	sub.u16(mxY, mxX)
	tbl := morxTable(1,
		mxSub{mxCoverage | morxNoncontextual, 1, sub.Bytes()},
		mxSub{mxCoverage | morxLigature, 1, ligatureSubtable()},
	)
	if got, want := shapeMorx(tbl, mxX, mxY), []uint16{mxX, mxX}; !slices.Equal(got, want) {
		// X Y -> X X: nothing to compose.
		t.Fatalf("shape = %v, want %v", got, want)
	}

	// Composed first, then Y (now gone) is not replaced.
	tbl = morxTable(1,
		mxSub{mxCoverage | morxLigature, 1, ligatureSubtable()},
		mxSub{mxCoverage | morxNoncontextual, 1, sub.Bytes()},
	)
	if got, want := shapeMorx(tbl, mxX, mxY, mxY), []uint16{mxLig, mxX}; !slices.Equal(got, want) {
		t.Fatalf("shape = %v, want %v", got, want)
	}
}

func TestMorxSubtableSelection(t *testing.T) {
	t.Parallel()

	lig := ligatureSubtable()
	in := []uint16{mxX, mxY}
	composed, plain := []uint16{mxLig}, in

	for _, tc := range []struct {
		name    string
		enabled int
		cov     uint32
		flags   int
		want    []uint16
	}{
		{"enabled", 0x3, mxCoverage | morxLigature, 0x2, composed},
		{"feature flags off", 0x1, mxCoverage | morxLigature, 0x2, plain},
		{"vertical only", 1, 0x80000000 | morxLigature, 1, plain},
		{"vertical and all directions", 1, 0x80000000 | 0x20000000 | morxLigature, 1, composed},
		{"rearrangement is skipped", 1, mxCoverage, 1, plain},
		{"insertion is skipped", 1, mxCoverage | 5, 1, plain},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tbl := morxTable(tc.enabled, mxSub{tc.cov, tc.flags, lig})
			if got := shapeMorx(tbl, in...); !slices.Equal(got, tc.want) {
				t.Fatalf("shape = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestMorxWithoutUsableSubtables makes sure a morx table that applies nothing is
// not used, so the font's GSUB still is.
func TestMorxWithoutUsableSubtables(t *testing.T) {
	t.Parallel()

	for _, tbl := range [][]byte{
		morxTable(1),
		morxTable(1, mxSub{mxCoverage | morxLigature, 2, ligatureSubtable()}),
	} {
		if m := parseMorx(tbl, 10); m != nil {
			t.Errorf("parseMorx = %+v, want none", m)
		}
	}
}

func TestMorxBackwardSubtable(t *testing.T) {
	t.Parallel()

	// A backwards subtable sees the glyphs last to first, so it composes Y X.
	tbl := morxTable(1, mxSub{mxCoverage | morxBackwards | morxLigature, 1, ligatureSubtable()})
	if got, want := shapeMorx(tbl, mxOther, mxY, mxX), []uint16{mxOther, mxLig}; !slices.Equal(got, want) {
		t.Fatalf("shape = %v, want %v", got, want)
	}
}

func TestMorxMalformed(t *testing.T) {
	t.Parallel()

	good := morxTable(1, mxSub{mxCoverage | morxLigature, 1, ligatureSubtable()})
	if parseMorx(nil, 10) != nil || parseMorx(good[:6], 10) != nil {
		t.Fatal("parsed a table that is too short")
	}
	bad := slices.Clone(good)
	bad[1] = 9 // Version 9.
	if parseMorx(bad, 10) != nil {
		t.Fatal("parsed an unknown version")
	}

	// Truncating or scrambling the table must not panic or hang.
	for n := range good {
		_ = shapeMorx(good[:n], mxX, mxY)
	}
	for i := range good {
		scrambled := slices.Clone(good)
		scrambled[i] ^= 0xFF
		_ = shapeMorx(scrambled, mxX, mxY, mxX, mxY)
	}

	// A state that never advances ends, since the machine is bounded.
	var loop wbuf
	loop.u32(6, 28, 28+len(lookup8(mxX, 4, 5)), 28+len(lookup8(mxX, 4, 5))+12)
	loop.u32(0, 0, 0)
	loop.Write(lookup8(mxX, 4, 5))
	for range 6 { // Every class: entry 0.
		loop.u16(0)
	}
	loop.u16(0, morxDontAdvance, 0) // Entry 0: stay and don't advance.
	tbl := morxTable(1, mxSub{mxCoverage | morxLigature, 1, loop.Bytes()})
	if got := shapeMorx(tbl, mxX, mxY); !slices.Equal(got, []uint16{mxX, mxY}) {
		t.Fatalf("shape = %v, want the glyphs unchanged", got)
	}
}

func TestAATLookupFormats(t *testing.T) {
	t.Parallel()

	var f0, f2, f4, f6, f10 wbuf
	f0.u16(0)
	f0.u16(0, 7, 8, 9)

	f2.u16(2, 6, 2, 0, 0, 0)
	f2.u16(4, 2, 100) // Glyphs 2-4 are 100.
	f2.u16(9, 8, 200)

	f4.u16(4, 6, 1, 0, 0, 0)
	f4.u16(6, 5, 12+6+6) // Glyphs 5-6 hold values from here: after this unit and a terminator.
	f4.u16(0xFFFF, 0xFFFF, 0)
	f4.u16(50, 60)

	f6.u16(6, 4, 2, 0, 0, 0)
	f6.u16(3, 30, 8, 80)

	f10.u16(10, 1, 4, 3) // One byte values for glyphs 4-6.
	f10.WriteByte(1)
	f10.WriteByte(2)
	f10.WriteByte(3)

	for _, tc := range []struct {
		name  string
		table []byte
		hits  map[int]int
		miss  []int
	}{
		{"array", f0.Bytes(), map[int]int{0: 0, 1: 7, 2: 8, 3: 9}, []int{4, 50}},
		{"segment single", f2.Bytes(), map[int]int{2: 100, 3: 100, 4: 100, 8: 200, 9: 200}, []int{1, 5, 7, 10}},
		{"segment array", f4.Bytes(), map[int]int{5: 50, 6: 60}, []int{4, 7}},
		{"single table", f6.Bytes(), map[int]int{3: 30, 8: 80}, []int{2, 4, 9}},
		{"trimmed array", lookup8(10, 1, 2, 3), map[int]int{10: 1, 11: 2, 12: 3}, []int{9, 13}},
		{"extended trimmed array", f10.Bytes(), map[int]int{4: 1, 5: 2, 6: 3}, []int{3, 7}},
		{"unknown format", []byte{0, 99, 0, 0}, nil, []int{0, 1}},
		{"empty", nil, nil, []int{0, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for g, want := range tc.hits {
				if got, ok := aatLookup(tc.table, 0, uint16(g), 4); !ok || got != want { //nolint:gosec // Test data.
					t.Errorf("glyph %d = %d, %v; want %d", g, got, ok, want)
				}
			}
			for _, g := range tc.miss {
				if got, ok := aatLookup(tc.table, 0, uint16(g), 4); ok { //nolint:gosec // Test data.
					t.Errorf("glyph %d = %d, want no value", g, got)
				}
			}
		})
	}
}
