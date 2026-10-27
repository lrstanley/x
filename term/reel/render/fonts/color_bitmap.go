// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fonts

import (
	"bytes"
	"image"
	_ "image/jpeg" // Registers the decoder for sbix JPEG glyphs.
	_ "image/png"  // Registers the decoder for CBDT and sbix PNG glyphs.
	"io"
	"slices"

	xdraw "golang.org/x/image/draw"
)

// maxGlyphRecord bounds one bitmap glyph read from a font.
const maxGlyphRecord = 4 << 20

// colorSource draws the color glyphs of one font.
type colorSource interface {
	// has reports whether the font has a color glyph for gid.
	has(gid uint16) bool

	// render draws gid fitted into a w×h box, centered, or returns nil.
	render(gid uint16, w, h int) *image.RGBA
}

// fitImage scales src to fit a w×h box, keeping its aspect ratio, and centers
// it.
func fitImage(src image.Image, w, h int) *image.RGBA {
	sb := src.Bounds()
	if sb.Empty() || w <= 0 || h <= 0 {
		return nil
	}
	dw, dh := w, sb.Dy()*w/sb.Dx()
	if dh > h {
		dw, dh = sb.Dx()*h/sb.Dy(), h
	}
	dw, dh = max(dw, 1), max(dh, 1)
	x, y := (w-dw)/2, (h-dh)/2
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, image.Rect(x, y, x+dw, y+dh), src, sb, xdraw.Over, nil)
	return dst
}

// decodeBitmap fits encoded image data (PNG or JPEG) into a w×h box.
func decodeBitmap(data []byte, w, h int) *image.RGBA {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	return fitImage(src, w, h)
}

// readAt reads n bytes at off, or returns nil.
func readAt(r io.ReaderAt, off, n int) []byte {
	if n <= 0 || n > maxGlyphRecord {
		return nil
	}
	buf := make([]byte, n)
	if _, err := r.ReadAt(buf, int64(off)); err != nil {
		return nil
	}
	return buf
}

// cbdtSource reads Google's color bitmap tables (CBLC locates the glyphs, CBDT
// holds them), as used by Noto Color Emoji.
type cbdtSource struct {
	r       io.ReaderAt
	cblc    be
	dataOff int // CBDT offset in the file.
	dataLen int
	strikes []cbdtStrike // By ascending ppem.
}

type cbdtStrike struct {
	ppem     int
	arrayOff int // Index subtable array, in cblc.
	count    int
}

func newCBDTSource(r io.ReaderAt, dir map[string]tableRec) colorSource {
	cblc, cbdt := readTable(r, dir, "CBLC"), dir["CBDT"]
	if len(cblc) < 8 || cbdt.length == 0 {
		return nil
	}
	s := &cbdtSource{r: r, cblc: cblc, dataOff: cbdt.off, dataLen: cbdt.length}
	for i := range min(cblc.u32(4), (len(cblc)-8)/48) {
		rec := 8 + 48*i // A BitmapSize record.
		s.strikes = append(s.strikes, cbdtStrike{
			ppem:     cblc.u8(rec + 44),
			arrayOff: cblc.u32(rec),
			count:    cblc.u32(rec + 8),
		})
	}
	slices.SortStableFunc(s.strikes, func(a, b cbdtStrike) int { return a.ppem - b.ppem })
	return s
}

// find locates the bitmap record of gid in strike: its offset relative to the
// CBDT table, its length, and its image format.
func (s *cbdtSource) find(st cbdtStrike, gid uint16) (off, length, format int, ok bool) {
	c := s.cblc
	g := int(gid)
	for i := range min(st.count, (len(c)-st.arrayOff)/8) {
		rec := st.arrayOff + 8*i
		first, last := c.u16(rec), c.u16(rec+2)
		if g < first || g > last {
			continue
		}
		sub := st.arrayOff + c.u32(rec+4)
		var base int
		format, base = c.u16(sub+2), c.u32(sub+4)
		idx := g - first
		switch c.u16(sub) { // Index format.
		case 1:
			off, length = c.u32(sub+8+4*idx), c.u32(sub+8+4*(idx+1))-c.u32(sub+8+4*idx)
		case 3:
			off, length = c.u16(sub+8+2*idx), c.u16(sub+8+2*(idx+1))-c.u16(sub+8+2*idx)
		default:
			return 0, 0, 0, false
		}
		off += base
		return off, length, format, length > 0 && off+length <= s.dataLen && (format == 17 || format == 18 || format == 19)
	}
	return 0, 0, 0, false
}

func (s *cbdtSource) has(gid uint16) bool {
	return slices.ContainsFunc(s.strikes, func(st cbdtStrike) bool {
		_, _, _, ok := s.find(st, gid)
		return ok
	})
}

func (s *cbdtSource) render(gid uint16, w, h int) *image.RGBA {
	// The smallest strike at least as large as the box, else the largest.
	var off, length, format int
	var found bool
	for _, st := range s.strikes {
		o, l, f, ok := s.find(st, gid)
		if !ok {
			continue
		}
		off, length, format, found = o, l, f, true
		if st.ppem >= max(w, h) {
			break
		}
	}
	if !found {
		return nil
	}
	rec := be(readAt(s.r, s.dataOff+off, length))
	var skip int // Bytes before the PNG's length field: the glyph metrics.
	switch format {
	case 17:
		skip = 5 // Small metrics.
	case 18:
		skip = 8 // Big metrics.
	}
	n := rec.u32(skip)
	if !rec.has(skip+4, n) {
		return nil
	}
	return decodeBitmap(rec[skip+4:skip+4+n], w, h)
}

// sbixSource reads Apple's standard bitmap graphics table, as used by Apple
// Color Emoji.
type sbixSource struct {
	r         io.ReaderAt
	off       int
	numGlyphs int
	strikes   []sbixStrike // By ascending ppem.
}

type sbixStrike struct {
	ppem int
	off  int // Relative to the table.
}

func newSbixSource(r io.ReaderAt, dir map[string]tableRec, numGlyphs int) colorSource {
	rec, ok := dir["sbix"]
	if !ok || rec.length < 8 || numGlyphs == 0 {
		return nil
	}
	head := be(readAt(r, rec.off, 8))
	if head == nil {
		return nil
	}
	s := &sbixSource{r: r, off: rec.off, numGlyphs: numGlyphs}
	offsets := be(readAt(r, rec.off+8, 4*min(head.u32(4), 256)))
	for i := range len(offsets) / 4 {
		off := offsets.u32(4 * i)
		if strike := be(readAt(r, rec.off+off, 4)); strike != nil {
			s.strikes = append(s.strikes, sbixStrike{ppem: strike.u16(0), off: off})
		}
	}
	slices.SortStableFunc(s.strikes, func(a, b sbixStrike) int { return a.ppem - b.ppem })
	return s
}

// locate finds the image of gid in strike: its type, and where its data is in
// the file. It follows "dupe" references and "flip" references, which are the
// glyph they name mirrored left to right (Apple uses them for emoji that face
// the other way); flipped reports whether the image is to be mirrored. Only
// headers are read.
func (s *sbixSource) locate(st sbixStrike, gid uint16) (typ string, at, length int, flipped bool) {
	for range 4 {
		if int(gid) >= s.numGlyphs {
			return "", 0, 0, false
		}
		offs := be(readAt(s.r, s.off+st.off+4+4*int(gid), 8))
		if offs == nil {
			return "", 0, 0, false
		}
		start, end := offs.u32(0), offs.u32(4)
		if end-start <= 8 {
			return "", 0, 0, false
		}
		// The record: origin offsets, the type, then the data, which is the
		// glyph to repeat or mirror for those types.
		head := be(readAt(s.r, s.off+st.off+start, 10))
		if head == nil {
			return "", 0, 0, false
		}
		switch typ = string(head[4:8]); typ {
		case "dupe":
		case "flip":
			flipped = !flipped
		default:
			return typ, s.off + st.off + start + 8, end - start - 8, flipped
		}
		gid = uint16(head.u16(8)) //nolint:gosec // 16 bits read.
	}
	return "", 0, 0, false
}

func supportedSbixType(typ string) bool { return typ == "png " || typ == "jpg " }

func (s *sbixSource) has(gid uint16) bool {
	return slices.ContainsFunc(s.strikes, func(st sbixStrike) bool {
		typ, _, _, _ := s.locate(st, gid)
		return supportedSbixType(typ)
	})
}

func (s *sbixSource) render(gid uint16, w, h int) *image.RGBA {
	// The smallest strike at least as large as the box, else the largest.
	var at, length int
	var flipped, found bool
	for _, st := range s.strikes {
		typ, a, l, f := s.locate(st, gid)
		if !supportedSbixType(typ) {
			continue
		}
		at, length, flipped, found = a, l, f, true
		if st.ppem >= max(w, h) {
			break
		}
	}
	if !found {
		return nil
	}
	img := decodeBitmap(readAt(s.r, at, length), w, h)
	if flipped && img != nil {
		mirror(img)
	}
	return img
}

func mirror(img *image.RGBA) {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		row := img.Pix[(y-b.Min.Y)*img.Stride : (y-b.Min.Y)*img.Stride+4*b.Dx()]
		for l, r := 0, b.Dx()-1; l < r; l, r = l+1, r-1 {
			for c := range 4 {
				row[4*l+c], row[4*r+c] = row[4*r+c], row[4*l+c]
			}
		}
	}
}
