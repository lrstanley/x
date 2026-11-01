// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"slices"
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

// simplify returns path without the points that stay within tolerance of
// the line between the points kept around them, by the Ramer-Douglas-Peucker
// algorithm. The ends are always kept.
func simplify(path []vec, tolerance float64) []vec {
	if len(path) < 3 {
		return path
	}
	a, b := path[0], path[len(path)-1]
	far, dist := 0, tolerance
	for i, p := range path[1 : len(path)-1] {
		if d := segmentDist(p, a, b); d > dist {
			far, dist = i+1, d
		}
	}
	if far == 0 {
		return []vec{a, b}
	}
	left := simplify(path[:far+1], tolerance)
	return slices.Concat(left[:len(left)-1], simplify(path[far:], tolerance))
}

// cover raises the coverage of each pixel of mask to that of sh.
func cover(mask *image.Alpha, sh shape) {
	r := sh.box.Intersect(mask.Rect)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			a := uint8(math.Round(0xff * clamp01(0.5-sh.sdf(float64(x)+0.5, float64(y)+0.5))))
			i := mask.PixOffset(x, y)
			mask.Pix[i] = max(mask.Pix[i], a)
		}
	}
}

// maskOf is the coverage of sh.
func maskOf(sh shape) *image.Alpha {
	mask := image.NewAlpha(sh.box)
	cover(mask, sh)
	return mask
}

// paintMask draws c over img where mask covers it.
func paintMask(img *image.NRGBA, mask *image.Alpha, c color.NRGBA) {
	draw.DrawMask(img, mask.Rect, image.NewUniform(c), image.Point{}, mask, mask.Rect.Min, draw.Over)
}
