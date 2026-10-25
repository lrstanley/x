// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fonts

import (
	"cmp"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"

	xfont "golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// maxCmapLen bounds the cmap tables read while indexing system fonts.
const maxCmapLen = 16 << 20

// SystemFallback returns an installed font that can draw r, for runes that no
// configured or bundled face covers (CJK, emoji, uncommon scripts).
//
// Fonts without outlines and color emoji fonts (COLR, CBDT, sbix, SVG) are
// skipped, since only outline glyphs can be drawn, as are fonts whose glyph
// for r has no ink. Candidates are tried upright first, then monospace, then
// nearest to regular weight, then in font search order (see [Dirs]). The first
// call reads the cmap of every installed font; fonts installed after that are
// not seen by the running process. Fonts are read on demand from their files,
// which stay open.
func SystemFallback(r rune) (*opentype.Font, bool) {
	// Invisible runes never need a font, so they don't justify reading every
	// installed font's cmap.
	if unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
		return nil, false
	}
	return lookupFallback(systemFallbackIndex(), r)
}

func lookupFallback(faces []fallbackFace, r rune) (*opentype.Font, bool) {
	for _, f := range faces {
		if !f.cov.has(r) {
			continue
		}
		if font, err := loadFallbackFont(f.face); err == nil && hasInk(font, r) {
			return font, true
		}
	}
	return nil, false
}

// hasInk reports whether font has a non-empty outline for r.
func hasInk(font *opentype.Font, r rune) bool {
	var buf sfnt.Buffer
	idx, err := font.GlyphIndex(&buf, r)
	if err != nil || idx == 0 {
		return false
	}
	bounds, _, err := font.GlyphBounds(&buf, idx, fixed.I(int(font.UnitsPerEm())), xfont.HintingNone)
	return err == nil && !bounds.Empty()
}

type fallbackFace struct {
	face
	cov coverage
}

// systemFallbackIndex holds the system faces usable for fallback, in
// preference order. It is built on first use.
var systemFallbackIndex = sync.OnceValue(buildSystemFallbackIndex)

func buildSystemFallbackIndex() []fallbackFace {
	return buildFallbackIndex(systemFontPaths())
}

// buildFallbackIndex reads the usable faces of every source and orders them
// by preference. Sources that fail to read are skipped.
func buildFallbackIndex(srcs []string) []fallbackFace {
	results := make([][]fallbackFace, len(srcs))
	parallelFor(len(srcs), func(i int) {
		results[i] = readFallbackFaces(srcs[i])
	})
	faces := slices.Concat(results...)
	slices.SortStableFunc(faces, func(a, b fallbackFace) int {
		return cmp.Or(
			compareBool(a.italic, b.italic),
			compareBool(!a.tables.monospace, !b.tables.monospace),
			cmp.Compare(weightRank(400, a.weight), weightRank(400, b.weight)),
		)
	})
	return faces
}

// compareBool orders false before true.
func compareBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	}
	return -1
}

// readFallbackFaces reads the faces of a "file:" source with their coverage,
// keeping only faces that have outlines, aren't color fonts, and map at least
// one rune.
func readFallbackFaces(src string) []fallbackFace {
	file, err := os.Open(strings.TrimPrefix(src, "file:"))
	if err != nil {
		return nil
	}
	defer file.Close()
	faces, err := readFaces(file, src)
	if err != nil {
		return nil
	}

	var out []fallbackFace
	covs := map[uint32]coverage{} // Faces of a collection often share one cmap.
	for _, f := range faces {
		t := f.tables
		if !t.outlines || t.color || t.cmapLen == 0 || t.cmapLen > maxCmapLen {
			continue
		}
		cov, ok := covs[t.cmapOff]
		if !ok {
			cov, _ = readCoverage(file, t) // An unreadable cmap covers nothing.
			covs[t.cmapOff] = cov
		}
		if len(cov) > 0 {
			out = append(out, fallbackFace{face: f, cov: cov})
		}
	}
	return out
}

func readCoverage(r io.ReaderAt, t faceTables) (coverage, error) {
	tbl := make([]byte, t.cmapLen)
	if _, err := r.ReadAt(tbl, int64(t.cmapOff)); err != nil {
		return nil, err
	}
	return parseCmap(tbl)
}

// fallbackFonts caches fonts loaded by [loadFallbackFont], including failures,
// by source and face index.
var fallbackFonts sync.Map // string -> func() (*opentype.Font, error)

// loadFallbackFont parses a system face for fallback. Unlike [loadFace], the
// file stays open and glyphs are read from it on demand, so a large CJK
// collection isn't held in memory.
func loadFallbackFont(f face) (*opentype.Font, error) {
	key := f.src + "#" + strconv.Itoa(f.index)
	load, ok := fallbackFonts.Load(key)
	if !ok {
		load, _ = fallbackFonts.LoadOrStore(key, sync.OnceValues(func() (*opentype.Font, error) {
			return openFallbackFont(f)
		}))
	}
	fn, _ := load.(func() (*opentype.Font, error))
	return fn()
}

func openFallbackFont(f face) (*opentype.Font, error) {
	file, err := os.Open(strings.TrimPrefix(f.src, "file:"))
	if err != nil {
		return nil, err
	}
	font, err := parseReaderAt(file, f.index)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return font, nil
}

// parseReaderAt parses face index of a font or collection without reading
// glyph data up front.
func parseReaderAt(r io.ReaderAt, index int) (*opentype.Font, error) {
	var tag [4]byte
	if _, err := r.ReadAt(tag[:], 0); err != nil {
		return nil, err
	}
	if string(tag[:]) != ttcTag {
		return opentype.ParseReaderAt(r)
	}
	col, err := opentype.ParseCollectionReaderAt(r)
	if err != nil {
		return nil, err
	}
	return col.Font(index)
}

// runeRange is an inclusive range of runes.
type runeRange struct{ lo, hi rune }

// coverage is a sorted, non-overlapping set of runes that map to a glyph.
type coverage []runeRange

func (c coverage) has(r rune) bool {
	_, found := slices.BinarySearchFunc(c, r, func(rr runeRange, r rune) int {
		switch {
		case rr.hi < r:
			return -1
		case rr.lo > r:
			return 1
		}
		return 0
	})
	return found
}

// normalized sorts c and merges overlapping and adjacent ranges in place.
// Fonts normally list their mappings in order; this keeps [coverage.has]
// correct for the ones that don't.
func (c coverage) normalized() coverage {
	slices.SortFunc(c, func(a, b runeRange) int { return cmp.Compare(a.lo, b.lo) })
	out := c[:0]
	for _, rr := range c {
		if n := len(out); n > 0 && out[n-1].hi+1 >= rr.lo {
			out[n-1].hi = max(out[n-1].hi, rr.hi)
			continue
		}
		out = append(out, rr)
	}
	return out
}

// parseCmap reads the coverage of the best Unicode subtable of a raw cmap
// table: format 12 (full repertoire) when present, otherwise format 4 (BMP).
func parseCmap(tbl []byte) (coverage, error) {
	best, format, err := bestCmapSubtable(tbl)
	if err != nil {
		return nil, err
	}
	if format == 12 {
		return parseCmap12(best)
	}
	return parseCmap4(best)
}

// bestCmapSubtable returns the Unicode subtable of a raw cmap table that maps
// the most runes, and its format (12 or 4).
func bestCmapSubtable(tbl []byte) (sub []byte, format int, err error) {
	if len(tbl) < 4 {
		return nil, 0, errors.New("short cmap table")
	}
	var best []byte
	for i := range int(binary.BigEndian.Uint16(tbl[2:])) {
		rec := 4 + 8*i
		if rec+8 > len(tbl) {
			break
		}
		platform, encoding := binary.BigEndian.Uint16(tbl[rec:]), binary.BigEndian.Uint16(tbl[rec+2:])
		off := int(binary.BigEndian.Uint32(tbl[rec+4:]))
		if off+4 > len(tbl) {
			continue
		}
		isUnicode := platform == 0 || (platform == 3 && (encoding == 1 || encoding == 10))
		if !isUnicode {
			continue
		}
		// Format 12 covers more than 4, so the larger of the two wins.
		if f := int(binary.BigEndian.Uint16(tbl[off:])); (f == 12 || f == 4) && f > format {
			best, format = tbl[off:], f
		}
	}
	if best == nil {
		return nil, 0, errors.New("no supported unicode cmap subtable")
	}
	return best, format, nil
}

func parseCmap12(sub []byte) (coverage, error) {
	if len(sub) < 16 {
		return nil, errors.New("short cmap format 12 subtable")
	}
	n := int(binary.BigEndian.Uint32(sub[12:]))
	if n > (len(sub)-16)/12 {
		return nil, errors.New("truncated cmap format 12 subtable")
	}
	var cov coverage
	for i := range n {
		g := sub[16+12*i:]
		lo, hi := rune(binary.BigEndian.Uint32(g)), rune(binary.BigEndian.Uint32(g[4:])) //nolint:gosec // Range-checked below.
		if binary.BigEndian.Uint32(g[8:]) == 0 {
			lo++ // The first rune maps to .notdef.
		}
		if lo <= hi && hi <= unicode.MaxRune {
			cov = append(cov, runeRange{lo, hi})
		}
	}
	return cov.normalized(), nil
}

func parseCmap4(sub []byte) (coverage, error) {
	if len(sub) < 14 {
		return nil, errors.New("short cmap format 4 subtable")
	}
	segX2 := int(binary.BigEndian.Uint16(sub[6:]))
	ends, starts, deltas, offsets := 14, 16+segX2, 16+2*segX2, 16+3*segX2
	if offsets+segX2 > len(sub) {
		return nil, errors.New("truncated cmap format 4 subtable")
	}
	b := be(sub)
	var cov coverage
	for i := 0; i < segX2; i += 2 {
		start, end := b.u16(starts+i), b.u16(ends+i)
		delta, ro := b.u16(deltas+i), b.u16(offsets+i)
		if start > end || start == 0xFFFF {
			continue
		}
		run := -1 // Start of the current run of mapped runes, or -1.
		for c := start; c <= end; c++ {
			var g int
			if ro == 0 {
				g = (c + delta) & 0xFFFF
			} else if g = b.u16(offsets + i + ro + 2*(c-start)); g != 0 {
				g = (g + delta) & 0xFFFF
			}
			switch {
			case g != 0 && run < 0:
				run = c
			case g == 0 && run >= 0:
				cov = append(cov, runeRange{rune(run), rune(c - 1)}) //nolint:gosec // At most 0xFFFF.
				run = -1
			}
		}
		if run >= 0 {
			cov = append(cov, runeRange{rune(run), rune(end)}) //nolint:gosec // At most 0xFFFF.
		}
	}
	return cov.normalized(), nil
}
