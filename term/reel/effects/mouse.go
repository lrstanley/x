// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"cmp"
	"fmt"
	"image"
	"image/color"
	"math"
	"slices"
	"sort"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/reel/vt"
)

const (
	// mouseGlide and mouseHide are the defaults of [WithPointerGlide] and
	// [WithPointerHide].
	mouseGlide = 150 * time.Millisecond
	mouseHide  = time.Second
	// mouseFade is how long a hidden pointer takes to fade out.
	mouseFade = 250 * time.Millisecond
	// mouseRipple is how long a press's ripple takes to spread and fade,
	// and a drag's trail to fade once it is released.
	mouseRipple = 400 * time.Millisecond
	// mouseRipples is how many ripples show at once, newest kept.
	mouseRipples = 4
	// mouseTrailTolerance is how far, in cells, a drag's path may stray
	// from its trail. A drag steps from cell to cell, up to half a cell off
	// the line it follows, and more from the line to a cell partway along
	// it, so a cell of tolerance draws a straight drag straight.
	mouseTrailTolerance = 1.0
)

var (
	// pointerFill is the default color of the pointer.
	pointerFill = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	// buttonColors are the default colors of the buttons, by
	// [vt.MouseButton]: left blue, right orange, and middle green, xterm 75,
	// 215, and 114.
	buttonColors = [...]color.NRGBA{
		vt.MouseLeft:   {R: 0x5f, G: 0xaf, B: 0xff, A: 0xff},
		vt.MouseRight:  {R: 0xff, G: 0xaf, B: 0x5f, A: 0xff},
		vt.MouseMiddle: {R: 0x87, G: 0xd7, B: 0x87, A: 0xff},
	}
)

// PointerStyle is the shape of the pointer [Mouse] draws.
type PointerStyle int

const (
	// PointerArrow is an arrow with its tip on the center of the cell.
	PointerArrow PointerStyle = iota
	// PointerDot is a disc centered on the cell.
	PointerDot
)

// pointerStyleNames are the names of the pointer styles, by value.
var pointerStyleNames = []string{"arrow", "dot"}

// MouseParts is a set of the parts of the mouse [Mouse] draws.
type MouseParts uint8

const (
	// MousePointer is the pointer, which glides to the cell of each mouse
	// event and hides once the mouse is idle.
	MousePointer MouseParts = 1 << iota
	// MouseClicks is a ripple where a button is pressed, and a disc under
	// the pointer while it is held.
	MouseClicks
	// MouseDrags is a trail along the path of the pointer while a button is
	// held, which fades once it is released.
	MouseDrags

	// MouseAll is every part, the default.
	MouseAll = MousePointer | MouseClicks | MouseDrags
)

// mousePartNames are the names of the parts, by bit.
var mousePartNames = []string{"pointer", "clicks", "drags"}

// MouseOption configures [Mouse].
type MouseOption func(*mouse)

// WithPointerStyle sets the pointer's shape. The default is [PointerArrow].
// Any other value is an error from Start.
func WithPointerStyle(s PointerStyle) MouseOption {
	return func(m *mouse) { m.style = s }
}

// WithPointerSize sets the pointer's height in pixels, or its diameter for
// [PointerDot]. Zero, the default, is a cell's height for an arrow, and
// half a cell's height for a dot. Below zero is an error from
// Start.
func WithPointerSize(px int) MouseOption {
	return func(m *mouse) { m.size = px }
}

// WithPointerColor sets the color the pointer is filled with. Its outline
// is black or white, whichever contrasts more. The default, and a nil c, is
// white.
func WithPointerColor(c color.Color) MouseOption {
	return func(m *mouse) { m.pointer = nrgbaOr(c, pointerFill) }
}

// WithButtonColor sets the color of the ripples, held disc, and trail of
// button, one of [vt.MouseLeft], [vt.MouseMiddle], and [vt.MouseRight]; other
// buttons are ignored. The defaults are blue for left, green for middle,
// and orange for right, and a nil c restores button's.
func WithButtonColor(button vt.MouseButton, c color.Color) MouseOption {
	return func(m *mouse) {
		if isButton(button) {
			m.buttons[button] = nrgbaOr(c, buttonColors[button])
		}
	}
}

// WithPointerGlide sets how long the pointer takes to move to the cell of
// each mouse event, easing in and out, in playback time. A press is not
// cut short: the pointer reaches its cell before it moves on, so the
// events after it wait up to that long. The default is 150ms; zero or less
// moves it there at once.
func WithPointerGlide(d time.Duration) MouseOption {
	return func(m *mouse) { m.glide = d }
}

// WithPointerHide sets how long, in playback time, the pointer stays after
// the last mouse event while no button is held. It then fades out over
// 250ms, and the next mouse event shows it again at its cell. The default
// is 1s; zero or less never hides it.
func WithPointerHide(d time.Duration) MouseOption {
	return func(m *mouse) { m.hide = d }
}

// WithMouseParts sets which parts of the mouse are drawn. The default is
// [MouseAll]; none draws nothing.
func WithMouseParts(p MouseParts) MouseOption {
	return func(m *mouse) { m.parts = p & MouseAll }
}

// Mouse returns an effect that draws the mouse on the terminal frames, from
// the mouse events of the timeline's [reel.Input] events, such as those of
// [reel.Session.Mouse], [reel.Session.Click], and [reel.Session.Drag]: a
// pointer that glides to the cell of each event (see [WithPointerGlide]), a
// ripple where a button is pressed, a disc under the pointer while it is
// held, and a trail along the path of a drag, which fades once the button is
// released. Ripples, discs, and trails take the button's color (see
// [WithButtonColor]). The pointer hides once the mouse is idle (see
// [WithPointerHide]). Wheel turns and the extra buttons only move the
// pointer.
//
// A session sends a mouse event only when the program asked for it, so
// events the program never saw are not drawn either.
//
// List Mouse after [Zoom], so the pointer is drawn where the zoom draws its
// cell, and after [Highlight], so it is not dimmed. With zooms that overlap
// in time, the pointer follows the last of them. A [Hide] listed before
// Mouse leaves out the ripples of presses in what it cuts.
func Mouse(opts ...MouseOption) reel.Effect {
	m := mouse{
		pointer: pointerFill,
		buttons: buttonColors,
		glide:   mouseGlide,
		hide:    mouseHide,
		parts:   MouseAll,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&m)
		}
	}
	return m
}

type mouse struct {
	style       PointerStyle
	size        int
	pointer     color.NRGBA
	buttons     [len(buttonColors)]color.NRGBA
	glide, hide time.Duration
	parts       MouseParts
}

// check returns an error for options Start would reject.
func (e mouse) check() error {
	switch {
	case e.style < PointerArrow || e.style > PointerDot:
		return fmt.Errorf("effects: pointer style %d out of range", e.style)
	case e.size < 0:
		return fmt.Errorf("effects: pointer size %d is below zero", e.size)
	}
	return nil
}

func (e mouse) Start(ctx *reel.Context) (reel.Stage, error) {
	if err := e.check(); err != nil {
		return nil, err
	}
	if e.parts == 0 {
		return nil, nil //nolint:nilnil // A nil stage leaves the recording alone.
	}
	// The options are in playback time, the stage works in timeline time.
	timeline := func(d time.Duration) time.Duration {
		return time.Duration(float64(max(d, 0)) * ctx.Speed)
	}
	size := ctx.Layout.Cell.Y
	if e.style == PointerDot {
		size = ctx.Layout.Cell.Y / 2
	}
	s := &mouseStage{
		ctx:     ctx,
		parts:   e.parts,
		buttons: e.buttons,
		glide:   timeline(e.glide),
		hide:    timeline(e.hide),
		fade:    timeline(mouseFade),
		ripple:  timeline(mouseRipple),
		unit:    float64(ctx.Layout.Cell.Y),
		pointer: pointerSprite(e.style, cmp.Or(e.size, size), e.pointer),
	}
	s.zooms, _ = ctx.Value(zoomsKey{}).([]*zoomStage)
	s.cuts, _ = ctx.Value(cutsKey{}).([]*cuts)
	return s.run, nil
}

// isButton reports whether b is a button [Mouse] draws presses of.
func isButton(b vt.MouseButton) bool {
	return b == vt.MouseLeft || b == vt.MouseMiddle || b == vt.MouseRight
}

// nrgbaOr is c as NRGBA, or def when c is nil.
func nrgbaOr(c color.Color, def color.NRGBA) color.NRGBA {
	if c == nil {
		return def
	}
	n, _ := color.NRGBAModel.Convert(c).(color.NRGBA)
	return n
}

// mouseStage is the mouse of one recording.
type mouseStage struct {
	ctx     *reel.Context
	parts   MouseParts
	buttons [len(buttonColors)]color.NRGBA
	// glide, hide, fade, and ripple are in timeline time. A zero hide never
	// hides the pointer.
	glide, hide, fade, ripple time.Duration
	// unit is the height of a cell of the terminal font, which ripples,
	// discs, and trails are sized by.
	unit float64
	// pointer is the pointer's image, with its hotspot at (0, 0).
	pointer *image.NRGBA
	// zooms are those of each [Zoom] before Mouse, and cuts those of each
	// [Hide].
	zooms []*zoomStage
	cuts  []*cuts

	// events are the mouse events read from the timeline, oldest first, and
	// read is the number of timeline events read.
	events []mouseEvent
	read   int
	// reached is when the pointer reaches the cell of the last press, which
	// later events wait for, up to a glide.
	reached time.Duration

	// layout is the rasterizer's layout for screens of size.
	layout render.Layout
	size   image.Point
}

// vec is a point in cells, or in pixels once a [cellView] maps it.
type vec struct{ x, y float64 }

// lerp is the point u of the way from v to w.
func (v vec) lerp(w vec, u float64) vec { return vec{v.x + (w.x-v.x)*u, v.y + (w.y-v.y)*u} }

// mouseEvent is a mouse event at offset at into the timeline, with what it
// left the mouse doing.
type mouseEvent struct {
	vt.MouseEvent

	at time.Duration
	// start is when the pointer starts to glide to the event's cell, from
	// from: at, or once it reaches the cell of a press before it.
	start time.Duration
	from  vec
	// held is the button held after the event, or [vt.MouseUnknown].
	held vt.MouseButton
	// drag is the index of the event that started the drag the event is
	// part of, its release included, or -1.
	drag int
}

// cell is the center of the event's cell.
func (ev mouseEvent) cell() vec { return vec{float64(ev.X) + 0.5, float64(ev.Y) + 0.5} }

func (s *mouseStage) run(f reel.Frame, emit func(reel.Frame)) {
	if f.Cells == nil {
		emit(f)
		return
	}
	s.readEvents()
	if len(s.events) == 0 {
		emit(f)
		return
	}
	if size := f.Cells.Screen.Bounds().Size(); size != s.size {
		s.layout, s.size = s.ctx.Rasterizer.Layout(size.X, size.Y), size
	}
	grid := s.layout.Grid.Add(f.Image.Rect.Min)
	// The mouse holds still from settled on, until the next event.
	last := s.events[len(s.events)-1]
	settled := max(last.start+s.glide, last.at+max(s.ripple, s.hide+s.fade))
	var cuts []time.Duration
	for p := time.Duration(0); p < f.Delay && f.At+s.timeline(p) <= settled; p += s.ctx.Step {
		cuts = append(cuts, p)
	}
	splitFrame(s.ctx, f, trimCuts(cuts, f.Delay, s.ctx.Step), func(p time.Duration) mouseLook {
		return s.lookAt(f, grid, p)
	}, func(l mouseLook) *image.NRGBA {
		return s.draw(f.Image, grid, l)
	}, emit)
}

// timeline is the timeline time playback time p plays.
func (s *mouseStage) timeline(p time.Duration) time.Duration {
	return time.Duration(float64(p) * s.ctx.Speed)
}

// readEvents adds the mouse events of the inputs appended to the timeline
// since it last read.
func (s *mouseStage) readEvents() {
	for ev := range s.ctx.Timeline.EventsFrom(s.read) {
		s.read++
		in, ok := ev.(reel.Input)
		if !ok {
			continue
		}
		for _, st := range vt.DecodeInput(in.Data) {
			if st.Kind == vt.StrokeMouse {
				s.add(in.At(), st.Mouse)
			}
		}
	}
}

// add appends the mouse event m at offset at.
func (s *mouseStage) add(at time.Duration, m vt.MouseEvent) {
	ev := mouseEvent{MouseEvent: m, at: at, start: min(max(at, s.reached), at+s.glide), drag: -1}
	ev.from = ev.cell()
	var prev mouseEvent
	if n := len(s.events); n > 0 {
		prev = s.events[n-1]
		if s.alpha(prev, at) > 0 {
			ev.from = s.pos(n-1, ev.start)
		}
	} else {
		prev.drag = -1
	}
	if m.Action == vt.MousePress && ev.from != ev.cell() {
		s.reached = ev.start + s.glide
	}
	switch {
	case m.Action == vt.MousePress && isButton(m.Button):
		ev.held, ev.drag = m.Button, len(s.events)
	case m.Action == vt.MouseMotion && isButton(m.Button):
		// Motion with a button held drags, even without a press before it.
		ev.held, ev.drag = m.Button, prev.drag
		if prev.held != m.Button {
			ev.drag = len(s.events)
		}
	case m.Action == vt.MouseRelease && (m.Button == prev.held || m.Button == vt.MouseUnknown):
		// X10 and urxvt releases do not say which button was let go.
		ev.drag = prev.drag
	case m.Action == vt.MouseMotion && m.Button == vt.MouseUnknown:
		// Motion with no button held.
	case prev.held != vt.MouseUnknown:
		// A wheel turn, an extra button, or the release of a button other
		// than the one held changes nothing held.
		ev.held, ev.drag = prev.held, prev.drag
	}
	s.events = append(s.events, ev)
}

// pos is where the pointer is at offset t, from the event i on: gliding
// to the cell of the last event up to i it has started gliding to.
func (s *mouseStage) pos(i int, t time.Duration) vec {
	for i > 0 && t < s.events[i].start {
		i--
	}
	ev := s.events[i]
	return ev.from.lerp(ev.cell(), 1-eased(t-ev.start, s.glide))
}

// alpha is the pointer's opacity at offset t, from the event ev on.
func (s *mouseStage) alpha(ev mouseEvent, t time.Duration) uint8 {
	idle := t - ev.at - s.hide
	if s.hide == 0 || ev.held != vt.MouseUnknown || idle < 0 {
		return 0xff
	}
	return uint8(math.Round(0xff * eased(idle, s.fade)))
}

// mouseLook is what the mouse shows at one moment, mapped onto the frame by
// view. The zero mouseLook shows nothing.
type mouseLook struct {
	view cellView
	// pos is the pointer's position, drawn at opacity alpha, and with a
	// disc under it in the color of the button held.
	pos     vec
	alpha   uint8
	held    vt.MouseButton
	ripples [mouseRipples]rippleLook
	trail   trailLook
}

// rippleLook is a ripple of a press of button, age of the way through.
// A zero button is no ripple.
type rippleLook struct {
	at     vec
	button vt.MouseButton
	age    uint8
}

// trailLook is the trail of a drag, along the cells of the events first to
// last, its last leg as far as the point of it nearest end, at opacity
// alpha. A zero alpha is no trail.
type trailLook struct {
	first, last int
	end         vec
	alpha       uint8
}

// cellView maps cells onto a frame's image: column x to x0+w*x, and row y
// to y0+h*y.
type cellView struct{ x0, y0, w, h float64 }

func (v cellView) at(c vec) vec { return vec{v.x0 + v.w*c.x, v.y0 + v.h*c.y} }

// lookAt is the look at playback offset p into f, a frame drawn with its
// cell grid at grid.
func (s *mouseStage) lookAt(f reel.Frame, grid image.Rectangle, p time.Duration) mouseLook {
	t := f.At + s.timeline(p)
	l := s.look(t)
	if l == (mouseLook{}) {
		return l
	}
	l.view = cellView{x0: float64(grid.Min.X), y0: float64(grid.Min.Y), w: float64(s.layout.Cell.X), h: float64(s.layout.Cell.Y)}
	// A zoom draws a piece of a frame as of the middle of its first step,
	// or a paused one as of its start.
	if !f.Paused {
		t += s.timeline(s.ctx.Step / 2)
	}
	for _, z := range slices.Backward(s.zooms) {
		if x, y, ok := z.axes(t); ok {
			l.view = cellView{x0: l.view.x0 + x.off, y0: l.view.y0 + y.off, w: l.view.w * x.k, h: l.view.h * y.k}
			break
		}
	}
	return l
}

// look is what the mouse shows at offset t, with a zero view.
func (s *mouseStage) look(t time.Duration) mouseLook {
	i := sort.Search(len(s.events), func(i int) bool { return s.events[i].at > t }) - 1
	if i < 0 {
		return mouseLook{}
	}
	ev := s.events[i]
	l := mouseLook{pos: s.pos(i, t)}
	if s.parts&MousePointer != 0 {
		l.alpha = s.alpha(ev, t)
	}
	if s.parts&MouseClicks != 0 {
		l.held = ev.held
		n := 0
		for j := i; j >= 0 && n < mouseRipples && t-s.events[j].at < s.ripple; j-- {
			if e := s.events[j]; e.Action == vt.MousePress && isButton(e.Button) && !hidden(s.cuts, e.at) {
				age := uint8(0xff * float64(t-e.at) / float64(s.ripple))
				l.ripples[n] = rippleLook{at: e.cell(), button: e.Button, age: age}
				n++
			}
		}
	}
	if s.parts&MouseDrags != 0 {
		l.trail = s.trail(i, t)
	}
	if l.alpha == 0 && l.held == vt.MouseUnknown && l.ripples[0].button == vt.MouseUnknown && l.trail.alpha == 0 {
		return mouseLook{}
	}
	return l
}

// trail is the trail at offset t, after event i: of the drag going on, or
// of one released less than a ripple before t. A drag that never moved
// has none. The trail follows the path the program was sent, and ends where
// the pointer is, so it never runs ahead of the pointer gliding along it.
func (s *mouseStage) trail(i int, t time.Duration) trailLook {
	// Events the pointer has not started gliding to are not on its path yet.
	for i > 0 && t < s.events[i].start {
		i--
	}
	r := i
	for r >= 0 && s.events[r].drag < 0 && t-s.events[r].at < s.ripple {
		r--
	}
	if r < 0 || s.events[r].drag < 0 {
		return trailLook{}
	}
	ev := s.events[r]
	start := s.events[ev.drag].MouseEvent
	if !slices.ContainsFunc(s.events[ev.drag:r+1], func(e mouseEvent) bool { return e.X != start.X || e.Y != start.Y }) {
		return trailLook{}
	}
	alpha := 1.0
	switch {
	case ev.held == vt.MouseUnknown:
		alpha = eased(t-ev.at, s.ripple)
	case r < i:
		// The button was let go without a release the encoding reported.
		return trailLook{}
	}
	if alpha == 0 {
		return trailLook{}
	}
	// Where the pointer is, or was when the next event moved it on.
	end := s.pos(i, t)
	if r < i {
		end = s.events[r+1].from
	}
	return trailLook{first: ev.drag, last: r, end: end, alpha: uint8(math.Round(0xff * alpha))}
}

// trailPath is the path tr draws, in cells: the cells of its events,
// straightened (see [simplify]). The legs before the last were sent, so
// they are drawn whole, and the last as far as the pointer has come along
// it, which is nowhere before the pointer reaches the press.
func (s *mouseStage) trailPath(tr trailLook) []vec {
	cells := make([]vec, 0, tr.last-tr.first+1)
	for _, ev := range s.events[tr.first : tr.last+1] {
		cells = append(cells, ev.cell())
	}
	cells = simplify(cells, mouseTrailTolerance)
	n := len(cells)
	cells[n-1] = closest(tr.end, cells[n-2], cells[n-1])
	return cells
}

// draw returns a copy of src, a frame with its cell grid at grid, with the
// mouse l shows drawn on it, or src when it shows nothing. Ripples and the
// pointer the view puts outside the grid, such as past a zoom's edge, are
// left out, and the trail is cut to the grid.
func (s *mouseStage) draw(src *image.NRGBA, grid image.Rectangle, l mouseLook) *image.NRGBA {
	if l == (mouseLook{}) {
		return src
	}
	img := cloneNRGBA(src)
	shown := func(p vec) bool { return image.Pt(int(math.Floor(p.x)), int(math.Floor(p.y))).In(grid) }
	if tr := l.trail; tr.alpha > 0 {
		cells := s.trailPath(tr)
		var segs []shape
		var box image.Rectangle
		for k := range len(cells) - 1 {
			a, b := l.view.at(cells[k]), l.view.at(cells[k+1])
			// cover scans each shape's box, so a long leg is drawn in
			// pieces about a cell long.
			pieces := int(math.Ceil(math.Hypot(b.x-a.x, b.y-a.y) / s.unit))
			for j := range pieces {
				sh := capsule(a.lerp(b, float64(j)/float64(pieces)), a.lerp(b, float64(j+1)/float64(pieces)), s.unit/4)
				segs = append(segs, sh)
				box = box.Union(sh.box)
			}
		}
		// A zoom may put much of the trail off the grid.
		mask := image.NewAlpha(box.Intersect(grid))
		for _, sh := range segs {
			cover(mask, sh)
		}
		c := s.buttons[s.events[tr.first].held]
		paintMask(img, mask, faded(c, 0.5*float64(tr.alpha)/0xff))
	}
	for _, r := range l.ripples {
		if at := l.view.at(r.at); r.button != vt.MouseUnknown && shown(at) {
			u := float64(r.age) / 0xff
			radius := s.unit * (0.3 + 0.9*(1-(1-u)*(1-u)))
			paintMask(img, maskOf(ring(at, radius, max(1.5, s.unit/8))), faded(s.buttons[r.button], 1-u))
		}
	}
	at := l.view.at(l.pos)
	if !shown(at) {
		return img
	}
	if l.held != vt.MouseUnknown {
		paintMask(img, maskOf(disc(at, s.unit*0.45)), faded(s.buttons[l.held], 0.56))
	}
	if l.alpha > 0 {
		tip := image.Pt(int(math.Round(at.x)), int(math.Round(at.y)))
		drawFaded(img, s.pointer, tip.Add(s.pointer.Rect.Min), l.alpha)
	}
	return img
}

// faded is c with its opacity scaled by keep, from 0 to 1.
func faded(c color.NRGBA, keep float64) color.NRGBA {
	c.A = uint8(math.Round(float64(c.A) * keep))
	return c
}

// pointerArrow is the outline of [PointerArrow], with its tip at (0, 0),
// pointerArrowHeight high.
var pointerArrow = []vec{{0, 0}, {0, 15}, {3.6, 11.6}, {6.2, 17.4}, {8.6, 16.4}, {6.1, 10.8}, {10.8, 10.8}}

const pointerArrowHeight = 17.4

// pointerSprite returns the pointer of style, size pixels high, filled with
// fill and outlined in black or white, whichever contrasts more, with its
// hotspot at (0, 0).
func pointerSprite(style PointerStyle, size int, fill color.NRGBA) *image.NRGBA {
	h := float64(max(size, 4))
	edge := max(1, h/16)
	var sh shape
	if style == PointerDot {
		sh = disc(vec{}, h/2)
	} else {
		pts := make([]vec, len(pointerArrow))
		for i, p := range pointerArrow {
			pts[i] = vec{p.x * h / pointerArrowHeight, p.y * h / pointerArrowHeight}
		}
		sh = polygon(pts)
	}
	outline := sh.grow(edge)
	img := image.NewNRGBA(outline.box)
	edgeColor := color.NRGBA{A: 0xff}
	if luma(fill) < 0x80 {
		edgeColor = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	}
	paintMask(img, maskOf(outline), edgeColor)
	paintMask(img, maskOf(sh), fill)
	return img
}

// luma is the perceived brightness of c, from 0 to 255.
func luma(c color.NRGBA) float64 {
	return 0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)
}
