// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package export

import (
	"cmp"
	"encoding/binary"
	"image"
	"image/color"
	"slices"
)

const (
	// adaptiveSlots is the number of opaque palette entries; the last slot is
	// TransparentPaletteIndex.
	adaptiveSlots = int(TransparentPaletteIndex)

	// adaptiveMatchDist is the squared RGB distance under which a new color
	// reuses an existing entry instead of claiming a slot (about 2 per channel).
	adaptiveMatchDist = 12

	// adaptiveSolidCount is the pixel count at which a new color is treated as
	// a fill or glyph body rather than an antialiasing blend, and gets an exact
	// entry ahead of median cut.
	adaptiveSolidCount = 64

	// adaptiveSolidReserve is the number of final slots a recording keeps for
	// solid colors only, so fills that first appear late stay exact after
	// blends have used up the rest.
	adaptiveSolidReserve = 16
)

// adaptivePalette builds a palette from the colors a recording actually
// uses. A fixed palette such as xterm-256 has no entries between its cube
// steps, so antialiased glyph edges snap to colors with a different hue.
//
// Entries are only ever appended, and every color keeps the index it was
// first assigned. Frames palettized earlier stay valid, and identical source
// frames always produce identical indices, which dedup and disposal
// optimization rely on.
type adaptivePalette struct {
	pal color.Palette
	rgb [adaptiveSlots][3]int32
	n   int

	// reserve keeps a share of free slots for colors that first appear in
	// later frames. Single images should use every slot.
	reserve bool

	// cache maps packed RGB to the palette index assigned to it. Spans only
	// read it while a frame is being written.
	cache map[uint32]uint8
	hist  map[uint32]int
	// spanMisses counts uncached colors per row span of the frame being
	// written. Each span writes only its own entry.
	spanMisses []map[uint32]int
}

func newAdaptivePalette(reserve bool) *adaptivePalette {
	pal := make(color.Palette, adaptiveSlots+1)
	for i := range adaptiveSlots {
		pal[i] = color.RGBA{A: 0xff}
	}
	pal[TransparentPaletteIndex] = transparentMarkerColor()
	return &adaptivePalette{
		pal:     pal,
		reserve: reserve,
		cache:   make(map[uint32]uint8),
		hist:    make(map[uint32]int),
	}
}

// palettize maps src into dst, first growing the palette with colors src
// introduces. dst.Palette must be p.pal. Alpha 0 writes
// TransparentPaletteIndex; other pixels map by their color alone, matching
// [palettizeInto].
func (p *adaptivePalette) palettize(dst *image.Paletted, src image.Image) {
	var px rgbaPixels
	switch s := src.(type) {
	case *image.NRGBA:
		px = rgbaPixels{pix: s.Pix, stride: s.Stride, rect: s.Rect}
	case *image.RGBA:
		px = rgbaPixels{pix: s.Pix, stride: s.Stride, rect: s.Rect, premultiplied: true}
	default:
		n := asNRGBA(src)
		px = rgbaPixels{pix: n.Pix, stride: n.Stride, rect: n.Rect}
	}
	area := px.rect.Intersect(dst.Bounds())
	if area.Empty() {
		return
	}

	runPalettizeSpans(dst, px, area, p)
	for _, misses := range p.spanMisses {
		for key, count := range misses {
			p.hist[key] += count
		}
	}
	if len(p.hist) == 0 {
		return
	}
	p.grow()
	for key := range p.hist {
		p.cache[key] = p.nearest(key)
	}
	clear(p.hist)
	runPalettizeSpans(dst, px, area, p)
}

func (p *adaptivePalette) resetSpans(n int) {
	for i := range p.spanMisses {
		clear(p.spanMisses[i])
	}
	if len(p.spanMisses) < n {
		p.spanMisses = append(p.spanMisses, make([]map[uint32]int, n-len(p.spanMisses))...)
	}
}

// writeSpan maps rows minY through maxY of area through the cache, counting
// colors that have no index yet in spanMisses[span]. Those pixels are left
// for a second pass once the palette has grown.
//
// Terminal frames are mostly flat runs, so each run of identical pixel words
// is looked up and counted once, weighted by its length. Alpha 0 pixels all
// map to TransparentPaletteIndex whatever their color, so they form one run.
func (p *adaptivePalette) writeSpan(span int, dst *image.Paletted, px rgbaPixels, area image.Rectangle, minY, maxY int) {
	width := area.Dx()
	misses := p.spanMisses[span]
	le := binary.LittleEndian
	for y := minY; y < maxY; y++ {
		si := (y-px.rect.Min.Y)*px.stride + (area.Min.X-px.rect.Min.X)*4
		di := (y-dst.Rect.Min.Y)*dst.Stride + (area.Min.X - dst.Rect.Min.X)
		row := px.pix[si : si+width*4]
		out := dst.Pix[di : di+width]
		last, lastIdx, lastOK, haveLast := uint32(0), uint8(0), false, false
		for x := 0; x < width; {
			w, mask := le.Uint32(row[x*4:]), ^uint32(0)
			if w>>24 == 0 {
				mask = 0xff000000
			}
			w &= mask
			m64, w64 := uint64(mask)*0x100000001, uint64(w)*0x100000001
			end := x + 1
			for end < min(x+8, width) && le.Uint32(row[end*4:])&mask == w {
				end++
			}
			if end-x == 8 {
				for end+8 <= width {
					r := row[end*4 : end*4+32]
					if le.Uint64(r)&m64 != w64 || le.Uint64(r[8:])&m64 != w64 ||
						le.Uint64(r[16:])&m64 != w64 || le.Uint64(r[24:])&m64 != w64 {
						break
					}
					end += 8
				}
				for end < width && le.Uint32(row[end*4:])&mask == w {
					end++
				}
			}

			idx := TransparentPaletteIndex
			if w>>24 != 0 {
				r, g, b := uint8(w), uint8(w>>8), uint8(w>>16)
				if a := uint8(w >> 24); px.premultiplied && a != 0xff {
					r, g, b = unpremultiply(r, a), unpremultiply(g, a), unpremultiply(b, a)
				}
				key := uint32(r)<<16 | uint32(g)<<8 | uint32(b)
				if !haveLast || key != last {
					last, haveLast = key, true
					lastIdx, lastOK = p.cache[key]
				}
				if !lastOK {
					if misses == nil {
						misses = make(map[uint32]int)
						p.spanMisses[span] = misses
					}
					misses[key] += end - x
				}
				idx = lastIdx
			}
			run := out[x:end]
			run[0] = idx
			for n := 1; n < len(run); n *= 2 {
				copy(run[n:], run[:n])
			}
			x = end
		}
	}
}

// grow adds palette entries for colors in hist that no entry already covers.
// Solid colors get exact entries first. Blends are added exactly when they
// fit, otherwise median cut picks representatives.
func (p *adaptivePalette) grow() {
	if p.n == adaptiveSlots {
		return
	}
	var solid, blends []histColor
	for key, count := range p.hist {
		if p.n > 0 {
			if _, d := p.nearestDist(key); d <= adaptiveMatchDist {
				continue
			}
		}
		c := histColor{key: key, count: count}
		if count >= adaptiveSolidCount {
			solid = append(solid, c)
		} else {
			blends = append(blends, c)
		}
	}
	byCount := func(a, b histColor) int {
		return cmp.Or(cmp.Compare(b.count, a.count), cmp.Compare(a.key, b.key))
	}
	slices.SortFunc(solid, byCount)
	for _, c := range solid {
		if p.n == adaptiveSlots {
			return
		}
		p.add(c.key)
	}

	remaining := adaptiveSlots - p.n
	if p.reserve {
		remaining -= adaptiveSolidReserve
	}
	if remaining <= 0 || len(blends) == 0 {
		return
	}
	if len(blends) <= remaining {
		slices.SortFunc(blends, byCount)
		for _, c := range blends {
			p.add(c.key)
		}
		return
	}
	budget := remaining
	if p.reserve {
		budget -= remaining / 4
	}
	for _, key := range medianCut(blends, max(budget, 1)) {
		if _, d := p.nearestDist(key); d > 0 {
			p.add(key)
		}
	}
}

func (p *adaptivePalette) add(key uint32) {
	r, g, b := uint8(key>>16), uint8(key>>8), uint8(key) //nolint:gosec // packed 8-bit channels.
	p.pal[p.n] = color.RGBA{R: r, G: g, B: b, A: 0xff}
	p.rgb[p.n] = [3]int32{int32(r), int32(g), int32(b)}
	p.n++
}

func (p *adaptivePalette) nearest(key uint32) uint8 {
	idx, _ := p.nearestDist(key)
	return idx
}

// nearestDist returns the closest used entry to key and its squared RGB
// distance. With no entries it returns index 0.
func (p *adaptivePalette) nearestDist(key uint32) (idx uint8, dist int32) {
	r, g, b := int32(key>>16&0xff), int32(key>>8&0xff), int32(key&0xff)
	best := 0
	dist = 1 << 30
	for i, c := range p.rgb[:p.n] {
		dr, dg, db := r-c[0], g-c[1], b-c[2]
		d := dr*dr + dg*dg + db*db
		if d < dist {
			best, dist = i, d
			if d == 0 {
				break
			}
		}
	}
	return uint8(best), dist //nolint:gosec // best < adaptiveSlots.
}

// histColor is one distinct color and its pixel count.
type histColor struct {
	key   uint32
	count int
}

func (c histColor) channel(axis int) int32 {
	return int32(c.key >> (16 - 8*axis) & 0xff) //nolint:gosec // 8-bit channel.
}

// colorStats accumulates count-weighted channel sums for squared error.
type colorStats struct {
	n     float64
	sum   [3]float64
	sumSq float64
}

func (s colorStats) add(c histColor) colorStats {
	w := float64(c.count)
	s.n += w
	for axis := range 3 {
		v := float64(c.channel(axis))
		s.sum[axis] += w * v
		s.sumSq += w * v * v
	}
	return s
}

func (s colorStats) sub(o colorStats) colorStats {
	s.n -= o.n
	s.sumSq -= o.sumSq
	for axis := range 3 {
		s.sum[axis] -= o.sum[axis]
	}
	return s
}

// sse is the count-weighted squared error around the mean.
func (s colorStats) sse() float64 {
	if s.n == 0 {
		return 0
	}
	return s.sumSq - (s.sum[0]*s.sum[0]+s.sum[1]*s.sum[1]+s.sum[2]*s.sum[2])/s.n
}

// colorBox is a median-cut partition with its count-weighted squared error.
type colorBox struct {
	colors []histColor
	stats  colorStats
}

func newColorBox(colors []histColor) colorBox {
	box := colorBox{colors: colors}
	for _, c := range colors {
		box.stats = box.stats.add(c)
	}
	return box
}

// medianCut partitions colors into at most k boxes and returns one
// representative per box. It always splits the box with the largest weighted
// error, at the cut along any channel that minimizes the error of the two
// halves. That isolates solid fills first and spends the remaining entries
// along antialiasing ramps; a plain weighted-median cut instead lands on the
// dominant fill and peels one ramp color per split.
func medianCut(colors []histColor, k int) []uint32 {
	boxes := []colorBox{newColorBox(colors)}
	for len(boxes) < k {
		bi := 0
		for i := range boxes {
			if boxes[i].stats.sse() > boxes[bi].stats.sse() {
				bi = i
			}
		}
		box := boxes[bi]
		if len(box.colors) < 2 || box.stats.sse() <= 0 {
			break
		}
		axis, split := bestCut(box)
		sortByChannel(box.colors, axis)
		boxes[bi] = newColorBox(box.colors[:split])
		boxes = append(boxes, newColorBox(box.colors[split:]))
	}

	reps := make([]uint32, len(boxes))
	for i, box := range boxes {
		reps[i] = boxRepresentative(box.colors)
	}
	return reps
}

func sortByChannel(colors []histColor, axis int) {
	slices.SortFunc(colors, func(a, b histColor) int {
		return cmp.Or(cmp.Compare(a.channel(axis), b.channel(axis)), cmp.Compare(a.key, b.key))
	})
}

// bestCut returns the channel and split index that minimize the summed error
// of both halves of box.
func bestCut(box colorBox) (axis, split int) {
	axis, split = 0, 1
	best := box.stats.sse()
	for a := range 3 {
		sortByChannel(box.colors, a)
		var left colorStats
		for i, c := range box.colors[:len(box.colors)-1] {
			left = left.add(c)
			if box.colors[i+1].channel(a) == c.channel(a) {
				continue
			}
			if e := left.sse() + box.stats.sub(left).sse(); e < best {
				axis, split, best = a, i+1, e
			}
		}
	}
	return axis, split
}

// boxRepresentative returns the box's dominant color when it holds most of
// the box's pixels, so solid fills stay exact, otherwise the weighted mean.
func boxRepresentative(colors []histColor) uint32 {
	total, mode := 0, colors[0]
	var sum [3]int
	for _, c := range colors {
		total += c.count
		if c.count > mode.count {
			mode = c
		}
		for axis := range 3 {
			sum[axis] += c.count * int(c.channel(axis))
		}
	}
	if mode.count*2 > total {
		return mode.key
	}
	var key uint32
	for axis := range 3 {
		key |= uint32((sum[axis]+total/2)/total) << (16 - 8*axis) //nolint:gosec // mean of 8-bit values.
	}
	return key
}
