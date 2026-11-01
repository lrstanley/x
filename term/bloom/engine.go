// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package bloom

import (
	"cmp"
	"image"
	"slices"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/bloom/canvas"
	"github.com/lrstanley/x/term/bloom/flex"
)

// box is a node of the tree being laid out, kept in the engine's arena and
// referred to by index, so a frame's tree costs no allocations once the
// arena has grown to fit it. What a box does is decided by the type of its
// child: one of this package's nodes, a [Layouter], or any other component.
type box struct {
	child    Child
	meta     meta
	modal    bool
	parent   int
	kidsOff  int
	kidsLen  int
	expanded int // root of a Layouter's subtree, or -1

	view   string // a component's View, called once per frame
	viewOK bool

	// memo is the last measurement, since a child is often measured with
	// the same space more than once in a frame.
	memoAvail   Size
	memoSize    Size
	memoOK      bool
	expandedFor Size

	rect   image.Rectangle // set by arrange; empty for boxes not laid out
	hidden bool            // collapsed by its container
	// cramped marks a [BoxNode] given less space than its frame, which
	// would show a broken piece of it, such as while a panel animates closed.
	cramped bool
}

// engine measures and arranges a tree. It is reused between frames.
type engine struct {
	method ansi.Method
	cache  *canvas.Cache
	memo   map[memoKey]Size // every measurement this frame
	boxes  []box
	kids   []int // children of each box, in spans
	stack  []int // children of the boxes being built
	tracks []flex.Track
	ints   []int
	motion *layoutMotion
	// moved is where the nearest animated box being arranged is on screen,
	// which the boxes in it animate relative to, so they move with it.
	moved image.Point
	area  image.Rectangle
}

// memoKey is a box measured within some space.
type memoKey struct {
	box   int
	avail Size
}

// layout builds the tree for c and lays it out in a w×h area, returning the
// index of its root box, or -1 for a nil c. A w or h of zero or less is the
// tree's natural size on that axis.
func (e *engine) layout(c Child, w, h int) int {
	// Clearing drops last frame's components and strings, which the arena's
	// storage would otherwise keep alive past a smaller frame.
	clear(e.boxes)
	clear(e.memo)
	e.boxes, e.kids = e.boxes[:0], e.kids[:0]
	if c == nil || absent(c) {
		if e.motion != nil {
			e.motion.begin(w, h)
			e.motion.end()
		}
		return -1
	}
	root := e.build(c, -1)
	if w <= 0 || h <= 0 {
		avail := Size{W: w, H: h}
		if w <= 0 {
			avail.W = unbounded
		}
		if h <= 0 {
			avail.H = unbounded
		}
		s := e.measure(root, avail)
		w, h = s.W, s.H
	}
	e.area = image.Rect(0, 0, w, h)
	if e.motion != nil {
		e.motion.begin(w, h)
	}
	e.arrange(root, e.area)
	if e.motion != nil {
		e.motion.end()
	}
	return root
}

// build adds a box for c and its children, returning its index.
func (e *engine) build(c Child, parent int) int {
	i := len(e.boxes)
	e.boxes = append(e.boxes, box{parent: parent, child: c, expanded: -1})

	var kids []Child
	b := &e.boxes[i]
	switch n := c.(type) {
	case TextNode:
		b.meta = n.meta
	case SlotNode:
		b.meta, kids = n.meta, []Child{n.child}
	case FlexNode:
		kids = n.children
	case GridNode:
		kids = n.children
	case WrapNode:
		kids = n.children
	case BoxNode:
		kids = []Child{n.child}
	case wrapNode:
		kids = []Child{n.child}
	case OverlayNode:
		kids = make([]Child, 0, 1+len(n.layers))
		base := n.base
		if absent(base) {
			base = Text("")
		}
		kids = append(kids, base)
		for _, l := range n.layers {
			kids = append(kids, l)
		}
	case LayerNode:
		b.meta, b.modal, kids = n.meta, n.modal, []Child{n.child}
	}

	_, grid := c.(GridNode)
	start := len(e.stack)
	for _, k := range kids {
		if k == nil || absent(k) {
			continue
		}
		if _, slot := k.(SlotNode); slot && grid {
			// A slot sizes its content within a grid cell as it would in a row.
			k = Row(k)
		}
		e.stack = append(e.stack, e.build(k, i))
	}
	e.boxes[i].kidsOff, e.boxes[i].kidsLen = len(e.kids), len(e.stack)-start
	e.kids = append(e.kids, e.stack[start:]...)
	e.stack = e.stack[:start]
	return i
}

func (e *engine) kidsOf(i int) []int {
	b := &e.boxes[i]
	return e.kids[b.kidsOff : b.kidsOff+b.kidsLen]
}

func (e *engine) firstKid(i int) int {
	if kids := e.kidsOf(i); len(kids) > 0 {
		return kids[0]
	}
	return -1
}

// expand returns the root of the subtree l builds for avail, building it
// again only when avail changed.
func (e *engine) expand(i int, l Layouter, avail Size) int {
	if b := &e.boxes[i]; b.expanded >= 0 && b.expandedFor == avail {
		return b.expanded
	}
	root := -1
	if child := l.Layout(Context{Width: avail.W, Height: avail.H}); child != nil {
		root = e.build(child, i)
	}
	e.boxes[i].expanded, e.boxes[i].expandedFor = root, avail
	return root
}

// viewOf returns a component's View, calling it once per frame.
func (e *engine) viewOf(i int) string {
	if b := &e.boxes[i]; !b.viewOK {
		b.view, b.viewOK = b.child.View(), true
	}
	return e.boxes[i].view
}

// textOf returns what a text node shows at w×h, or at its natural size for
// a w or h of zero. A width or height the style sets itself is kept.
func textOf(n TextNode, w, h int) string {
	if n.truncate {
		if tf := n.style.GetTransform(); tf != nil {
			// The transform goes first, so what it makes is kept to one line.
			n.s, n.style = lineBreaks.Replace(tf(n.s)), n.style.UnsetTransform()
		}
		room := w
		if room <= 0 {
			room = unbounded
		}
		if inner := lineWidth(n.style, room); ansi.StringWidth(n.s) > inner {
			n.s = truncateLine(n.s, inner)
		}
	}
	if !n.styled {
		return n.s
	}
	st := n.style
	if w > 0 && st.GetWidth() == 0 {
		st = st.Width(max(w-st.GetHorizontalMargins(), 0))
	}
	if h > 0 && st.GetHeight() == 0 {
		st = st.Height(max(h-st.GetVerticalMargins(), 0))
	}
	return st.Render(n.s)
}

// measure returns the natural size of box i within avail. Every
// measurement is kept for the frame: a row or column measures its children
// in more than one space, and without it, nesting them would measure the
// leaves a number of times exponential in the depth.
func (e *engine) measure(i int, avail Size) Size {
	if i < 0 {
		return Size{}
	}
	if b := &e.boxes[i]; b.memoOK && b.memoAvail == avail {
		return b.memoSize
	}
	key := memoKey{box: i, avail: avail}
	s, ok := e.memo[key]
	if !ok {
		s = e.measureChild(i, avail)
		s = Size{W: min(max(s.W, 0), avail.W), H: min(max(s.H, 0), avail.H)}
		if e.memo == nil {
			e.memo = make(map[memoKey]Size)
		}
		e.memo[key] = s
	}
	b := &e.boxes[i]
	b.memoAvail, b.memoSize, b.memoOK = avail, s, true
	return s
}

func (e *engine) measureChild(i int, avail Size) Size {
	switch n := e.boxes[i].child.(type) {
	case TextNode:
		return e.measureText(n, avail)
	case FlexNode:
		return e.measureFlex(i, n, avail)
	case GridNode:
		return e.measureGrid(i, n, avail)
	case WrapNode:
		return e.flow(i, n, avail.W, image.Point{}, false)
	case BoxNode:
		l, t, r, b := n.inset()
		s := e.measure(e.firstKid(i), Size{W: max(avail.W-l-r, 0), H: max(avail.H-t-b, 0)})
		return Size{W: s.W + l + r, H: s.H + t + b}
	case wrapNode:
		if n.kind == wrapScroll {
			s := e.measure(e.firstKid(i), Size{W: avail.W, H: unbounded})
			return Size{W: s.W, H: min(s.H, avail.H)}
		}
		return e.measure(e.firstKid(i), avail)
	case SlotNode, OverlayNode, LayerNode:
		// A slot outside a row or column, an overlay, and a layer are as big
		// as their first child: the slot's child, or the base.
		return e.measure(e.firstKid(i), avail)
	case drawableNode:
		if m, ok := n.d.(Measurer); ok {
			return m.Measure(avail)
		}
		return Size{}
	case Measurer:
		return n.Measure(avail)
	case Layouter:
		return e.measure(e.expand(i, n, avail), avail)
	default:
		w, h := e.cache.Size(e.viewOf(i))
		return Size{W: w, H: h}
	}
}

// measureText returns the natural size of a text node within avail. A
// styled node is measured from its style's frame rather than rendered,
// unless it is too wide and wraps, or its style changes it in ways only
// rendering tells.
func (e *engine) measureText(n TextNode, avail Size) Size {
	w, h := e.cache.Size(n.s)
	if !n.styled {
		return Size{W: w, H: h}
	}
	// Lip Gloss's Width and Height include padding and borders, not margins.
	st := n.style
	if sw := st.GetWidth(); sw > 0 {
		w = sw + st.GetHorizontalMargins()
	} else {
		w += st.GetHorizontalFrameSize()
	}
	h = max(h+st.GetVerticalPadding()+st.GetVerticalBorderSize(), st.GetHeight()) + st.GetVerticalMargins()
	if n.truncate && st.GetTransform() == nil {
		// A truncated line is cut to fit rather than wrapped, so its size
		// follows from the style alone, without rendering a long line.
		if mw := st.GetMaxWidth(); mw > 0 {
			w = min(w, mw)
		}
		if mh := st.GetMaxHeight(); mh > 0 {
			h = min(h, mh)
		}
		if st.GetWidth() == 0 {
			w = min(w, avail.W)
		}
		return Size{W: w, H: h}
	}
	if st.GetWidth() > 0 || st.GetTransform() != nil || st.GetMaxWidth() > 0 || st.GetMaxHeight() > 0 {
		// The style wraps or changes the text in ways only rendering tells.
		w, h = e.cache.Size(textOf(n, 0, 0))
	}
	if w > avail.W {
		// Too wide: rendered at the available width, where it wraps.
		w, h = e.cache.Size(textOf(n, avail.W, 0))
	}
	return Size{W: w, H: h}
}

// axes returns a size's main and cross lengths for a row, or a column when
// vertical is set, and size builds one back.
func axes(s Size, vertical bool) (main, cross int) {
	if vertical {
		return s.H, s.W
	}
	return s.W, s.H
}

func size(main, cross int, vertical bool) Size {
	if vertical {
		return Size{W: cross, H: main}
	}
	return Size{W: main, H: cross}
}

func (s Size) point() image.Point { return image.Pt(s.W, s.H) }

// slotOf returns how box i is placed in a row or column: its slot, or [Auto]
// for a child given directly.
func (e *engine) slotOf(i int) SlotNode {
	sl, _ := e.boxes[i].child.(SlotNode)
	return sl
}

// measureFlex returns the natural size of a row or column: the natural sizes
// of its children on the main axis, and on the cross axis, the largest of
// its children at the sizes they get when it is that long.
func (e *engine) measureFlex(i int, n FlexNode, avail Size) Size {
	mainAvail, crossAvail := axes(avail, n.vertical)
	kids := e.kidsOf(i)
	main, visible := 0, 0
	for _, k := range kids {
		sl := e.slotOf(k)
		m := sl.track(0, mainAvail).Basis
		if sl.sizing == sizeFill || sl.natural(mainAvail) {
			m, _ = axes(e.measure(k, avail), n.vertical)
			if sl.sizing == sizePercent && sl.n > 0 {
				// The container must be big enough for the percentage of it
				// to fit the child.
				m = (m*100 + sl.n - 1) / sl.n
			}
		}
		m = max(m, sl.min)
		if sl.max > 0 {
			m = min(m, sl.max)
		}
		main += m
		if !sl.track(m, mainAvail).Empty() {
			visible++
		}
	}
	main = min(main+n.gap*max(visible-1, 0), mainAvail)

	sizes, _, mark := e.planFlex(i, n, main, crossAvail)
	cross := 0
	for j, k := range kids {
		if sizes[j] != flex.Collapsed {
			_, c := axes(e.measure(k, size(sizes[j], crossAvail, n.vertical)), n.vertical)
			cross = max(cross, c)
		}
	}
	e.ints = e.ints[:mark]
	return size(main, cross, n.vertical)
}

// planFlex shares mainAvail between the children of row or column i, and
// returns the size and offset of each. Both slices are borrowed from the
// ints arena: give them back with e.ints = e.ints[:mark] once done, after
// the children, which borrow their own, are measured or arranged.
func (e *engine) planFlex(i int, n FlexNode, mainAvail, crossAvail int) (sizes, offsets []int, mark int) {
	kids := e.kidsOf(i)
	tracksAt := len(e.tracks)
	for _, k := range kids {
		sl := e.slotOf(k)
		natural := 0
		if sl.natural(mainAvail) {
			natural, _ = axes(e.measure(k, size(mainAvail, crossAvail, n.vertical)), n.vertical)
		}
		e.tracks = append(e.tracks, sl.track(natural, mainAvail))
	}

	mark = len(e.ints)
	e.ints = slices.Grow(e.ints, 2*len(kids))[:mark+2*len(kids)]
	sizes = flex.Split(e.ints[mark:mark], mainAvail, n.gap, e.tracks[tracksAt:])
	offsets = flex.Place(e.ints[mark+len(kids):mark+len(kids)], sizes, mainAvail, n.gap, n.justify)
	e.tracks = e.tracks[:tracksAt]
	return sizes, offsets, mark
}

// arrange places box i in r, and its children in turn.
func (e *engine) arrange(i int, r image.Rectangle) {
	if i < 0 {
		return
	}
	if n, ok := e.boxes[i].child.(SlotNode); ok && e.motion != nil && n.id != nil && n.animation != nil {
		r = e.motion.rect(n.id, n.animation, r, e.moved)
		defer func(at image.Point) { e.moved = at }(e.moved)
		e.moved = r.Min
	}
	e.boxes[i].rect = r
	switch n := e.boxes[i].child.(type) {
	case SlotNode:
		e.arrange(e.firstKid(i), r)
	case FlexNode:
		e.arrangeFlex(i, n, r)
	case GridNode:
		e.arrangeGrid(i, n, r)
	case WrapNode:
		e.flow(i, n, r.Dx(), r.Min, true)
	case BoxNode:
		// Built directly, as image.Rect would swap the edges of a frame too
		// big for r into a rectangle over the frame, rather than an empty one.
		l, t, right, b := n.inset()
		e.boxes[i].cramped = r.Dx() < l+right || r.Dy() < t+b
		minX, minY := min(r.Min.X+l, r.Max.X), min(r.Min.Y+t, r.Max.Y)
		e.arrange(e.firstKid(i), image.Rectangle{
			Min: image.Pt(minX, minY),
			Max: image.Pt(max(r.Max.X-right, minX), max(r.Max.Y-b, minY)),
		})
	case wrapNode:
		e.arrangeWrap(i, n, r)
	case OverlayNode:
		kids := e.kidsOf(i)
		e.arrange(kids[0], r)
		for _, k := range kids[1:] {
			e.arrangeLayer(k, r)
		}
	case Layouter:
		e.arrange(e.expand(i, n, Size{W: r.Dx(), H: r.Dy()}), r)
	}
}

func (e *engine) arrangeWrap(i int, n wrapNode, r image.Rectangle) {
	kid := e.firstKid(i)
	switch n.kind {
	case wrapCenter:
		s := e.measure(kid, Size{W: r.Dx(), H: r.Dy()})
		at := r.Min.Add(image.Pt(Center.offset(s.W, r.Dx()), Center.offset(s.H, r.Dy())))
		e.arrange(kid, image.Rectangle{Min: at, Max: at.Add(s.point())})
	case wrapScroll:
		s := e.measure(kid, Size{W: r.Dx(), H: unbounded})
		top := r.Min.Y - n.offset
		e.arrange(kid, image.Rect(r.Min.X, top, r.Max.X, top+max(s.H, r.Dy())))
	case wrapTransition:
		e.arrange(kid, r)
	}
}

func (e *engine) arrangeFlex(i int, n FlexNode, r image.Rectangle) {
	mainAvail, crossAvail := axes(Size{W: r.Dx(), H: r.Dy()}, n.vertical)
	sizes, offsets, mark := e.planFlex(i, n, mainAvail, crossAvail)
	for j, k := range e.kidsOf(i) {
		if sizes[j] == flex.Collapsed {
			e.boxes[k].hidden = true
			continue
		}
		sl := e.slotOf(k)
		align := n.align
		if sl.aligned {
			align = sl.align
		}
		cross, crossAt := crossAvail, 0
		if align != Stretch {
			_, cross = axes(e.measure(k, size(sizes[j], crossAvail, n.vertical)), n.vertical)
			crossAt = align.offset(cross, crossAvail)
		}
		at := r.Min.Add(size(offsets[j], crossAt, n.vertical).point())
		e.arrange(k, image.Rectangle{Min: at, Max: at.Add(size(sizes[j], cross, n.vertical).point())})
	}
	e.ints = e.ints[:mark]
}

// arrangeLayer places a layer of an overlay laid out in r.
func (e *engine) arrangeLayer(i int, r image.Rectangle) {
	n, _ := e.boxes[i].child.(LayerNode)
	area := r.Inset(n.margin)
	kid := e.firstKid(i)

	w, h := max(n.w, 0), max(n.h, 0)
	if w == 0 || h == 0 {
		s := e.measure(kid, Size{W: area.Dx(), H: area.Dy()})
		w, h = cmp.Or(w, s.W), cmp.Or(h, s.H)
	}
	w, h = min(w, area.Dx()), min(h, area.Dy())

	at := area.Min.Add(image.Pt(n.x.offset(w, area.Dx()), n.y.offset(h, area.Dy())))
	if n.anchor != nil {
		target, ok := e.rectOf(n.anchor)
		if !ok {
			e.boxes[i].hidden = true
			return
		}
		switch n.side {
		case Bottom:
			at = image.Pt(target.Min.X, target.Max.Y)
		case Top:
			at = image.Pt(target.Min.X, target.Min.Y-h)
		case Right:
			at = image.Pt(target.Max.X, target.Min.Y)
		case Left:
			at = image.Pt(target.Min.X-w, target.Min.Y)
		}
		at.X = min(max(at.X, area.Min.X), area.Max.X-w)
		at.Y = min(max(at.Y, area.Min.Y), area.Max.Y-h)
	}
	at = at.Add(image.Pt(n.offsetX, n.offsetY))

	lr := image.Rectangle{Min: at, Max: at.Add(image.Pt(w, h))}
	if e.motion != nil && n.id != nil && n.animation != nil {
		lr = e.motion.rect(n.id, n.animation, lr, e.moved)
		defer func(at image.Point) { e.moved = at }(e.moved)
		e.moved = lr.Min
	}
	e.boxes[i].rect = lr
	e.arrange(kid, lr)
}

// rectOf returns the rectangle of the box with ID id, if some of it is on
// screen: laid out, and not clipped away by the boxes it is in.
func (e *engine) rectOf(id any) (image.Rectangle, bool) {
	for i := range e.boxes {
		b := &e.boxes[i]
		if b.meta.id != id || b.hidden {
			continue
		}
		vis := b.rect
		for p := b.parent; p >= 0 && !vis.Empty(); p = e.boxes[p].parent {
			vis = vis.Intersect(e.boxes[p].rect)
		}
		if !vis.Empty() {
			return b.rect, true
		}
	}
	return image.Rectangle{}, false
}
