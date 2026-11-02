// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"image"
	"image/color"
	"math"
)

// shape is a shape [Mouse] draws, antialiased over a pixel: sdf is the
// signed distance in pixels from its edge, negative inside, and box bounds
// the pixels it covers.
type shape struct {
	sdf func(x, y float64) float64
	box image.Rectangle
}

// grow is sh grown by d pixels on every side.
func (sh shape) grow(d float64) shape {
	return shape{
		sdf: func(x, y float64) float64 { return sh.sdf(x, y) - d },
		box: sh.box.Inset(-int(math.Ceil(d))),
	}
}

// boxAround is the pixels within r of c, with a pixel more for
// antialiasing.
func boxAround(c vec, r float64) image.Rectangle {
	return image.Rect(
		int(math.Floor(c.x-r))-1, int(math.Floor(c.y-r))-1,
		int(math.Ceil(c.x+r))+1, int(math.Ceil(c.y+r))+1,
	)
}

// disc is the disc of radius r around c.
func disc(c vec, r float64) shape {
	return shape{
		sdf: func(x, y float64) float64 { return math.Hypot(x-c.x, y-c.y) - r },
		box: boxAround(c, r),
	}
}

// ring is the circle of radius r around c, w pixels wide.
func ring(c vec, r, w float64) shape {
	return shape{
		sdf: func(x, y float64) float64 { return math.Abs(math.Hypot(x-c.x, y-c.y)-r) - w/2 },
		box: boxAround(c, r+w/2),
	}
}

// capsule is the line from a to b, w pixels wide, with round ends.
func capsule(a, b vec, w float64) shape {
	return shape{
		sdf: func(x, y float64) float64 { return segmentDist(vec{x, y}, a, b) - w/2 },
		box: boxAround(a, w/2).Union(boxAround(b, w/2)),
	}
}

// polygon is the polygon with vertices pts.
func polygon(pts []vec) shape {
	var box image.Rectangle
	for _, p := range pts {
		box = box.Union(boxAround(p, 0))
	}
	return shape{
		sdf: func(x, y float64) float64 {
			d, inside := math.Inf(1), false
			for i, a := range pts {
				b := pts[(i+1)%len(pts)]
				d = min(d, segmentDist(vec{x, y}, a, b))
				if (a.y > y) != (b.y > y) && x < a.x+(y-a.y)*(b.x-a.x)/(b.y-a.y) {
					inside = !inside
				}
			}
			if inside {
				return -d
			}
			return d
		},
		box: box,
	}
}

// closest is the point of the line segment from a to b nearest p.
func closest(p, a, b vec) vec {
	dx, dy := b.x-a.x, b.y-a.y
	var u float64
	if l := dx*dx + dy*dy; l > 0 {
		u = clamp01(((p.x-a.x)*dx + (p.y-a.y)*dy) / l)
	}
	return vec{a.x + u*dx, a.y + u*dy}
}

// segmentDist is the distance from p to the line segment from a to b.
func segmentDist(p, a, b vec) float64 {
	c := closest(p, a, b)
	return math.Hypot(p.x-c.x, p.y-c.y)
}

// resample returns the points every step along path, from its start, and
// its end. A path with no length is its start alone. step must be above
// zero.
func resample(path []vec, step float64) []vec {
	out := []vec{path[0]}
	// next is how far along the leg from a the next point is.
	next := step
	for k := 1; k < len(path); k++ {
		a, b := path[k-1], path[k]
		l := math.Hypot(b.x-a.x, b.y-a.y)
		for ; next <= l; next += step {
			out = append(out, a.lerp(b, next/l))
		}
		next -= l
	}
	if end := path[len(path)-1]; out[len(out)-1] != end {
		out = append(out, end)
	}
	return out
}

// smoothPath returns path resampled every step (see [resample]), each point
// the average of it and the k points on each side of it. Toward the ends
// the points are averaged with fewer, evenly around them, so the ends stay
// put.
func smoothPath(path []vec, step float64, k int) []vec {
	pts := resample(path, step)
	out := make([]vec, len(pts))
	for i := range pts {
		r := min(k, i, len(pts)-1-i)
		var sum vec
		for _, p := range pts[i-r : i+r+1] {
			sum.x, sum.y = sum.x+p.x, sum.y+p.y
		}
		m := float64(2*r + 1)
		out[i] = vec{sum.x / m, sum.y / m}
	}
	return out
}

// cover raises the coverage of each pixel of mask to that of sh. Shapes
// drawn into one mask overlap, such as the legs of a trail, so pixels
// already covered whole are skipped.
func cover(mask *image.Alpha, sh shape) {
	r := sh.box.Intersect(mask.Rect)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		row := mask.Pix[mask.PixOffset(r.Min.X, y):][:r.Dx()]
		for j, a := range row {
			if a == 0xff {
				continue
			}
			switch d := sh.sdf(float64(r.Min.X+j)+0.5, float64(y)+0.5); {
			case d <= -0.5:
				row[j] = 0xff
			case d < 0.5:
				row[j] = max(a, uint8(math.Round(0xff*(0.5-d))))
			}
		}
	}
}

// maskOf is the coverage of sh.
func maskOf(sh shape) *image.Alpha {
	mask := image.NewAlpha(sh.box)
	cover(mask, sh)
	return mask
}

// paintMask draws c over img where mask covers it, blending as
// [draw.DrawMask] does with [draw.Over], but on img's pixels directly
// rather than through a color per pixel of the mask's box, most of which a
// trail leaves uncovered.
func paintMask(img *image.NRGBA, mask *image.Alpha, c color.NRGBA) {
	const m = 0xffff
	sr, sg, sb, sa := c.RGBA()
	r := mask.Rect.Intersect(img.Rect)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		i := img.PixOffset(r.Min.X, y)
		for _, cov := range mask.Pix[mask.PixOffset(r.Min.X, y):][:r.Dx()] {
			if cov != 0 {
				ma := uint32(cov) * 0x101
				px := img.Pix[i : i+4 : i+4]
				dr, dg, db, da := color.NRGBA{px[0], px[1], px[2], px[3]}.RGBA()
				// Over, premultiplied, then back to NRGBA as
				// image.NRGBA.SetRGBA64 does.
				keep := m - sa*ma/m
				or, og, ob, oa := (dr*keep+sr*ma)/m, (dg*keep+sg*ma)/m, (db*keep+sb*ma)/m, (da*keep+sa*ma)/m
				if oa != 0 && oa != m {
					or, og, ob = or*m/oa, og*m/oa, ob*m/oa
				}
				px[0], px[1], px[2], px[3] = uint8(or>>8), uint8(og>>8), uint8(ob>>8), uint8(oa>>8) //nolint:gosec // Each is at most 0xffff.
			}
			i += 4
		}
	}
}
