// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package bloom

import (
	"image"
	"reflect"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/bloom/canvas"
	"github.com/lrstanley/x/term/bloom/fx"
)

// opKind is what an op does to the frame.
type opKind uint8

const (
	opBlit    opKind = iota // copy src into rect, blanking the rest of it
	opClear                 // blank rect, background and all
	opDraw                  // draw a uv.Drawable into rect
	opEffects               // apply effects to rect of the frame
	opPush                  // draw what follows into its own cells, for effects
	opPop                   // apply effects to those cells and composite them
)

// op is one step of drawing a frame. A frame is a list of them, built by
// walking the laid-out tree, so that a frame can be compared with the one
// before it without drawing it.
type op struct {
	kind     opKind
	src      *canvas.Canvas
	draw     uv.Drawable
	rect     image.Rectangle
	clip     image.Rectangle
	viewport image.Rectangle
	effects  []fx.Effect
	t        float64

	// For opPop: the index of its opPush, the frame entries drawn since it,
	// and how far its effects moved what was drawn, known once executed.
	push         int
	entries      [2]int
	offset       image.Point
	cursorHidden bool // an effect hid the cell of the cursor drawn inside
}

func (o *op) equal(p *op) bool {
	if o.kind != p.kind || o.kind == opDraw || o.src != p.src || o.rect != p.rect ||
		o.clip != p.clip || o.viewport != p.viewport || o.t != p.t || len(o.effects) != len(p.effects) {
		return false
	}
	for i := range o.effects {
		if !sameEffect(o.effects[i], p.effects[i]) {
			return false
		}
	}
	return true
}

// sameEffect reports whether a and b are equal, treating effects that cannot
// be compared, like an [fx.Func], as different.
func sameEffect(a, b fx.Effect) bool {
	if a == nil || b == nil {
		return a == b
	}
	va, vb := reflect.ValueOf(a), reflect.ValueOf(b)
	return va.Type() == vb.Type() && fixed(va) && a == b
}

// fixed reports whether v is comparable and cannot change without changing
// v itself. An effect behind a pointer can change without the pointer
// changing, including one wrapped by another effect, such as [fx.Delay].
func fixed(v reflect.Value) bool {
	if !v.Comparable() {
		return false
	}
	k := v.Kind()
	if k == reflect.Pointer || k == reflect.UnsafePointer || k == reflect.Chan {
		return false
	}
	if k == reflect.Interface {
		return v.IsNil() || fixed(v.Elem())
	}
	if k == reflect.Struct {
		for _, f := range v.Fields() {
			if !fixed(f) {
				return false
			}
		}
	}
	if k == reflect.Array {
		for i := range v.Len() {
			if !fixed(v.Index(i)) {
				return false
			}
		}
	}
	return true
}

// renderer lays out, paints, and composites trees. It keeps what it can
// between frames: the engine's arena, parsed strings, and the last frame, so
// an unchanged frame is not composited again.
type renderer struct {
	engine

	styled   []styledMemo   // by box index, from the last frames
	frames   uint64         // frames painted, to retire unused memos
	style    lipgloss.Style // scratch for comparing styles without allocating
	fxs      []fx.Effect    // effects of this frame's ops, copied from the tree
	prevFxs  []fx.Effect
	env      canvas.Env
	mixer    canvas.Mixer
	parse    bool // false when only the layout is wanted, as for Solve
	canvas   *canvas.Canvas
	scratch  []*canvas.Canvas
	targets  []target
	ops      []op
	prevOps  []op
	entries  []entry
	cursor   *tea.Cursor
	cursorAt int // index of the op drawn after the cursor was placed
	out      string
	drawn    image.Point // size of the last composited frame
	drawnEnv canvas.Env
	frame    *Frame
}

func newRenderer(method ansi.Method) *renderer {
	return &renderer{
		method: method, cache: canvas.NewCache(method),
		env:    canvas.DefaultEnv,
		parse:  true,
		canvas: canvas.New(0, 0, method),
	}
}

// render lays out and draws c in a w×h frame, returning it as a string. For
// a w or h of zero or less, the frame is c's natural size.
func (r *renderer) render(c Child, w, h int) string {
	root := r.paintTree(c, w, h)
	w, h = r.size(root, w, h)

	if r.drawn != image.Pt(w, h) || !r.drawnEnv.Equal(r.env) || !r.sameOps() {
		r.canvas.Resize(w, h)
		r.execute()
		r.out = r.canvas.Render()
		r.drawn, r.drawnEnv = image.Pt(w, h), r.env
	} else {
		// The same ops have the same effects, which moved and hid things the
		// same way.
		for i := range r.ops {
			r.ops[i].offset, r.ops[i].cursorHidden = r.prevOps[i].offset, r.prevOps[i].cursorHidden
		}
	}
	r.placeEntries()
	r.cache.EndFrame(w * h)

	clear(r.prevOps)
	clear(r.prevFxs)
	r.ops, r.prevOps = r.prevOps[:0], r.ops
	r.fxs, r.prevFxs = r.prevFxs[:0], r.fxs
	return r.out
}

// paintTree lays out c and builds the frame's ops and entries, returning the
// root box.
func (r *renderer) paintTree(c Child, w, h int) int {
	clear(r.entries)
	r.ops, r.entries, r.fxs, r.cursor = r.ops[:0], r.entries[:0], r.fxs[:0], nil
	r.frames++
	root := r.layout(c, w, h)
	if root >= 0 {
		r.paint(root, r.area, -1)
	}

	// Memos for boxes not styled this frame would keep their strings alive.
	for i := range r.styled {
		if r.styled[i].used != r.frames {
			r.styled[i] = styledMemo{}
		}
	}
	return root
}

// placeEntries moves the entries and cursor drawn inside each effect pass by
// the offset its effects moved its cells, clips them to where the cells were
// composited, and then hides the cursor if something drawn later covers it.
func (r *renderer) placeEntries() {
	for i := range r.ops {
		o := &r.ops[i]
		if o.kind != opPop {
			continue
		}
		for j := o.entries[0]; j < o.entries[1]; j++ {
			e := &r.entries[j]
			e.rect = e.rect.Add(o.offset)
			e.vis = e.vis.Add(o.offset).Intersect(o.clip)
		}
		if r.cursor != nil && r.cursorAt > o.push && r.cursorAt <= i {
			at := image.Pt(r.cursor.X, r.cursor.Y).Add(o.offset)
			if at.In(o.clip) && !o.cursorHidden {
				r.cursor.X, r.cursor.Y = at.X, at.Y
			} else {
				r.cursor = nil
			}
		}
	}
	r.hideCoveredCursor()
}

// size returns the frame size for a tree laid out in w×h.
func (r *renderer) size(root, w, h int) (width, height int) {
	if root >= 0 {
		rect := r.area
		return rect.Dx(), rect.Dy()
	}
	return max(w, 0), max(h, 0)
}

func (r *renderer) sameOps() bool {
	if len(r.ops) != len(r.prevOps) {
		return false
	}
	for i := range r.ops {
		if !r.ops[i].equal(&r.prevOps[i]) {
			return false
		}
	}
	return true
}

// styledMemo is a text node as drawn in a box, kept since rendering with
// Lip Gloss is slow, as is truncating a long line, and a box tends to look
// the same from one frame to the next.
type styledMemo struct {
	ok       bool
	used     uint64 // the frame it was last used in
	style    lipgloss.Style
	s        string
	truncate bool
	w, h     int
	out      string
}

// textAt returns text node n as drawn at w×h for box i.
func (r *renderer) textAt(i int, n TextNode, w, h int) string {
	if !n.styled && !n.truncate {
		return n.s
	}
	if i >= len(r.styled) {
		r.styled = append(r.styled, make([]styledMemo, i+1-len(r.styled))...)
	}
	m := &r.styled[i]
	// Styles are compared deeply, as they cannot be compared with ==; one
	// with a transform function never matches, and is rendered every time.
	// Comparing through pointers to storage that outlives the call keeps
	// DeepEqual from copying the styles to the heap.
	r.style = n.style
	if m.ok && m.w == w && m.h == h && m.truncate == n.truncate && m.s == n.s && reflect.DeepEqual(&m.style, &r.style) {
		m.used = r.frames
		return m.out
	}
	out := textOf(n, w, h)
	*m = styledMemo{ok: true, used: r.frames, style: n.style, s: n.s, truncate: n.truncate, w: w, h: h, out: out}
	return out
}

func (r *renderer) blit(s string, rect, clip image.Rectangle) {
	var src *canvas.Canvas
	if r.parse {
		src = r.cache.Get(s)
	}
	r.ops = append(r.ops, op{kind: opBlit, src: src, rect: rect, clip: clip})
}

// paint adds the ops and entries for box i, clipped to clip, whose nearest
// ancestor with an entry is entry parent.
func (r *renderer) paint(i int, clip image.Rectangle, parent int) {
	if i < 0 {
		return
	}
	b := &r.boxes[i]
	vis := b.rect.Intersect(clip)
	if b.hidden || vis.Empty() {
		return
	}
	if b.meta.id != nil {
		parent = r.addEntry(b, vis, clip, parent)
	}

	switch n := b.child.(type) {
	case TextNode:
		var text string
		if r.parse {
			text = r.textAt(i, n, b.rect.Dx(), b.rect.Dy())
		}
		r.blit(text, b.rect, vis)
		if n.cursor != nil {
			r.placeCursor(n.cursor, b.rect.Min, vis)
		}
	case SlotNode, FlexNode, GridNode, WrapNode:
		for _, k := range r.kidsOf(i) {
			r.paint(k, vis, parent)
		}
	case BoxNode:
		if b.cramped {
			return
		}
		var frame string
		if r.parse {
			frame = r.textAt(i, TextNode{style: n.style, styled: true}, b.rect.Dx(), b.rect.Dy())
		}
		r.blit(frame, b.rect, vis)
		r.paint(r.firstKid(i), vis, parent)
	case wrapNode:
		if n.kind == wrapTransition && len(n.effects) > 0 {
			r.paintEffects(r.firstKid(i), b.rect, clip, n.t, n.effects, parent, len(r.entries))
			return
		}
		r.paint(r.firstKid(i), vis, parent)
	case OverlayNode:
		kids := r.kidsOf(i)
		r.paint(kids[0], vis, parent)
		for _, k := range kids[1:] {
			r.paintLayer(k, vis, parent)
		}
	case drawableNode:
		r.ops = append(r.ops, op{kind: opDraw, draw: n.d, rect: b.rect, clip: vis})
	case LayerNode:
		// Layers are painted by their overlay.
	case Layouter:
		r.paint(b.expanded, vis, parent)
	default:
		r.blit(r.viewOf(i), b.rect, vis)
	}
}

// paintEffects paints box i into its own cells covering rect, which start
// out blank, to be composited after effects are applied for t, clipped to
// clip, which is also the viewport effects move the cells in. The cells are
// drawn whole, as effects may move cells that start out clipped into view.
// The entries from index from on move with the cells.
func (r *renderer) paintEffects(i int, rect, clip image.Rectangle, t float64, effects []fx.Effect, parent, from int) {
	push := len(r.ops)
	r.ops = append(r.ops, op{kind: opPush, rect: rect})
	r.paint(i, rect, parent)
	r.ops = append(r.ops, op{
		kind: opPop, rect: rect, clip: clip, viewport: clip, t: t, effects: r.snapshot(effects),
		push: push, entries: [2]int{from, len(r.entries)},
	})
}

// snapshot copies effects into storage the renderer owns, so that the
// previous frame's ops do not change when a caller reuses a slice of them.
func (r *renderer) snapshot(effects []fx.Effect) []fx.Effect {
	start := len(r.fxs)
	r.fxs = append(r.fxs, effects...)
	return r.fxs[start:len(r.fxs):len(r.fxs)]
}

func (r *renderer) paintLayer(i int, clip image.Rectangle, parent int) {
	b := &r.boxes[i]
	if b.hidden || b.rect.Empty() {
		return
	}
	n, _ := b.child.(LayerNode)
	if len(n.backdrop) > 0 {
		r.ops = append(r.ops, op{kind: opEffects, rect: clip, viewport: clip, t: n.t, effects: r.snapshot(n.backdrop)})
	}
	if len(n.shadow) > 0 {
		shadow := b.rect.Add(image.Pt(n.shadowX, n.shadowY)).Intersect(clip)
		r.ops = append(r.ops, op{kind: opEffects, rect: shadow, viewport: clip, t: n.t, effects: r.snapshot(n.shadow)})
	}
	from := len(r.entries)
	if b.meta.id != nil || b.modal {
		// A layer with effects may move; placeEntries clips it once moved.
		vis := b.rect.Intersect(clip)
		if len(n.effects) > 0 {
			vis = b.rect
		}
		parent = r.addEntry(b, vis, clip, parent)
	}

	// A layer is opaque: whatever its child does not draw, such as a Space,
	// is blank rather than showing the base through. Cells drawn for effects
	// start out blank; otherwise the layer's rect is blanked first.
	if len(n.effects) > 0 {
		r.paintEffects(r.firstKid(i), b.rect, clip, n.t, n.effects, parent, from)
		return
	}
	r.ops = append(r.ops, op{kind: opClear, rect: b.rect.Intersect(clip)})
	r.paint(r.firstKid(i), b.rect.Intersect(clip), parent)
}

// placeCursor shows c, relative to origin, if that cell is inside clip.
func (r *renderer) placeCursor(c *tea.Cursor, origin image.Point, clip image.Rectangle) {
	at := origin.Add(image.Pt(c.X, c.Y))
	if !at.In(clip) {
		return
	}
	cur := *c
	cur.X, cur.Y = at.X, at.Y
	r.cursor, r.cursorAt = &cur, len(r.ops)
}

// hideCoveredCursor hides the cursor when something drawn after it, such as
// a dialog, covers its cell.
func (r *renderer) hideCoveredCursor() {
	if r.cursor == nil {
		return
	}
	for _, o := range r.ops[r.cursorAt:] {
		if o.kind != opPop && o.kind != opEffects && image.Pt(r.cursor.X, r.cursor.Y).In(o.rect.Intersect(o.clip)) {
			r.cursor = nil
			return
		}
	}
}

// target is where ops draw: the frame, or the cells of an opPush.
type target struct {
	c      *canvas.Canvas
	origin image.Point // frame position of the target's top-left cell
}

// execute draws the frame's ops onto the canvas.
func (r *renderer) execute() {
	r.targets = append(r.targets[:0], target{c: r.canvas})
	for i := range r.ops {
		o := &r.ops[i]
		t := r.targets[len(r.targets)-1]
		switch o.kind {
		case opBlit:
			t.c.Blit(o.src, o.rect.Sub(t.origin), o.clip.Sub(t.origin))
		case opClear:
			t.c.ClearArea(o.rect.Sub(t.origin))
		case opDraw:
			scr := t.c.Screen(o.rect.Min.Sub(t.origin), o.rect.Size(), o.clip.Sub(t.origin))
			o.draw.Draw(scr, scr.Bounds())
		case opEffects:
			s := t.c.Region(o.rect.Sub(t.origin), r.env, &r.mixer)
			s.Viewport = o.viewport.Sub(t.origin)
			for _, e := range o.effects {
				e.Apply(s, o.t)
			}
		case opPush:
			depth := len(r.targets) - 1
			if depth >= len(r.scratch) {
				r.scratch = append(r.scratch, canvas.New(0, 0, r.method))
			}
			sc := r.scratch[depth]
			sc.Resize(o.rect.Dx(), o.rect.Dy())
			r.targets = append(r.targets, target{c: sc, origin: o.rect.Min})
		case opPop:
			r.targets = r.targets[:len(r.targets)-1]
			parent := r.targets[len(r.targets)-1]
			s := t.c.Layer(o.rect.Sub(parent.origin), o.viewport.Sub(parent.origin), parent.c, r.env, &r.mixer)
			for _, e := range o.effects {
				e.Apply(s, o.t)
			}
			parent.c.Composite(s, o.clip.Sub(parent.origin))
			o.offset = s.Offset
			if r.cursor != nil && r.cursorAt > o.push && r.cursorAt <= i {
				at := image.Pt(r.cursor.X, r.cursor.Y).Sub(o.rect.Min)
				o.cursorHidden = !s.Visible(at.X, at.Y)
			}
		}
	}
}
