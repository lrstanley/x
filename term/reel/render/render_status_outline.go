// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"time"

	icol "github.com/lrstanley/x/term/reel/render/internal/color"
	idraw "github.com/lrstanley/x/term/reel/render/internal/draw"
	"github.com/lrstanley/x/term/reel/render/units"
	"github.com/lrstanley/x/term/reel/vt"
)

// Program status colors when the palette does not set them. Working, blocked,
// and error records use the OSC 9;4 progress colors.
var (
	defaultProgramDone = color.NRGBA{R: 0x30, G: 0xd1, B: 0x58, A: 0xff}
	statusHighlight    = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
)

const (
	// statusStroke is the outline width in pixels, and statusStrokeGrow the
	// most any part of it widens past that.
	statusStroke     = 1.5
	statusStrokeGrow = 1.0
	// statusInsetMargin is the least margin, in pixels, the outline is drawn
	// in. With less it covers the window's outermost pixels instead.
	statusInsetMargin = 4

	// statusOrbit is how long a comet takes to go around the window. There
	// are two, half a lap apart, so the outline repeats every half orbit.
	statusOrbit = 4 * time.Second
	// statusCometTail is the share of the perimeter a comet's tail spans, and
	// statusCometHead how many pixels its head fades in over.
	statusCometTail = 0.3
	statusCometHead = 4.0
	// statusGlint is the length, in pixels, of the bright head of a
	// determinate progress fill.
	statusGlint = 48.0

	// statusIdleTint is how far idle records blend from the window background
	// toward the foreground.
	statusIdleTint = 0.42
)

// statusColor is the color of a state: its outline, icon, and dot.
func (f *renderFrame) statusColor(s vt.ProgramState) color.NRGBA {
	switch s {
	case vt.ProgramWorking:
		return f.progressColor(vt.ProgressSet)
	case vt.ProgramBlocked:
		return f.progressColor(vt.ProgressPause)
	case vt.ProgramError:
		return f.progressColor(vt.ProgressError)
	case vt.ProgramDone:
		return paletteNRGBA(f.palette.ProgramDone, defaultProgramDone)
	case vt.ProgramIdle:
	}
	return icol.BlendNRGBA(f.backgroundNRGBA(nil), f.foregroundNRGBA(nil), statusIdleTint)
}

// statusRing is where the outline goes. It depends only on the layout, so
// the frame cache can repaint it whether or not a status is shown.
type statusRing struct {
	shape statusShape
	// The stroke covers off..off+width past the window edge: just outside
	// it, or over its outermost pixels when off is negative (inset).
	off float64
	// bounds holds every pixel the outline can touch, in is how far into
	// the window that reaches, and corner is the size of the corner boxes,
	// measured into the window, outside which the edge is straight.
	bounds image.Rectangle
	in     int
	corner int
}

// inset reports whether the outline covers the window's outermost pixels,
// since the margin is too thin to hold it.
func (r *statusRing) inset() bool { return r.off < 0 }

// layoutStatus sets the program status f draws and where its outline goes.
// The ring is laid out whenever status can be drawn, so a status that goes
// away can be repainted from the same geometry.
func (d *Renderer) layoutStatus(f *renderFrame) {
	f.status.view = d.shownStatus(f.st, f.at)
	if !d.statusEnabled() {
		return
	}
	win, img := f.windowBounds, f.imageBounds
	margin := min(win.Min.X-img.Min.X, win.Min.Y-img.Min.Y, img.Max.X-win.Max.X, img.Max.Y-win.Max.Y)
	r := statusRing{shape: newStatusShape(win, d.opts.BorderRadius.Int())}
	if margin < statusInsetMargin {
		r.off = -statusStroke
	}
	out := int(math.Ceil(r.off+statusStroke+statusStrokeGrow)) + 1
	r.in = int(math.Ceil(max(-r.off, 0)+statusStrokeGrow)) + 1
	r.corner = int(math.Ceil(r.shape.r)) + r.in
	r.bounds = win.Inset(-out).Intersect(img)
	f.status.ring = r
}

// statusBands are the parts of the image the program status can paint: a
// full-width band across the top of the window, through the title bar, one
// across the bottom edge, and the left and right edge columns between them.
// They are empty when status cannot be drawn.
func (f *renderFrame) statusBands() [4]image.Rectangle {
	r := &f.status.ring
	if r.bounds.Empty() {
		return [4]image.Rectangle{}
	}
	win, img := f.windowBounds, f.imageBounds
	top := image.Rectangle{
		Min: image.Pt(img.Min.X, r.bounds.Min.Y),
		Max: image.Pt(img.Max.X, max(win.Min.Y+r.corner, f.chromeBounds.Max.Y)),
	}
	bottom := image.Rectangle{
		Min: image.Pt(img.Min.X, max(win.Max.Y-r.corner, top.Max.Y)),
		Max: image.Pt(img.Max.X, r.bounds.Max.Y),
	}
	left := image.Rectangle{
		Min: image.Pt(r.bounds.Min.X, top.Max.Y),
		Max: image.Pt(win.Min.X+r.in, bottom.Min.Y),
	}
	right := image.Rectangle{
		Min: image.Pt(max(win.Max.X-r.in, left.Max.X), top.Max.Y),
		Max: image.Pt(r.bounds.Max.X, bottom.Min.Y),
	}
	return [4]image.Rectangle{top, bottom, left, right}
}

// statusShape is the window edge as a rounded rectangle.
type statusShape struct {
	// Window center, half extents less the radius, and radius.
	cx, cy, hw, hh, r float64
	perim             float64
}

func newStatusShape(win image.Rectangle, radius int) statusShape {
	w, h := float64(win.Dx()), float64(win.Dy())
	r := units.Clamp(float64(radius), 0, min(w, h)/2)
	s := statusShape{
		cx: float64(win.Min.X) + w/2, cy: float64(win.Min.Y) + h/2,
		hw: w/2 - r, hh: h/2 - r, r: r,
	}
	s.perim = 4*s.hw + 4*s.hh + 2*math.Pi*r
	return s
}

// edgeDistance is the signed distance from (px, py) to the edge: negative
// inside the window.
func (s *statusShape) edgeDistance(px, py float64) float64 {
	qx, qy := math.Abs(px-s.cx)-s.hw, math.Abs(py-s.cy)-s.hh
	ox, oy := max(qx, 0), max(qy, 0)
	return math.Sqrt(ox*ox+oy*oy) + min(max(qx, qy), 0) - s.r
}

// The along offsets of the straight edges, clockwise from the top center: a
// point at x on the top edge is x pixels along (wrapping to the perimeter
// left of center), the right edge starts at rightAlong, and so on. The
// straight runs in [Renderer.drawStatus] start from these and step one pixel
// at a time.
func (s *statusShape) rightAlong() float64  { return s.hw + math.Pi*s.r/2 }
func (s *statusShape) bottomAlong() float64 { return 2*s.hw + 2*(math.Pi*s.r/2) + 2*s.hh }
func (s *statusShape) leftAlong() float64   { return 3*s.hw + 3*(math.Pi*s.r/2) + 3*s.hh }

// along returns how far around the window edge, in pixels clockwise from the
// top center, the edge point nearest (px, py) is, in 0..perim.
func (s *statusShape) along(px, py float64) float64 {
	x, y := px-s.cx, py-s.cy
	cx, cy := units.Clamp(x, -s.hw, s.hw), units.Clamp(y, -s.hh, s.hh)
	vx, vy := x-cx, y-cy
	if vx == 0 && vy == 0 {
		// Inside the straight part: go to the nearer side.
		if s.hw-math.Abs(x) < s.hh-math.Abs(y) {
			vx = math.Copysign(1, x)
		} else {
			vy = math.Copysign(1, y)
		}
	}
	hw, hh, r := s.hw, s.hh, s.r
	arc := math.Pi * r / 2
	switch {
	case vx == 0 && vy < 0: // top edge
		if cx >= 0 {
			return cx
		}
		return s.perim + cx
	case vx > 0 && vy < 0: // top right corner
		return hw + r*(math.Atan2(vy, vx)+math.Pi/2)
	case vx > 0 && vy == 0: // right edge
		return s.rightAlong() + cy + hh
	case vx > 0 && vy > 0: // bottom right corner
		return hw + arc + 2*hh + r*math.Atan2(vy, vx)
	case vx == 0 && vy > 0: // bottom edge
		return s.bottomAlong() - cx
	case vx < 0 && vy > 0: // bottom left corner
		return 3*hw + 2*arc + 2*hh + r*(math.Atan2(vy, vx)-math.Pi/2)
	case vx < 0 && vy == 0: // left edge
		return s.leftAlong() - cy
	default: // top left corner
		return 3*hw + 3*arc + 4*hh + r*(math.Atan2(vy, vx)+math.Pi)
	}
}

// statusPaint is how one point of the outline looks.
type statusPaint struct {
	col    color.NRGBA // stroke color
	stroke float64     // stroke opacity
	width  float64     // stroke width past statusStroke
}

// statusLUTKey is everything an outline look depends on besides the clock,
// so a look that does not move with it is built once and reused across
// frames.
type statusLUTKey struct {
	state    vt.ProgramState
	progress int  // working only
	breath   int8 // blocked only
	col      color.NRGBA
	dim      float64
	perim    float64
}

// statusPaints returns the look of the outline at f.at, one entry per pixel
// along the window edge (see [statusShape.along]), or a single entry when it
// looks the same everywhere. It is built at most once per frame, into
// d.status.lut, and reused by later frames unless it moves with the clock. It
// is dimmed like the window when that is unfocused.
func (d *Renderer) statusPaints(f *renderFrame) []statusPaint {
	if f.status.lut != nil {
		return f.status.lut
	}
	v, perim := &f.status.view, f.status.ring.shape.perim
	key := statusLUTKey{state: v.drv.State, breath: v.breath, col: f.statusColor(v.drv.State), perim: perim}
	if v.drv.State == vt.ProgramWorking {
		key.progress = v.drv.Progress
	}
	if !f.st.Focused {
		key.dim = f.opts.FocusDimming
	}
	c := &d.status
	if c.lut != nil && c.key == key && !v.animates() {
		f.status.lut = c.lut
		return c.lut
	}

	lut, base := c.lut[:0], key.col
	switch v.drv.State {
	case vt.ProgramDone:
		// A steady outline.
		lut = append(lut, statusPaint{col: base, stroke: 0.95})
	case vt.ProgramError:
		// A steady, slightly heavier outline.
		lut = append(lut, statusPaint{col: base, stroke: 1, width: 0.5})
	case vt.ProgramBlocked:
		lut = append(lut, blockedPaint(base, v.breath))
	case vt.ProgramWorking:
		if v.drv.Progress >= 0 {
			lut = progressPaints(lut, base, perim, v.drv.Progress)
		} else {
			lut = cometPaints(lut, base, perim, f.at)
		}
	case vt.ProgramIdle:
	}
	if len(lut) == 0 { // idle, and states this renderer does not know
		// A faint neutral outline.
		lut = append(lut, statusPaint{col: base, stroke: 0.6})
	}
	if key.dim > 0 {
		black := color.NRGBA{A: 0xff}
		for i := range lut {
			lut[i].col = icol.BlendNRGBA(lut[i].col, black, key.dim)
		}
	}
	c.lut, c.key = lut, key
	f.status.lut = lut
	return lut
}

// blockedPaint is a blocked outline at breath step b: it brightens, thickens,
// and turns toward white as it breathes in.
func blockedPaint(base color.NRGBA, step int8) statusPaint {
	b := float64(step) / statusBreathLevels
	return statusPaint{col: icol.BlendNRGBA(base, statusHighlight, 0.18*b), stroke: 0.45 + 0.55*b, width: 0.5 * b}
}

// progressPaints appends a working outline at a known progress, in percent,
// to lut: it fills clockwise from the top center over a dim track, with a
// bright glint at its head.
func progressPaints(lut []statusPaint, base color.NRGBA, perim float64, progress int) []statusPaint {
	n := max(int(math.Ceil(perim)), 1)
	fill := perim * float64(units.Clamp(progress, 0, 100)) / 100
	full := progress >= 100
	for i := range n {
		at := float64(i) + 0.5
		c := units.Clamp(fill-at+0.5, 0, 1)
		p := statusPaint{col: base, stroke: 0.25 + 0.75*c}
		if behind := fill - at; !full && behind >= 0 && behind < statusGlint {
			w := (1 - behind/statusGlint) * c
			w *= w
			p.col = icol.BlendNRGBA(base, statusHighlight, 0.5*w)
			p.width = 0.5 * w
		}
		lut = append(lut, p)
	}
	return lut
}

// cometPaints appends a working outline with no progress at offset at to
// lut: two comets orbit the window, half a lap apart, with fading tails over
// a dim track.
func cometPaints(lut []statusPaint, base color.NRGBA, perim float64, at time.Duration) []statusPaint {
	n := max(int(math.Ceil(perim)), 1)
	head := perim * float64(idraw.Cycle(at, statusOrbit)) / float64(statusOrbit)
	half, tail := perim/2, perim*statusCometTail
	track := statusPaint{col: base, stroke: 0.3}
	for i := range n {
		behind := head - float64(i) - 0.5
		behind -= math.Floor(behind/half) * half
		ahead := half - behind
		if behind >= tail && ahead >= statusCometHead {
			lut = append(lut, track)
			continue
		}
		w := 0.0
		if behind < tail {
			w = 1 - behind/tail
			w *= w
		}
		if ahead < statusCometHead {
			w = max(w, smoothstep(1-ahead/statusCometHead))
		}
		lut = append(lut, statusPaint{
			col:    icol.BlendNRGBA(base, statusHighlight, 0.6*w*w*w),
			stroke: 0.3 + 0.7*w,
			width:  w * w,
		})
	}
	return lut
}

// outlinePen paints the outline of one frame into one destination.
type outlinePen struct {
	dst   draw.Image
	nrgba *image.NRGBA // dst, when it is one, so pixels are blended in place
	off   float64      // [statusRing.off]
	inset bool
	// Only pixels minD..maxD past the window edge can change.
	minD, maxD float64

	lut   []statusPaint
	shape *statusShape
	win   image.Rectangle
	// clip is what can change of the ring's bounds and dst. Corner rows
	// split into corners left of cl and right of cr, and the straight edge
	// x0..x1 between them, which wraps to 0 along the edge at topX. Side rows
	// paint the columns left0..left1 and right0..right1.
	clip                         image.Rectangle
	cl, cr, x0, x1, topX         int
	left0, left1, right0, right1 int
}

func newOutlinePen(dst draw.Image, f *renderFrame, lut []statusPaint) outlinePen {
	ring := &f.status.ring
	o := outlinePen{
		dst: dst, off: ring.off, inset: ring.inset(),
		lut: lut, shape: &ring.shape, win: f.windowBounds, clip: ring.bounds.Intersect(dst.Bounds()),
	}
	o.nrgba, _ = dst.(*image.NRGBA)
	if o.inset {
		// A translucent pixel this close is recolored, see paint.
		o.minD, o.maxD = ring.off-1, ring.off+statusStroke+statusStrokeGrow+1
	} else {
		// Where the widest stroke has any coverage.
		o.minD, o.maxD = ring.off-0.5, ring.off+statusStroke+statusStrokeGrow+0.5
	}

	// Spans are split at the window's middle, so a window narrower than two
	// corners is not painted twice.
	win, clip := o.win, o.clip
	midX := win.Min.X + win.Dx()/2
	o.cl, o.cr = min(win.Min.X+ring.corner, midX), max(win.Max.X-ring.corner, midX)
	o.x0, o.x1 = max(o.cl, clip.Min.X), min(o.cr, clip.Max.X)
	o.topX = units.Clamp(int(math.Ceil(o.shape.cx-0.5)), o.x0, o.x1)
	el, er := min(win.Min.X+ring.in, midX), max(win.Max.X-ring.in, midX)
	o.left0, o.left1 = o.liveColumns(clip.Min.X, min(el, clip.Max.X), func(px float64) float64 { return float64(win.Min.X) - px })
	o.right0, o.right1 = o.liveColumns(max(er, clip.Min.X), clip.Max.X, func(px float64) float64 { return px - float64(win.Max.X) })
	return o
}

// live reports whether a pixel du past the window edge can change.
func (o *outlinePen) live(du float64) bool { return du >= o.minD && du <= o.maxD }

// liveColumns returns the columns of x0..x1 that can change, the first
// through the last whose pixel center px is du(px) past the window edge.
func (o *outlinePen) liveColumns(x0, x1 int, du func(px float64) float64) (lo, hi int) {
	lo, hi = x1, x1
	for x := x0; x < x1; x++ {
		if o.live(du(float64(x) + 0.5)) {
			lo, hi = min(lo, x), x+1
		}
	}
	return lo, hi
}

// at is the look of the outline a distance along the edge, see
// [statusShape.along].
func (o *outlinePen) at(along float64) *statusPaint {
	return &o.lut[int(along)%len(o.lut)]
}

// paint draws p at the pixel (x, y), du pixels outside the window edge.
func (o *outlinePen) paint(x, y int, du float64, p *statusPaint) {
	if !o.live(du) {
		return
	}
	bw := statusStroke + p.width
	lo := o.off // the stroke covers lo..lo+bw past the edge
	if o.inset {
		lo = -bw // a wider stroke grows into the window
		// A window pixel the rounded mask left translucent: recolor what is
		// there instead of adding to its alpha, which would leave a brighter
		// blob at each corner.
		if du > -1.5 && o.recolor(x, y, p.col, min(1, du+0.5-lo)*p.stroke) {
			return
		}
	}
	if c := min(du+0.5, lo+bw) - max(du-0.5, lo); c > 0 {
		if o.nrgba == nil {
			idraw.OverAt(o.dst, x, y, p.col, coverage(c*p.stroke))
			return
		}
		i := o.nrgba.PixOffset(x, y)
		idraw.OverPixel(o.nrgba.Pix[i:i+4:i+4], p.col, coverage(c*p.stroke))
	}
}

// recolor is [idraw.RecolorAt] for the pixel (x, y) of dst.
func (o *outlinePen) recolor(x, y int, col color.NRGBA, k float64) bool {
	if o.nrgba == nil {
		return idraw.RecolorAt(o.dst, x, y, col, k)
	}
	i := o.nrgba.PixOffset(x, y)
	return idraw.RecolorPixel(o.nrgba.Pix[i:i+4:i+4], col, k)
}

// sides paints row y across the straight left and right edges, where how far
// along the edge a pixel is does not depend on x.
func (o *outlinePen) sides(y int) {
	py := float64(y) + 0.5
	cy := py - o.shape.cy // within the straight part on these rows
	l := o.at(o.shape.leftAlong() - cy)
	r := o.at(o.shape.rightAlong() + cy + o.shape.hh)
	for x := o.left0; x < o.left1; x++ {
		o.paint(x, y, float64(o.win.Min.X)-(float64(x)+0.5), l)
	}
	for x := o.right0; x < o.right1; x++ {
		o.paint(x, y, float64(x)+0.5-float64(o.win.Max.X), r)
	}
}

// corners paints row y across both corners, with the full rounded box
// distance.
func (o *outlinePen) corners(y int) {
	py := float64(y) + 0.5
	for _, span := range [2][2]int{{o.clip.Min.X, o.cl}, {o.cr, o.clip.Max.X}} {
		for x := max(span[0], o.clip.Min.X); x < min(span[1], o.clip.Max.X); x++ {
			px := float64(x) + 0.5
			du := o.shape.edgeDistance(px, py)
			if !o.live(du) {
				continue
			}
			p := &o.lut[0]
			if len(o.lut) > 1 {
				p = o.at(o.shape.along(px, py))
			}
			o.paint(x, y, du, p)
		}
	}
}

// edge paints row y across the straight top or bottom edge, which is all the
// same distance from it. Where a pixel is along the edge steps by one pixel
// across the row, clockwise: left to right on top, wrapping to 0 at the top
// center, and right to left at the bottom.
func (o *outlinePen) edge(y int) {
	py := float64(y) + 0.5
	du := max(float64(o.win.Min.Y)-py, py-float64(o.win.Max.Y))
	if o.x0 >= o.x1 || !o.live(du) {
		return
	}
	n := len(o.lut)
	if n == 1 {
		o.run(o.x0, o.x1, y, du, &o.lut[0])
		return
	}
	if py >= o.shape.cy {
		i := int(o.shape.bottomAlong()-(float64(o.x0)+0.5-o.shape.cx)) % n
		for x := o.x0; x < o.x1; x++ {
			o.paint(x, y, du, &o.lut[i])
			i--
			if i < 0 {
				i = n - 1
			}
		}
		return
	}
	for _, span := range [2][2]int{{o.x0, o.topX}, {o.topX, o.x1}} {
		if span[0] >= span[1] {
			continue
		}
		at := float64(span[0]) + 0.5 - o.shape.cx
		if at < 0 {
			at += o.shape.perim
		}
		i := int(at) % n
		for x := span[0]; x < span[1]; x++ {
			o.paint(x, y, du, &o.lut[i])
			i++
			if i == n {
				i = 0
			}
		}
	}
}

// run paints p over x0..x1 of row y, all du pixels outside the window edge,
// in one pass when nothing in it is recolored.
func (o *outlinePen) run(x0, x1, y int, du float64, p *statusPaint) {
	if o.inset && du > -1.5 {
		for x := x0; x < x1; x++ {
			o.paint(x, y, du, p)
		}
		return
	}
	bw := statusStroke + p.width
	lo := o.off
	if o.inset {
		lo = -bw
	}
	if c := min(du+0.5, lo+bw) - max(du-0.5, lo); c > 0 {
		idraw.OverRow(o.dst, x0, x1, y, p.col, coverage(c*p.stroke))
	}
}

// drawStatus paints the program status outline around the window. It runs
// after the rounded window mask, since the outline sits on the margin that
// mask restores, and only visits pixels within reach of the edge: rows
// across the top and bottom edges, and short spans across the sides and
// corners. Along the straight edges, how far a pixel is from the edge is the
// same down a column or across a row, so it is worked out once for each.
func (d *Renderer) drawStatus(dst draw.Image, f *renderFrame) {
	ring := &f.status.ring
	if !f.status.view.shown || !ring.bounds.Overlaps(dst.Bounds()) {
		return
	}
	o := newOutlinePen(dst, f, d.statusPaints(f))
	win := f.windowBounds
	for y := o.clip.Min.Y; y < o.clip.Max.Y; y++ {
		if y >= win.Min.Y+ring.corner && y < win.Max.Y-ring.corner {
			o.sides(y)
			continue
		}
		o.corners(y)
		o.edge(y)
	}
}
