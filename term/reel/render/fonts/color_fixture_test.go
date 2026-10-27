// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fonts

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"maps"
	"slices"
	"unicode/utf16"
)

// wbuf builds big-endian binary data.
type wbuf struct{ bytes.Buffer }

func (w *wbuf) u16(vs ...int) {
	for _, v := range vs {
		_ = binary.Write(w, binary.BigEndian, uint16(v)) //nolint:gosec // Test data.
	}
}

func (w *wbuf) u32(vs ...int) {
	for _, v := range vs {
		_ = binary.Write(w, binary.BigEndian, uint32(v)) //nolint:gosec // Test data.
	}
}

// fixtureBitmap is a solid color PNG glyph.
type fixtureBitmap struct {
	c    color.NRGBA
	w, h int
}

func (b fixtureBitmap) png() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, b.w, b.h))
	draw.Draw(img, img.Bounds(), image.NewUniform(b.c), image.Point{}, draw.Src)
	var out bytes.Buffer
	_ = png.Encode(&out, img)
	return out.Bytes()
}

type fixtureLig struct {
	parts []uint16
	glyph uint16
}

// fixture describes a synthetic color font: just the tables the color glyph
// reader uses, so tests don't depend on installed or bundled emoji fonts.
type fixture struct {
	format        string // "cbdt1", "cbdt3" (CBLC index format) or "sbix".
	numGlyphs     int
	cmap          map[rune]uint16
	bitmaps       map[uint16]fixtureBitmap
	dupes         map[uint16]uint16 // sbix only: glyph to the glyph it repeats.
	flips         map[uint16]uint16 // sbix only: glyph to the glyph it mirrors.
	marks         []uint16          // Glyphs that GDEF classes as marks, which are drawn over the glyph before.
	ligs          []fixtureLig
	singles       map[uint16]uint16 // Single substitutions, applied before the ligatures.
	extension     bool              // Wrap the ligature lookup in an extension lookup.
	rangeCoverage bool              // Write ligature coverage as ranges.
}

func (f fixture) build() []byte {
	tables := map[string][]byte{
		"name": fixtureName("Fixture Emoji"),
		"cmap": f.cmapTable(),
		"GSUB": f.gsubTable(),
	}
	if len(f.marks) > 0 {
		var gdef wbuf
		gdef.u16(1, 0, 12, 0, 0, 0) // Version 1.0, then only the glyph class definitions.
		gdef.u16(2, len(f.marks))   // ClassDef format 2: a range per glyph.
		for _, g := range slices.Sorted(slices.Values(f.marks)) {
			gdef.u16(int(g), int(g), 3) // Class 3 is a mark.
		}
		tables["GDEF"] = gdef.Bytes()
	}
	if f.format == "sbix" {
		var maxp wbuf
		maxp.u32(0x00005000)
		maxp.u16(f.numGlyphs)
		tables["maxp"], tables["sbix"] = maxp.Bytes(), f.sbixTable()
	} else {
		tables["CBLC"], tables["CBDT"] = f.cbdtTables()
	}
	return assembleSFNT(tables)
}

func assembleSFNT(tables map[string][]byte) []byte {
	tags := slices.Sorted(maps.Keys(tables))

	var head, data wbuf
	head.u32(0x00010000)
	head.u16(len(tags), 0, 0, 0)
	for _, tag := range tags {
		head.WriteString(tag)
		head.u32(0, 12+16*len(tags)+data.Len(), len(tables[tag]))
		data.Write(tables[tag])
		for data.Len()%4 != 0 {
			data.WriteByte(0)
		}
	}
	return append(head.Bytes(), data.Bytes()...)
}

func fixtureName(family string) []byte {
	str := utf16.Encode([]rune(family))
	var w wbuf
	w.u16(0, 1, 6+12) // Format 0, one record, string storage after it.
	w.u16(3, 1, 0x409, 1, 2*len(str), 0)
	for _, u := range str {
		w.u16(int(u))
	}
	return w.Bytes()
}

func (f fixture) cmapTable() []byte {
	runes := slices.Sorted(maps.Keys(f.cmap))

	var w wbuf
	w.u16(0, 1)
	w.u16(3, 10)
	w.u32(12)
	w.u16(12, 0)
	w.u32(16+12*len(runes), 0, len(runes))
	for _, r := range runes {
		w.u32(int(r), int(r), int(f.cmap[r]))
	}
	return w.Bytes()
}

func (f fixture) coverage(glyphs []uint16) []byte {
	var w wbuf
	if !f.rangeCoverage {
		w.u16(1, len(glyphs))
		for _, g := range glyphs {
			w.u16(int(g))
		}
		return w.Bytes()
	}
	w.u16(2, len(glyphs))
	for i, g := range glyphs { // One range per glyph.
		w.u16(int(g), int(g), i)
	}
	return w.Bytes()
}

func (f fixture) gsubTable() []byte {
	// Lookups: single substitutions (if any), then ligatures.
	var lookups [][]byte
	if len(f.singles) > 0 {
		var from []uint16
		for g := range f.singles {
			from = append(from, g)
		}
		slices.Sort(from)
		cov := f.coverage(from)
		var st wbuf
		st.u16(2, 6+2*len(from), len(from))
		for _, g := range from {
			st.u16(int(f.singles[g]))
		}
		st.Write(cov)
		lookups = append(lookups, fixtureLookup(1, st.Bytes()))
	}
	if len(f.ligs) > 0 {
		lig := f.ligatureSubtable()
		if f.extension {
			var ext wbuf
			ext.u16(1, 4)
			ext.u32(8)
			ext.Write(lig)
			lookups = append(lookups, fixtureLookup(7, ext.Bytes()))
		} else {
			lookups = append(lookups, fixtureLookup(4, lig))
		}
	}

	var feature wbuf
	feature.u16(0, len(lookups))
	for i := range lookups {
		feature.u16(i)
	}
	var features wbuf
	features.u16(1)
	features.WriteString("ccmp")
	features.u16(8)
	features.Write(feature.Bytes())

	var list wbuf
	list.u16(len(lookups))
	at := 2 + 2*len(lookups)
	for _, l := range lookups {
		list.u16(at)
		at += len(l)
	}
	for _, l := range lookups {
		list.Write(l)
	}

	var w wbuf
	w.u16(1, 0, 10, 12, 12+features.Len()) // Version, scripts, features, lookups.
	w.u16(0)                               // No scripts.
	w.Write(features.Bytes())
	w.Write(list.Bytes())
	return w.Bytes()
}

func fixtureLookup(kind int, subtable []byte) []byte {
	var w wbuf
	w.u16(kind, 0, 1, 8)
	w.Write(subtable)
	return w.Bytes()
}

func (f fixture) ligatureSubtable() []byte {
	sets := map[uint16][]fixtureLig{}
	for _, l := range f.ligs {
		sets[l.parts[0]] = append(sets[l.parts[0]], l)
	}
	firsts := make([]uint16, 0, len(sets))
	for g := range sets {
		firsts = append(firsts, g)
	}
	slices.Sort(firsts)

	cov := f.coverage(firsts)
	var setData [][]byte
	for _, first := range firsts {
		var ligs wbuf
		var w wbuf
		w.u16(len(sets[first]))
		at := 2 + 2*len(sets[first])
		for _, l := range sets[first] {
			w.u16(at)
			ligs.u16(int(l.glyph), len(l.parts))
			for _, p := range l.parts[1:] {
				ligs.u16(int(p))
			}
			at = 2 + 2*len(sets[first]) + ligs.Len()
		}
		w.Write(ligs.Bytes())
		setData = append(setData, w.Bytes())
	}

	var w wbuf
	header := 6 + 2*len(firsts)
	w.u16(1, header, len(firsts))
	at := header + len(cov)
	for _, s := range setData {
		w.u16(at)
		at += len(s)
	}
	w.Write(cov)
	for _, s := range setData {
		w.Write(s)
	}
	return w.Bytes()
}

func (f fixture) sortedBitmapGlyphs() []uint16 {
	gids := make([]uint16, 0, len(f.bitmaps))
	for g := range f.bitmaps {
		gids = append(gids, g)
	}
	slices.Sort(gids)
	return gids
}

func (f fixture) cbdtTables() (cblc, cbdt []byte) {
	gids := f.sortedBitmapGlyphs()
	first, last := int(gids[0]), int(gids[len(gids)-1])

	var data wbuf
	data.u32(0x00030000)
	offsets := make([]int, 0, last-first+2)
	for g := first; g <= last; g++ {
		offsets = append(offsets, data.Len()-4)
		if b, ok := f.bitmaps[uint16(g)]; ok { //nolint:gosec // Test data.
			img := b.png()
			data.WriteByte(byte(b.h)) // Small metrics: height, width, bearings, advance.
			data.WriteByte(byte(b.w))
			data.Write([]byte{0, 0, byte(b.w)})
			data.u32(len(img))
			data.Write(img)
		}
	}
	offsets = append(offsets, data.Len()-4)

	var sub wbuf
	indexFormat := 1
	if f.format == "cbdt3" {
		indexFormat = 3
	}
	sub.u16(indexFormat, 17)
	sub.u32(4)
	for _, o := range offsets {
		if indexFormat == 3 {
			sub.u16(o)
		} else {
			sub.u32(o)
		}
	}

	var w wbuf
	w.u32(0x00030000, 1)
	w.u32(56, 8+sub.Len(), 1, 0) // Subtable array, its size, one subtable, color ref.
	w.Write(make([]byte, 24))    // Line metrics.
	w.u16(first, last)
	w.Write([]byte{16, 16, 32, 1}) // ppem x and y, bit depth, flags.
	w.u16(first, last)             // The index subtable array: one entry, whose subtable follows it.
	w.u32(8)
	w.Write(sub.Bytes())
	return w.Bytes(), data.Bytes()
}

func (f fixture) sbixTable() []byte {
	var records [][]byte
	for g := range f.numGlyphs {
		var rec wbuf
		switch b, ok := f.bitmaps[uint16(g)]; { //nolint:gosec // Test data.
		case ok:
			rec.u16(0, 0)
			rec.WriteString("png ")
			rec.Write(b.png())
		case f.dupes[uint16(g)] != 0: //nolint:gosec // Test data.
			rec.u16(0, 0)
			rec.WriteString("dupe")
			rec.u16(int(f.dupes[uint16(g)])) //nolint:gosec // Test data.
		case f.flips[uint16(g)] != 0: //nolint:gosec // Test data.
			rec.u16(0, 0)
			rec.WriteString("flip")
			rec.u16(int(f.flips[uint16(g)])) //nolint:gosec // Test data.
		}
		records = append(records, rec.Bytes())
	}

	var strike wbuf
	strike.u16(16, 72)
	at := 4 + 4*(f.numGlyphs+1)
	for _, r := range records {
		strike.u32(at)
		at += len(r)
	}
	strike.u32(at)
	for _, r := range records {
		strike.Write(r)
	}

	var w wbuf
	w.u16(1, 1)
	w.u32(1, 12)
	w.Write(strike.Bytes())
	return w.Bytes()
}

// Fixture colors.
var (
	fxRed     = color.NRGBA{R: 255, A: 255}
	fxGreen   = color.NRGBA{G: 255, A: 255}
	fxBlue    = color.NRGBA{B: 255, A: 255}
	fxYellow  = color.NRGBA{R: 255, G: 255, A: 255}
	fxMagenta = color.NRGBA{R: 255, B: 255, A: 255}
	fxCyan    = color.NRGBA{G: 255, B: 255, A: 255}
	fxPink    = color.NRGBA{R: 255, G: 128, B: 128, A: 255}
	fxOrange  = color.NRGBA{R: 255, G: 128, A: 255}
)

// Fixture glyph ids.
const (
	gidA = iota + 1
	gidSmile
	gidThumbs
	gidMan
	gidWoman
	gidZWJ
	gidVS16
	gidCouple // man ZWJ woman, twice as wide as the others.
	gidRegU
	gidRegS
	gidFlagUS
	gidHeart
	gidStar
	gidStarColor
	gidSmile2 // sbix: a "dupe" of the smile.
	gidSmile3 // sbix: a "flip" of the smile.
	gidCount
)

// standardFixture is a font with a few emoji, some sequences, and a single
// substitution, in the given format.
func standardFixture(format string) fixture {
	return fixture{
		format:    format,
		numGlyphs: gidCount,
		cmap: map[rune]uint16{
			'A': gidA, 0x1F600: gidSmile, 0x1F44D: gidThumbs, 0x1F468: gidMan, 0x1F469: gidWoman,
			0x200D: gidZWJ, 0xFE0F: gidVS16, 0x1F1FA: gidRegU, 0x1F1F8: gidRegS, 0x2764: gidHeart,
			0x2605: gidStar, 0x1F601: gidSmile2, 0x1F602: gidSmile3,
		},
		bitmaps: map[uint16]fixtureBitmap{
			gidSmile:     {fxRed, 16, 16},
			gidThumbs:    {fxGreen, 16, 16},
			gidMan:       {fxBlue, 16, 16},
			gidWoman:     {fxYellow, 16, 16},
			gidCouple:    {fxMagenta, 32, 16},
			gidFlagUS:    {fxCyan, 16, 16},
			gidHeart:     {fxPink, 16, 16},
			gidStarColor: {fxOrange, 16, 16},
		},
		dupes: map[uint16]uint16{gidSmile2: gidSmile},
		flips: map[uint16]uint16{gidSmile3: gidSmile},
		ligs: []fixtureLig{
			{[]uint16{gidMan, gidZWJ, gidWoman}, gidCouple},
			{[]uint16{gidRegU, gidRegS}, gidFlagUS},
		},
		singles: map[uint16]uint16{gidStar: gidStarColor},
	}
}
