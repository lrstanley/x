// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fonts

import (
	"image"
	"image/color"
	"io"
	"sort"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

// colrSource draws version 0 COLR glyphs: a stack of outline glyphs, each
// filled with a CPAL palette color, as used by Segoe UI Emoji and Twemoji.
// Version 1 glyphs (gradients, transforms, compositing) are not supported.
type colrSource struct {
	font    *sfnt.Font
	colr    be
	palette []color.NRGBA // The first CPAL palette.
}

func newCOLRSource(r io.ReaderAt, dir map[string]tableRec, index int) colorSource {
	colr, cpal := readTable(r, dir, "COLR"), readTable(r, dir, "CPAL")
	if len(colr) < 14 || colr.u16(2) == 0 || len(cpal) < 14 {
		return nil
	}
	f, err := parseReaderAt(r, index)
	if err != nil {
		return nil
	}

	s := &colrSource{font: f, colr: colr}
	first, records := cpal.u16(12), cpal.u32(8)
	for i := range cpal.u16(2) { // Palette 0 entries; records are BGRA.
		at := records + 4*(first+i)
		if !cpal.has(at, 4) {
			break
		}
		s.palette = append(s.palette, color.NRGBA{R: cpal[at+2], G: cpal[at+1], B: cpal[at], A: cpal[at+3]})
	}
	return s
}

// layers returns the first layer record and the layer count of gid.
func (s *colrSource) layers(gid uint16) (first, n int, ok bool) {
	base, count := s.colr.u32(4), s.colr.u16(2)
	i := sort.Search(count, func(i int) bool { return s.colr.u16(base+6*i) >= int(gid) })
	if i == count || s.colr.u16(base+6*i) != int(gid) {
		return 0, 0, false
	}
	first, n = s.colr.u16(base+6*i+2), s.colr.u16(base+6*i+4)
	return first, n, n > 0
}

func (s *colrSource) has(gid uint16) bool {
	_, _, ok := s.layers(gid)
	return ok
}

func (s *colrSource) render(gid uint16, w, h int) *image.RGBA {
	first, n, ok := s.layers(gid)
	if !ok || w <= 0 || h <= 0 {
		return nil
	}

	// Fit the glyph's advance by the font's line height into the box, at the
	// ppem that scales the em to it.
	var buf sfnt.Buffer
	upem := fixed.I(int(s.font.UnitsPerEm()))
	advance, err := s.font.GlyphAdvance(&buf, sfnt.GlyphIndex(gid), upem, font.HintingNone)
	if err != nil || advance <= 0 {
		return nil
	}
	m, err := s.font.Metrics(&buf, upem, font.HintingNone)
	if err != nil || m.Ascent+m.Descent <= 0 {
		return nil
	}
	scale := min(float64(w)/fixedF(advance), float64(h)/fixedF(m.Ascent+m.Descent))
	ppem := fixed.Int26_6(scale * float64(s.font.UnitsPerEm()) * 64) //nolint:gosec // Bounded by the box.
	x0 := (float64(w) - fixedF(advance)*scale) / 2
	y0 := (float64(h)-fixedF(m.Ascent+m.Descent)*scale)/2 + fixedF(m.Ascent)*scale

	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	ras := vector.NewRasterizer(w, h)
	ras.DrawOp = xdraw.Over
	for i := range n {
		rec := s.colr.u32(8) + 4*(first+i)                                // A layer record: the glyph, then its palette entry.
		layer, pal := sfnt.GlyphIndex(s.colr.u16(rec)), s.colr.u16(rec+2) //nolint:gosec // 16 bits read.
		fill := color.NRGBA{A: 255}                                       // 0xFFFF is the text color: black.
		if pal < len(s.palette) {
			fill = s.palette[pal]
		}
		segs, loadErr := s.font.LoadGlyph(&buf, layer, ppem, nil)
		if loadErr != nil {
			continue
		}
		ras.Reset(w, h)
		for _, seg := range segs {
			pt := func(i int) (float32, float32) {
				return float32(x0 + fixedF(seg.Args[i].X)), float32(y0 + fixedF(seg.Args[i].Y))
			}
			switch seg.Op {
			case sfnt.SegmentOpMoveTo:
				ras.MoveTo(pt(0))
			case sfnt.SegmentOpLineTo:
				ras.LineTo(pt(0))
			case sfnt.SegmentOpQuadTo:
				x1, y1 := pt(0)
				x2, y2 := pt(1)
				ras.QuadTo(x1, y1, x2, y2)
			case sfnt.SegmentOpCubeTo:
				x1, y1 := pt(0)
				x2, y2 := pt(1)
				x3, y3 := pt(2)
				ras.CubeTo(x1, y1, x2, y2, x3, y3)
			}
		}
		ras.ClosePath()
		ras.Draw(dst, dst.Bounds(), image.NewUniform(fill), image.Point{})
	}
	return dst
}

func fixedF(v fixed.Int26_6) float64 { return float64(v) / 64 }
