// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fonts

import (
	"image"
	"image/draw"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"
)

// maxColorGlyphs bounds the glyphs [SystemColorGlyph] remembers. A terminal
// shows a few hundred distinct emoji at most; the cap only stops an adversarial
// stream of clusters from growing the cache without limit.
const maxColorGlyphs = 8192

// ColorGlyph is one glyph of an installed color font, drawn in the font's own
// colors rather than as a mask tinted by a foreground. Bitmap fonts (CBDT,
// sbix) are scaled; layered vector fonts (COLR version 0) are rasterized at the
// size drawn. It is safe for concurrent use.
type ColorGlyph struct {
	layers []colorLayer // At least one, drawn in order.

	mu   sync.Mutex
	imgs map[[2]int]*image.RGBA // By size. A nil image is a size the glyph can't be drawn at.
}

// maxGlyphSizes bounds the sizes a [ColorGlyph] remembers. Glyphs are shared by
// every renderer: those at other scales, and text outside the grid, draw them
// at their own size.
const maxGlyphSizes = 4

// colorLayer is one glyph of a font.
type colorLayer struct {
	src colorSource
	gid uint16
}

// Image draws the glyph fitted into a w×h box, centered and with its aspect
// ratio kept, as premultiplied RGBA. The result is shared and must not be
// modified. It returns nil if the glyph cannot be drawn.
func (g *ColorGlyph) Image(w, h int) *image.RGBA {
	g.mu.Lock()
	defer g.mu.Unlock()
	size := [2]int{w, h}
	img, ok := g.imgs[size]
	if !ok {
		if img = g.render(w, h); len(g.imgs) >= maxGlyphSizes || g.imgs == nil {
			g.imgs = make(map[[2]int]*image.RGBA, maxGlyphSizes)
		}
		g.imgs[size] = img
	}
	return img
}

// render draws the layers over each other, all fitted into the same box.
func (g *ColorGlyph) render(w, h int) *image.RGBA {
	img := g.layers[0].src.render(g.layers[0].gid, w, h)
	if img == nil {
		return nil
	}
	for _, l := range g.layers[1:] {
		if over := l.src.render(l.gid, w, h); over != nil {
			draw.Draw(img, img.Bounds(), over, image.Point{}, draw.Over)
		}
	}
	return img
}

// SystemColorGlyph returns the glyph of an installed color font for a grapheme
// cluster: an emoji, a skin tone or ZWJ sequence, a flag, a keycap. Sequences
// are composed through the font's own substitutions (GSUB, or morx in Apple's
// font), so they are drawn only if the font has them as a single glyph, or as a
// base glyph with marks drawn over it; a font that would draw the parts side by
// side is skipped, as is one that draws nothing for the cluster.
//
// CBDT and sbix bitmap glyphs and COLR version 0 layered glyphs are supported;
// fonts that only have SVG or COLR version 1 glyphs are not. Fonts are found
// the way [SystemFallback] finds them, and the first call reads the tables of
// every installed font. Fonts installed after that are not seen by the running
// process.
func SystemColorGlyph(cluster string) (*ColorGlyph, bool) {
	return lookupColorCached(&systemColorGlyphs, findSystemColor, cluster)
}

func findSystemColor(cluster []rune) *ColorGlyph {
	g, _ := lookupColor(systemColorIndex(), cluster)
	return g
}

// colorGlyphCache remembers lookups, including failures.
type colorGlyphCache struct {
	mu sync.Mutex
	m  map[string]*ColorGlyph // A nil glyph is a cluster no font has.
}

var systemColorGlyphs colorGlyphCache

// lookupColorCached returns the glyph find returns for cluster, which may be
// emoji, once. It checks the cache before anything else: it is asked for every
// wide cell of every frame.
func lookupColorCached(c *colorGlyphCache, find func(cluster []rune) *ColorGlyph, cluster string) (*ColorGlyph, bool) {
	if !mayBeEmoji(cluster) {
		return nil, false
	}
	c.mu.Lock()
	g, ok := c.m[cluster]
	c.mu.Unlock()
	if !ok {
		runes, valid := clusterRunes(cluster)
		if !valid {
			return nil, false
		}
		g = find(runes)
		c.mu.Lock()
		if c.m == nil || len(c.m) >= maxColorGlyphs {
			c.m = map[string]*ColorGlyph{}
		}
		c.m[cluster] = g
		c.mu.Unlock()
	}
	return g, g != nil
}

// systemColorIndex holds the system faces with color glyphs. It is built on
// first use.
var systemColorIndex = sync.OnceValue(func() []*colorFace {
	return buildColorIndex(systemFontPaths())
})

// colorFace is a font face with color glyphs. Its tables are read when a
// cluster is first looked up in it.
type colorFace struct {
	face
	open func() *openColorFace // Nil if the face can't draw color glyphs.
}

// buildColorIndex reads the color faces of every source, in source order.
// Sources that fail to read are skipped.
func buildColorIndex(srcs []string) []*colorFace {
	results := make([][]*colorFace, len(srcs))
	parallelFor(len(srcs), func(i int) {
		results[i] = readColorFaces(srcs[i])
	})
	return slices.Concat(results...)
}

// readColorFaces reads the faces of a "file:" source that have color glyphs.
func readColorFaces(src string) []*colorFace {
	file, err := os.Open(strings.TrimPrefix(src, "file:"))
	if err != nil {
		return nil
	}
	defer file.Close()

	var out []*colorFace
	for _, f := range colorFaces(file, src) {
		out = append(out, &colorFace{face: f, open: sync.OnceValue(func() *openColorFace { return openColorFont(f) })})
	}
	return out
}

// colorFaces returns the faces of the font in r that have color glyphs.
func colorFaces(r io.ReaderAt, src string) []face {
	faces, _ := readFaces(r, src)
	return slices.DeleteFunc(faces, func(f face) bool { return !f.tables.color })
}

// lookupColor returns the glyph the first face that has one draws for cluster.
func lookupColor(faces []*colorFace, cluster []rune) (*ColorGlyph, bool) {
	for _, f := range faces {
		if of := f.open(); of != nil {
			if layers, ok := of.glyph(cluster); ok {
				return &ColorGlyph{layers: layers}, true
			}
		}
	}
	return nil, false
}

// openColorFace holds what is needed to find and draw the color glyphs of a
// face. Its file stays open, and glyph data is read from it on demand.
type openColorFace struct {
	glyphs  glyphLookup
	gsub    *gsub
	morx    *morx                 // Preferred over gsub, as Apple's font has only this.
	isMark  func(gid uint16) bool // From GDEF, or nil if the font has no glyph classes.
	sources []colorSource
}

func openColorFont(f face) *openColorFace {
	file, err := os.Open(strings.TrimPrefix(f.src, "file:"))
	if err != nil {
		return nil
	}
	of := openColorReader(file, f.index)
	if of == nil {
		_ = file.Close()
	}
	return of
}

// openColorReader reads the tables of face index of the font in r that are
// needed to find and draw its color glyphs. It returns nil if there are none.
func openColorReader(r io.ReaderAt, index int) *openColorFace {
	dir, err := readTableDir(r, index)
	if err != nil {
		return nil
	}
	of := &openColorFace{}
	if of.glyphs, err = newGlyphLookup(readTable(r, dir, "cmap")); err != nil {
		return nil
	}
	if tbl := readTable(r, dir, "GSUB"); tbl != nil {
		of.gsub = parseGSUB(tbl)
	}
	numGlyphs := readTable(r, dir, "maxp").u16(4)
	if tbl := readTable(r, dir, "morx"); tbl != nil {
		of.morx = parseMorx(tbl, numGlyphs)
	}
	of.isMark = markGlyphs(readTable(r, dir, "GDEF"))
	for _, src := range []colorSource{
		newSbixSource(r, dir, numGlyphs),
		newCBDTSource(r, dir),
		newCOLRSource(r, dir, index),
	} {
		if src != nil {
			of.sources = append(of.sources, src)
		}
	}
	if len(of.sources) == 0 {
		return nil
	}
	return of
}

// shape maps cluster to glyphs and composes them with the font's substitutions.
// It fails if the font lacks a rune of the cluster.
func (f *openColorFace) shape(cluster []rune) ([]uint16, bool) {
	gids := make([]uint16, 0, len(cluster))
	for _, r := range cluster {
		gid := f.glyphs(r)
		switch {
		case gid != 0:
			gids = append(gids, gid)
		case r == 0xFE0F || r == 0xFE0E: // A selector the font doesn't map.
		default:
			return nil, false
		}
	}
	switch {
	case f.morx != nil:
		gids = f.morx.shape(gids)
	case f.gsub != nil:
		gids = f.gsub.shape(gids)
	}
	return gids, true
}

// glyph shapes cluster and returns the color glyph it draws as. Fonts disagree
// on how to treat default-ignorable runes (Noto consumes a variation selector
// while shaping, Apple's font draws a blank glyph for it), so only the glyphs
// with color data count, not the number of glyphs.
//
// That is one glyph, or a base glyph with mark glyphs: Apple draws a couple
// with a heart as two halves, the second a mark that is placed over the first.
// A sequence the font can't compose has a glyph per part and is refused, rather
// than drawn as whichever part came first.
func (f *openColorFace) glyph(cluster []rune) ([]colorLayer, bool) {
	gids, ok := f.shape(cluster)
	if !ok {
		return nil, false
	}

	var layers []colorLayer
	for _, gid := range gids {
		for _, src := range f.sources {
			if src.has(gid) {
				layers = append(layers, colorLayer{src, gid})
				break
			}
		}
	}
	switch {
	case len(layers) == 0:
		return nil, false
	case len(layers) == 1:
		return layers, true
	case f.isMark == nil || f.isMark(layers[0].gid):
		return nil, false
	}
	for _, l := range layers[1:] {
		if !f.isMark(l.gid) {
			return nil, false
		}
	}
	return layers, true
}

// clusterRunes splits a grapheme cluster into runes, refusing invalid UTF-8.
func clusterRunes(s string) ([]rune, bool) {
	if s == "" || !utf8.ValidString(s) {
		return nil, false
	}
	return []rune(s), true
}

// mayBeEmoji reports whether cluster has a rune from a block that holds emoji.
// It lets wide text that is not emoji (CJK) skip reading every installed font
// to look for a color glyph. It is deliberately loose: the font decides.
func mayBeEmoji(cluster string) bool {
	for _, r := range cluster {
		switch {
		case r >= 0x1F000 && r <= 0x1FAFF: // Pictographs, emoticons, flags, supplemental symbols.
		case r >= 0x2000 && r <= 0x2BFF: // Punctuation, arrows, technical, dingbats, symbols.
		case r == 0xA9, r == 0xAE, r == 0x3030, r == 0x303D, r == 0x3297, r == 0x3299:
		case r == 0xFE0F, r == 0x20E3: // Emoji presentation selector and keycap.
		default:
			continue
		}
		return true
	}
	return false
}
