// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/lrstanley/x/term/reel/vt/internal/wasmvt"
)

// Glyph Protocol registrations are libghostty-vt's own, read through
// ghostty_terminal_glyph_glossary* (see vt/build/glyph-glossary.patch). The
// emulator validates them, answers the program, and keeps each register
// command, which is decoded here. It only accepts the glyf format.

// GlyphSize is how a [Glyph] is scaled into its cells.
type GlyphSize uint8

// Glyph Protocol size modes.
const (
	// GlyphSizeHeight scales the line height to the cell height.
	GlyphSizeHeight GlyphSize = iota
	// GlyphSizeAdvance scales the advance to the span width.
	GlyphSizeAdvance
	// GlyphSizeContain scales the authored extent to fit inside the span.
	GlyphSizeContain
	// GlyphSizeCover scales the authored extent to fill the span.
	GlyphSizeCover
	// GlyphSizeStretch scales each axis to the span on its own.
	GlyphSizeStretch
)

// GlyphAlign is where a [Glyph]'s scaled extent sits in its span, on one axis.
type GlyphAlign uint8

// Glyph Protocol alignments. Start is the left or bottom edge, and End the
// right or top one.
const (
	GlyphAlignCenter GlyphAlign = iota
	GlyphAlignStart
	GlyphAlignEnd
	// GlyphAlignBaseline puts y=0 on the font baseline. It is only vertical.
	GlyphAlignBaseline
)

// GlyphPoint is one outline point, in design units.
type GlyphPoint struct {
	X, Y    int32
	OnCurve bool
}

// Glyph is a glyph a program registered with the Glyph Protocol, which a
// renderer draws in place of the font's glyph for its rune, in the cell's
// foreground color, across Cells cells. Its other fields are the options the
// program sent, or their defaults, which the protocol's sizing rules (§8.5)
// place the outline by. Renderers may place it otherwise: Rio, the reference
// terminal, and reel's renderer use only UnitsPerEm and Cells, and the
// outline's bounds.
type Glyph struct {
	// Rune is the Private Use Area codepoint the glyph is drawn for.
	Rune rune
	// Contours are the outline's closed TrueType contours, in design units.
	// Two off-curve points in a row have an on-curve point between them.
	Contours [][]GlyphPoint
	// UnitsPerEm is the design units in one em.
	UnitsPerEm int
	// Advance and LineHeight are the authored extent, in design units: from
	// x=0, and from the descender to the ascender, Y up with the baseline at
	// y=0.
	Advance, LineHeight int
	// Cells is how many cells the glyph is drawn across, 1 or 2. The rune
	// still takes one: the program leaves the next cell blank.
	Cells int
	// Size, AlignX, and AlignY place the authored extent in the span, Cells
	// cells less Pad.
	Size           GlyphSize
	AlignX, AlignY GlyphAlign
	// Pad is the span's insets as fractions: top and bottom of the cell
	// height, right and left of the span width.
	Pad [4]float64

	// raw is the register command the glyph came from, without its APC
	// framing.
	raw string
}

// Glyphs is the Glyph Protocol glossary: the glyphs a program registered.
// It is shared between states and must not be modified.
type Glyphs struct {
	// order is oldest first, the order the emulator evicts in.
	order  []*Glyph
	byRune map[rune]*Glyph
}

// Lookup returns the glyph registered for r, or nil. A nil g has none.
func (g *Glyphs) Lookup(r rune) *Glyph {
	if g == nil {
		return nil
	}
	return g.byRune[r]
}

// glyphsLocked returns the glossary, nil when it is empty. It is t.glyphs
// while the emulator's glossary generation holds, or its glyphs are all the
// same, so states share it. An unchanged glyph keeps its pointer, which
// renderers cache by.
func (t *Terminal) glyphsLocked() *Glyphs {
	if t.glyphsFresh {
		return t.glyphs
	}
	s := t.scratch
	t.mod.Zero(s, 16)
	if t.mod.Xghostty_terminal_glyph_glossary(t.term, s, s+8) != 0 {
		return t.glyphs
	}
	t.glyphsFresh = true
	n, gen := int(t.mod.U32(s, 0)), t.mod.U64(s, 8)
	if gen == t.glyphsGen {
		return t.glyphs
	}
	t.glyphsGen = gen
	var prev []*Glyph
	if t.glyphs != nil {
		prev = t.glyphs.order
	}
	next := &Glyphs{order: make([]*Glyph, 0, n), byRune: make(map[rune]*Glyph, n)}
	for i := range n {
		// Entries the emulator stored always read and decode: skipping one
		// only guards against a disagreement between the two.
		if t.mod.Xghostty_terminal_glyph_glossary_entry(t.term, wasmvt.Int32(i), s, s+4, s+8) != 0 {
			continue
		}
		cp := rune(t.mod.U32(s, 0)) //nolint:gosec // a Unicode codepoint.
		raw := t.mod.Slice(t.mod.I32(s, 4), int(t.mod.U32(s, 8)))
		gl := t.glyphs.Lookup(cp)
		if gl == nil || gl.raw != string(raw) {
			var err error
			if gl, err = decodeGlyph(cp, raw); err != nil {
				continue
			}
		}
		next.order = append(next.order, gl)
		next.byRune[cp] = gl
	}
	switch {
	case slices.Equal(next.order, prev):
	case len(next.order) == 0:
		t.glyphs = nil
	default:
		t.glyphs = next
	}
	return t.glyphs
}

// glyphsVT is the register commands of g, oldest first, so a fresh terminal
// holds the same glossary in the same order. They ask for no reply.
func glyphsVT(g *Glyphs) string {
	if g == nil {
		return ""
	}
	var b strings.Builder
	for _, gl := range g.order {
		at := strings.LastIndexByte(gl.raw, ';')
		b.WriteString("\x1b_25a1;")
		for opt := range strings.SplitSeq(gl.raw[:at], ";") {
			if !strings.HasPrefix(opt, "reply=") {
				b.WriteString(opt)
				b.WriteByte(';')
			}
		}
		b.WriteString("reply=0")
		b.WriteString(gl.raw[at:])
		b.WriteString("\x1b\\")
	}
	return b.String()
}

// errGlyph is a register command libghostty-vt accepted but this decoder
// cannot read, which only a disagreement between the two produces.
var errGlyph = errors.New("vt: malformed glyph registration")

// decodeGlyph decodes the register command raw ("r;key=value;...;payload")
// for cp. The emulator has already validated it, so options it accepted
// parse, and its defaults apply to those left out.
func decodeGlyph(cp rune, raw []byte) (*Glyph, error) {
	payloadAt := bytes.LastIndexByte(raw, ';')
	if !bytes.HasPrefix(raw, []byte("r;")) || payloadAt < 2 {
		return nil, errGlyph
	}
	opts := map[string]string{}
	for opt := range strings.SplitSeq(string(raw[2:payloadAt]), ";") {
		if k, v, ok := strings.Cut(opt, "="); ok {
			opts[k] = v // The last one wins, as in the emulator.
		}
	}
	num := func(key string, def int) int {
		if v, err := strconv.Atoi(opts[key]); err == nil {
			return v
		}
		return def
	}
	upm := num("upm", 1000)
	gl := &Glyph{
		Rune:       cp,
		UnitsPerEm: upm,
		Advance:    num("aw", upm),
		LineHeight: num("lh", upm),
		Cells:      num("width", 1),
		Size:       glyphSizes[opts["size"]],
		raw:        string(raw),
	}
	if h, v, ok := strings.Cut(opts["align"], ","); ok {
		gl.AlignX, gl.AlignY = glyphAligns[h], glyphAligns[v]
	}
	if v, ok := opts["pad"]; ok {
		for i, f := range strings.SplitN(v, ",", 4) {
			gl.Pad[i], _ = strconv.ParseFloat(f, 64)
		}
	}
	data, err := base64.StdEncoding.DecodeString(string(raw[payloadAt+1:]))
	if err != nil {
		return nil, errGlyph
	}
	if gl.Contours, err = decodeGlyf(data); err != nil {
		return nil, err
	}
	return gl, nil
}

// glyphSizes and glyphAligns are the protocol's names for the size modes
// and alignments. The emulator only accepts these, and baseline only as the
// vertical one.
var (
	glyphSizes = map[string]GlyphSize{
		"height": GlyphSizeHeight, "advance": GlyphSizeAdvance, "contain": GlyphSizeContain,
		"cover": GlyphSizeCover, "stretch": GlyphSizeStretch,
	}
	glyphAligns = map[string]GlyphAlign{
		"center": GlyphAlignCenter, "start": GlyphAlignStart, "end": GlyphAlignEnd,
		"baseline": GlyphAlignBaseline,
	}
)

// glyf simple-glyph flag bits.
const (
	glyfOnCurve = 1 << iota
	glyfXShort
	glyfYShort
	glyfRepeat
	glyfXSame // or, with glyfXShort, positive
	glyfYSame // or, with glyfYShort, positive
)

// decodeGlyf decodes an OpenType glyf simple-glyph record into its contours.
func decodeGlyf(data []byte) ([][]GlyphPoint, error) {
	r := glyfReader{b: data}
	contours := int(int16(r.u16())) //nolint:gosec // numberOfContours is signed.
	r.skip(8)                       // The bounding box.
	if contours < 0 || r.bad {
		return nil, errGlyph
	}
	ends := make([]int, contours)
	for i := range ends {
		ends[i] = int(r.u16())
	}
	r.skip(int(r.u16())) // Instructions, which the emulator refuses.
	if r.bad {
		return nil, errGlyph
	}
	n := 0
	if contours > 0 {
		n = ends[contours-1] + 1
	}
	flags := make([]byte, 0, n)
	for len(flags) < n {
		f := r.u8()
		flags = append(flags, f)
		if f&glyfRepeat != 0 {
			for range r.u8() {
				flags = append(flags, f)
			}
		}
		if r.bad {
			return nil, errGlyph
		}
	}
	flags = flags[:n]
	points := make([]GlyphPoint, n)
	for axis, bits := range [2][2]byte{{glyfXShort, glyfXSame}, {glyfYShort, glyfYSame}} {
		short, same := bits[0], bits[1]
		var at int32
		for i, f := range flags {
			switch {
			case f&short != 0 && f&same != 0:
				at += int32(r.u8())
			case f&short != 0:
				at -= int32(r.u8())
			case f&same == 0:
				at += int32(int16(r.u16())) //nolint:gosec // deltas are signed.
			}
			if axis == 0 {
				points[i].X = at
			} else {
				points[i].Y, points[i].OnCurve = at, f&glyfOnCurve != 0
			}
		}
	}
	if r.bad {
		return nil, errGlyph
	}
	out := make([][]GlyphPoint, 0, contours)
	start := 0
	for _, end := range ends {
		if end < start || end >= n {
			return nil, errGlyph
		}
		out = append(out, points[start:end+1:end+1])
		start = end + 1
	}
	return out, nil
}

// glyfReader reads big-endian fields, and is bad once it runs out.
type glyfReader struct {
	b   []byte
	bad bool
}

func (r *glyfReader) skip(n int) {
	if n > len(r.b) {
		r.b, r.bad = nil, true
		return
	}
	r.b = r.b[n:]
}

func (r *glyfReader) u8() byte {
	if len(r.b) < 1 {
		r.bad = true
		return 0
	}
	v := r.b[0]
	r.b = r.b[1:]
	return v
}

func (r *glyfReader) u16() uint16 {
	if len(r.b) < 2 {
		r.bad = true
		return 0
	}
	v := binary.BigEndian.Uint16(r.b)
	r.b = r.b[2:]
	return v
}
