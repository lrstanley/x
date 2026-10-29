// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"image"
	"math"
	"strings"

	xdraw "golang.org/x/image/draw"

	"github.com/lrstanley/x/term/reel/vt"
)

// imageLayer is one of the three Kitty graphics layers, drawn in order.
type imageLayer uint8

const (
	// layerBelowBackground is drawn over the window background, under the
	// cell backgrounds.
	layerBelowBackground imageLayer = iota
	// layerBelowText is drawn over the cell backgrounds, under the text.
	layerBelowText
	// layerAboveText is drawn over the text, under the cursor.
	layerAboveText
)

func layerOf(p vt.Placement) imageLayer {
	switch {
	case p.BelowBackground():
		return layerBelowBackground
	case p.BelowText():
		return layerBelowText
	}
	return layerAboveText
}

// scaledKey identifies one scaled copy of part of an image.
type scaledKey struct {
	gen    uint64
	source image.Rectangle
	size   image.Point
}

// imageCache keeps the scaled copies the last frame drew, so a placement
// that did not change is not scaled again.
type imageCache struct {
	last, next map[scaledKey]*image.NRGBA
}

// get returns the source part of img scaled to size.
func (c *imageCache) get(img *vt.Image, source image.Rectangle, size image.Point) *image.NRGBA {
	key := scaledKey{gen: img.Generation, source: source, size: size}
	if c.next == nil {
		c.next = map[scaledKey]*image.NRGBA{}
	}
	if s := c.next[key]; s != nil {
		return s
	}
	s := c.last[key]
	if s == nil {
		s = image.NewNRGBA(image.Rectangle{Max: size})
		xdraw.BiLinear.Scale(s, s.Rect, img.Pixels, source, xdraw.Src, nil)
	}
	c.next[key] = s
	return s
}

// rotate keeps what this frame drew, for the next.
func (c *imageCache) rotate() {
	c.last, c.next = c.next, nil
}

// scaleRect maps r, in the pixels of a terminal whose cells are cell, to a
// frame whose grid starts at origin, with cells of cw by ch pixels.
func scaleRect(r image.Rectangle, origin, cell image.Point, cw, ch int) image.Rectangle {
	sx, sy := float64(cw)/float64(cell.X), float64(ch)/float64(cell.Y)
	scale := func(v int, s float64) int { return int(math.Round(float64(v) * s)) }
	return image.Rect(
		scale(r.Min.X, sx), scale(r.Min.Y, sy),
		scale(r.Max.X, sx), scale(r.Max.Y, sy),
	).Add(origin)
}

// drawImages draws scr's Kitty graphics placements in layer, scaled from the
// terminal's cells to the frame's and clipped to the grid.
func (d *Renderer) drawImages(dst *image.NRGBA, scr vt.Screen, f *renderFrame, layer imageLayer) {
	buf, ok := scr.(*vt.Buffer)
	if !ok {
		return
	}
	cell := buf.CellSize()
	if cell.X <= 0 || cell.Y <= 0 {
		return
	}
	clip := dst.Bounds().Intersect(f.gridBounds)
	if clip.Empty() {
		return
	}
	for _, p := range buf.Images() {
		if layerOf(p) != layer || p.Image == nil || p.Source.Empty() {
			continue
		}
		r := scaleRect(p.Rect, f.gridBounds.Min, cell, f.cellW, f.cellH)
		area := clip
		if !p.Clip.Empty() {
			area = area.Intersect(scaleRect(p.Clip, f.gridBounds.Min, cell, f.cellW, f.cellH))
		}
		if r.Empty() || !r.Overlaps(area) {
			continue
		}
		sub, _ := dst.SubImage(area).(*image.NRGBA) // NRGBA.SubImage is always an NRGBA.
		scaled := d.images.get(p.Image, p.Source, r.Size())
		xdraw.Draw(sub, r, scaled, image.Point{}, xdraw.Over)
	}
}

// hasImages reports whether scr has Kitty graphics placements.
func hasImages(scr vt.Screen) bool {
	buf, ok := scr.(*vt.Buffer)
	return ok && len(buf.Images()) > 0
}

// isPlaceholder reports whether cell is a Kitty graphics Unicode placeholder,
// whose image the placements draw instead of its text.
func isPlaceholder(cell *vt.Cell) bool {
	return strings.HasPrefix(cell.Content, "\U0010EEEE")
}
