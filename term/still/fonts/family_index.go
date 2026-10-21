// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fonts

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"unicode/utf16"
)

// face describes one font face as recorded in the font file itself (the name
// and OS/2 tables). The filename is never consulted.
type face struct {
	src      string   // "embed:<rel>" or "file:<abs>".
	index    int      // Face index within a collection (0 for plain fonts).
	families []string // Typographic family (name ID 16) then family (name ID 1), when they differ.
	weight   int      // OS/2 usWeightClass, or derived from the style name if OS/2 is absent.
	italic   bool     // OS/2 fsSelection italic or oblique bit, or derived from the style name.
}

// boldWeight is the lightest weight treated as bold when deciding whether a
// face is a real bold variant.
const boldWeight = 600

func (f face) bold() bool { return f.weight >= boldWeight }

// familyIndex maps normalized family names to the faces in that family, in
// precedence order (earlier entries win ties).
type familyIndex struct {
	faces   map[string][]face
	display map[string]string // Normalized name to the name as written in the font.
}

func (ix *familyIndex) add(f face) {
	for _, name := range f.families {
		key := normalizeFamily(name)
		ix.faces[key] = append(ix.faces[key], f)
		if _, ok := ix.display[key]; !ok {
			ix.display[key] = strings.Join(strings.Fields(name), " ")
		}
	}
}

// names returns the sorted family names as written in the fonts.
func (ix *familyIndex) names() []string {
	return slices.SortedFunc(maps.Values(ix.display), compareFold)
}

func compareFold(a, b string) int {
	return strings.Compare(strings.ToLower(a), strings.ToLower(b))
}

// normalizeFamily folds a family name for comparison the way CSS does for
// font-family: case-insensitive, with runs of whitespace collapsed.
func normalizeFamily(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

// readFaces reads the face metadata of every face in a font (or collection)
// using only the sfnt table directory plus the name and OS/2 tables, so glyph
// data is never read.
func readFaces(r io.ReaderAt, src string) ([]face, error) {
	var head [12]byte
	if _, err := r.ReadAt(head[:], 0); err != nil {
		return nil, err
	}

	offsets := []uint32{0}
	if string(head[:4]) == "ttcf" {
		n := binary.BigEndian.Uint32(head[8:12])
		if n == 0 || n > 1024 {
			return nil, errors.New("invalid collection size")
		}
		raw := make([]byte, 4*n)
		if _, err := r.ReadAt(raw, 12); err != nil {
			return nil, err
		}
		offsets = offsets[:0]
		for i := range n {
			offsets = append(offsets, binary.BigEndian.Uint32(raw[4*i:]))
		}
	}

	out := make([]face, 0, len(offsets))
	for i, off := range offsets {
		f, err := readFace(r, int64(off))
		if err != nil {
			return nil, err
		}
		f.src = src
		f.index = i
		out = append(out, f)
	}
	return out, nil
}

func readFace(r io.ReaderAt, base int64) (face, error) {
	var hdr [12]byte
	if _, err := r.ReadAt(hdr[:], base); err != nil {
		return face{}, err
	}
	switch binary.BigEndian.Uint32(hdr[:4]) {
	case 0x00010000, 0x4f54544f, 0x74727565, 0x74797031: // 1.0, "OTTO", "true", "typ1".
	default:
		return face{}, errors.New("not an sfnt font")
	}

	numTables := int(binary.BigEndian.Uint16(hdr[4:6]))
	dir := make([]byte, 16*numTables)
	if _, err := r.ReadAt(dir, base+12); err != nil {
		return face{}, err
	}

	var nameOff, nameLen, os2Off, os2Len uint32
	for i := range numTables {
		rec := dir[16*i:]
		switch string(rec[:4]) {
		case "name":
			nameOff, nameLen = binary.BigEndian.Uint32(rec[8:]), binary.BigEndian.Uint32(rec[12:])
		case "OS/2":
			os2Off, os2Len = binary.BigEndian.Uint32(rec[8:]), binary.BigEndian.Uint32(rec[12:])
		}
	}
	if nameLen == 0 || nameLen > 4<<20 {
		return face{}, errors.New("missing or oversized name table")
	}

	tbl := make([]byte, nameLen)
	if _, err := r.ReadAt(tbl, int64(nameOff)); err != nil {
		return face{}, err
	}

	var f face
	for _, id := range []uint16{16, 1} {
		name := nameString(tbl, id)
		key := normalizeFamily(name)
		if key != "" && !slices.ContainsFunc(f.families, func(o string) bool { return normalizeFamily(o) == key }) {
			f.families = append(f.families, name)
		}
	}
	if len(f.families) == 0 {
		return face{}, errors.New("font has no family name")
	}

	if os2Len >= 64 {
		var os2 [64]byte
		if _, err := r.ReadAt(os2[:], int64(os2Off)); err == nil {
			f.weight = int(binary.BigEndian.Uint16(os2[4:]))
			sel := binary.BigEndian.Uint16(os2[62:])
			f.italic = sel&(1<<0) != 0 || sel&(1<<9) != 0 // Italic, oblique.
		}
	}
	if f.weight < 1 || f.weight > 1000 {
		style := nameString(tbl, 17)
		if style == "" {
			style = nameString(tbl, 2)
		}
		weight, italic := weightItalicFromStyle(style)
		f.weight, f.italic = weight, f.italic || italic
	}
	return f, nil
}

// styleWeights maps the weight words used in style names to CSS weights.
var styleWeights = map[string]int{
	"thin": 100, "hairline": 100,
	"extralight": 200, "ultralight": 200,
	"light": 300,
	"":      400, "regular": 400, "normal": 400, "book": 400, "roman": 400,
	"medium":   500,
	"semibold": 600, "demibold": 600,
	"bold":      700,
	"extrabold": 800, "ultrabold": 800,
	"black": 900, "heavy": 900,
}

var styleSeparators = strings.NewReplacer(" ", "", "-", "", "_", "")

// weightItalicFromStyle derives weight and italic from a style name such as
// "Bold Italic". It is only used for the rare fonts without an OS/2 table.
// Unrecognized weight words are treated as regular.
func weightItalicFromStyle(style string) (weight int, italic bool) {
	s := styleSeparators.Replace(strings.ToLower(style))
	for _, word := range []string{"italic", "oblique"} {
		if strings.Contains(s, word) {
			italic = true
			s = strings.ReplaceAll(s, word, "")
		}
	}
	if weight, ok := styleWeights[s]; ok {
		return weight, italic
	}
	return 400, italic
}

// nameString returns the best string for nameID in a raw name table, preferring
// Windows/English, then any Windows, then Unicode, then Macintosh Roman.
func nameString(tbl []byte, id uint16) string {
	if len(tbl) < 6 {
		return ""
	}
	count := int(binary.BigEndian.Uint16(tbl[2:]))
	strOff := int(binary.BigEndian.Uint16(tbl[4:]))

	bestScore := 0
	var best []byte
	var bestUTF16 bool
	for i := range count {
		rec := 6 + 12*i
		if rec+12 > len(tbl) {
			break
		}
		platform := binary.BigEndian.Uint16(tbl[rec:])
		lang := binary.BigEndian.Uint16(tbl[rec+4:])
		if binary.BigEndian.Uint16(tbl[rec+6:]) != id {
			continue
		}
		var score int
		switch {
		case platform == 3 && lang == 0x409:
			score = 4
		case platform == 3:
			score = 3
		case platform == 0:
			score = 2
		case platform == 1 && lang == 0:
			score = 1
		}
		if score <= bestScore {
			continue
		}
		length := int(binary.BigEndian.Uint16(tbl[rec+8:]))
		off := strOff + int(binary.BigEndian.Uint16(tbl[rec+10:]))
		if off+length > len(tbl) {
			continue
		}
		bestScore, best, bestUTF16 = score, tbl[off:off+length], platform != 1
	}
	if best == nil {
		return ""
	}
	if !bestUTF16 {
		return string(best) // Mac Roman; the ASCII subset covers family/style names.
	}
	u := make([]uint16, len(best)/2)
	for i := range u {
		u[i] = binary.BigEndian.Uint16(best[2*i:])
	}
	return string(utf16.Decode(u))
}

// readSourceFaces reads the face metadata of an "embed:" or "file:" source.
// Files are read with ReadAt so glyph data is never touched.
func readSourceFaces(src string) ([]face, error) {
	if strings.HasPrefix(src, "embed:") {
		raw, err := readSource(src)
		if err != nil {
			return nil, err
		}
		return readFaces(bytes.NewReader(raw), src)
	}
	f, err := os.Open(strings.TrimPrefix(src, "file:"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readFaces(f, src)
}

// buildIndex reads every source concurrently and indexes the faces in source
// order, so precedence is deterministic. Sources that fail to read or that are
// not valid fonts are skipped. More workers than CPUs are used since the work
// is mostly file latency (network mounts, WSL's /mnt/c).
func buildIndex(srcs []string) *familyIndex {
	results := make([][]face, len(srcs))
	next := make(chan int)

	var wg sync.WaitGroup
	for range 2 * runtime.GOMAXPROCS(0) {
		wg.Go(func() {
			for i := range next {
				results[i], _ = readSourceFaces(srcs[i])
			}
		})
	}
	for i := range srcs {
		next <- i
	}
	close(next)
	wg.Wait()

	ix := &familyIndex{faces: map[string][]face{}, display: map[string]string{}}
	for _, faces := range results {
		for _, f := range faces {
			ix.add(f)
		}
	}
	return ix
}

// embeddedFamilyIndex indexes the bundled fonts. It is built once on first use.
var embeddedFamilyIndex = sync.OnceValue(func() *familyIndex {
	entries, _ := fs.ReadDir(embeddedFonts, "data")
	var srcs []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".gz") {
			srcs = append(srcs, "embed:data/"+e.Name())
		}
	}
	return buildIndex(srcs)
})

// systemFamilyIndex indexes the installed fonts. It is built on first use, so
// fonts installed after the first lookup are not seen by the running process.
var systemFamilyIndex = sync.OnceValue(buildSystemIndex)

func buildSystemIndex() *familyIndex {
	return buildIndex(systemFontFiles(Dirs()))
}

// systemFontFiles walks dirs in order and returns the font files found,
// deduplicated, as "file:<path>" sources.
func systemFontFiles(dirs []string) []string {
	var srcs []string
	seen := map[string]struct{}{}
	for _, dir := range dirs {
		_ = filepath.WalkDir(dir, func(path string, de fs.DirEntry, err error) error {
			if err != nil || de.IsDir() {
				return nil //nolint:nilerr // Ignore errors and continue searching.
			}
			switch strings.ToLower(filepath.Ext(path)) {
			case ".ttf", ".otf", ".ttc", ".otc":
			default:
				return nil
			}
			if _, ok := seen[path]; !ok {
				seen[path] = struct{}{}
				srcs = append(srcs, "file:"+path)
			}
			return nil
		})
	}
	return srcs
}
