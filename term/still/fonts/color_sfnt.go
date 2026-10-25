// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fonts

import (
	"encoding/binary"
	"errors"
	"io"
	"sort"
)

// be reads big-endian fields of an sfnt table. Reads past the end return 0
// instead of panicking, since the data comes from font files on disk. Loops
// over table contents are bounded by the table length, so a corrupt count
// cannot make a read run away.
type be []byte

func (b be) u8(at int) int {
	if at < 0 || at+1 > len(b) {
		return 0
	}
	return int(b[at])
}

func (b be) u16(at int) int {
	if at < 0 || at+2 > len(b) {
		return 0
	}
	return int(binary.BigEndian.Uint16(b[at:]))
}

func (b be) i16(at int) int {
	return int(int16(b.u16(at))) //nolint:gosec // Reinterpreting the bits is the point.
}

func (b be) u32(at int) int {
	if at < 0 || at+4 > len(b) {
		return 0
	}
	return int(binary.BigEndian.Uint32(b[at:]))
}

// has reports whether n bytes are readable at at.
func (b be) has(at, n int) bool { return at >= 0 && n >= 0 && at+n <= len(b) }

// ttcTag starts a font collection (.ttc), where other fonts start with a version.
const ttcTag = "ttcf"

// tableRec locates a table in a font file.
type tableRec struct{ off, length int }

// readTableDir reads the table directory of face index of a font or collection.
func readTableDir(r io.ReaderAt, index int) (map[string]tableRec, error) {
	var head [12]byte
	if _, err := r.ReadAt(head[:], 0); err != nil {
		return nil, err
	}
	base := int64(0)
	if string(head[:4]) == ttcTag {
		n := binary.BigEndian.Uint32(head[8:])
		if index < 0 || uint32(index) >= n { //nolint:gosec // Checked non-negative.
			return nil, errors.New("face index out of range")
		}
		var off [4]byte
		if _, err := r.ReadAt(off[:], 12+4*int64(index)); err != nil {
			return nil, err
		}
		base = int64(binary.BigEndian.Uint32(off[:]))
		if _, err := r.ReadAt(head[:], base); err != nil {
			return nil, err
		}
	}
	n := int(binary.BigEndian.Uint16(head[4:]))
	dir := make([]byte, 16*n)
	if _, err := r.ReadAt(dir, base+12); err != nil {
		return nil, err
	}
	tables := make(map[string]tableRec, n)
	for i := range n {
		rec := dir[16*i:]
		tables[string(rec[:4])] = tableRec{
			off:    int(binary.BigEndian.Uint32(rec[8:])),
			length: int(binary.BigEndian.Uint32(rec[12:])),
		}
	}
	return tables, nil
}

// maxColorTableLen bounds the tables read whole while loading a color font.
// Bitmap data (CBDT, sbix) is never read whole.
const maxColorTableLen = 32 << 20

// readTable reads table tag whole, or returns nil when the font has no such
// table or it is implausibly large.
func readTable(r io.ReaderAt, dir map[string]tableRec, tag string) be {
	rec, ok := dir[tag]
	if !ok || rec.length <= 0 || rec.length > maxColorTableLen {
		return nil
	}
	buf := make([]byte, rec.length)
	if _, err := r.ReadAt(buf, int64(rec.off)); err != nil {
		return nil
	}
	return buf
}

// markGlyphs returns whether a glyph is a mark, as classified by the glyph class
// definitions of a raw GDEF table, or nil if there are none.
func markGlyphs(gdef be) func(gid uint16) bool {
	const markClass = 3
	off := gdef.u16(4)
	if off == 0 {
		return nil
	}
	return func(gid uint16) bool {
		g := int(gid)
		switch gdef.u16(off) {
		case 1: // The classes of a run of glyphs.
			if i := g - gdef.u16(off+2); i >= 0 && i < gdef.u16(off+4) {
				return gdef.u16(off+6+2*i) == markClass
			}
		case 2: // Ranges of glyphs, sorted.
			n := min(gdef.u16(off+2), (len(gdef)-off-4)/6)
			i := sort.Search(n, func(i int) bool { return gdef.u16(off+4+6*i+2) >= g })
			return i < n && gdef.u16(off+4+6*i) <= g && gdef.u16(off+4+6*i+4) == markClass
		}
		return false
	}
}

// glyphLookup maps a rune to a glyph index, or 0 when the font has none.
type glyphLookup func(r rune) uint16

// newGlyphLookup returns the rune to glyph mapping of a raw cmap table.
func newGlyphLookup(cmap []byte) (glyphLookup, error) {
	sub, format, err := bestCmapSubtable(cmap)
	if err != nil {
		return nil, err
	}
	b := be(sub)
	if format == 12 {
		n := min(b.u32(12), (len(b)-16)/12)
		return func(r rune) uint16 {
			// Groups are sorted by end code: find the first that ends at or
			// after r.
			i := sort.Search(n, func(i int) bool { return b.u32(16+12*i+4) >= int(r) })
			if i == n || b.u32(16+12*i) > int(r) {
				return 0
			}
			return uint16(b.u32(16+12*i+8) + int(r) - b.u32(16+12*i)) //nolint:gosec // Glyph indexes are 16 bits.
		}, nil
	}

	segX2 := b.u16(6)
	ends, starts, deltas, offsets := 14, 16+segX2, 16+2*segX2, 16+3*segX2
	segs := segX2 / 2
	return func(r rune) uint16 {
		if r > 0xFFFF {
			return 0
		}
		i := sort.Search(segs, func(i int) bool { return b.u16(ends+2*i) >= int(r) })
		if i == segs || b.u16(starts+2*i) > int(r) {
			return 0
		}
		var g int
		if ro := b.u16(offsets + 2*i); ro == 0 {
			g = int(r)
		} else if g = b.u16(offsets + 2*i + ro + 2*(int(r)-b.u16(starts+2*i))); g == 0 {
			return 0
		}
		return uint16((g + b.u16(deltas+2*i)) & 0xFFFF) //nolint:gosec // Masked to 16 bits.
	}, nil
}
