// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package bloom

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/term/bloom/anim"
	"github.com/lrstanley/x/term/bloom/flex"
	"github.com/lrstanley/x/term/bloom/fx"
)

// TextNode is a string, optionally styled. Build one with [Text].
type TextNode struct {
	meta

	s        string
	style    lipgloss.Style
	styled   bool
	truncate bool
}

// Text returns a node showing s, a string that may hold ANSI styles.
func Text(s string) TextNode { return TextNode{s: s} }

// Style renders the text with style at the size the node is given, so a
// background fills the node and long lines wrap to its width. A width or
// height set on style itself is kept.
func (n TextNode) Style(style lipgloss.Style) TextNode {
	n.style, n.styled = style, true
	return n
}

// Truncate shows one line, replacing newlines with spaces and cutting text
// that does not fit with "…". Padding and borders count against its width.
func (n TextNode) Truncate() TextNode {
	n.s, n.truncate = lineBreaks.Replace(n.s), true
	return n
}

// ID sets the ID mouse events and focus refer to the node by.
func (n TextNode) ID[K comparable](id K) TextNode { n.id = id; return n }

// Focusable makes the node one that [Focus] can move to.
func (n TextNode) Focusable() TextNode { n.focusable = true; return n }

// Cursor shows the terminal cursor at c, relative to the node's top-left
// cell, while that cell is on screen and not covered by a later layer.
func (n TextNode) Cursor(c *tea.Cursor) TextNode { n.cursor = c; return n }

// View implements [Child], drawing the node at its natural size.
func (n TextNode) View() string { return Render(n, 0, 0) }

// sizing is how a slot takes part in sharing a row's or column's main axis.
type sizing uint8

const (
	sizeAuto sizing = iota
	sizeFixed
	sizeFill
	sizePercent
)

// SlotNode places a child in a [Row] or [Column] with a sizing rule. Build
// one with [Fixed], [Fill], [Grow], [Percent], or [Auto]. A child given to
// a row or column directly is placed as with [Auto].
type SlotNode struct {
	meta

	child     Child
	sizing    sizing
	n         int // cells for Fixed, weight for Fill, percent for Percent
	min, max  int
	collapse  int
	align     Align
	aligned   bool
	animation *layoutAnimation
}

// Fixed places child at exactly n cells on the main axis. A nil child is
// empty space, such as Fixed(1, nil) for a blank line in a column; [Space]
// is empty space that grows to fill instead.
func Fixed(n int, child Child) SlotNode { return SlotNode{child: child, sizing: sizeFixed, n: n} }

// Fill places child in an equal share of the space left by its siblings.
func Fill(child Child) SlotNode { return Grow(1, child) }

// Grow places child in a share of the space left by its siblings, weighted
// against the other growing children's weights.
func Grow(weight int, child Child) SlotNode {
	return SlotNode{child: child, sizing: sizeFill, n: weight}
}

// Percent places child at p percent of its container's main axis. It can
// shrink when space runs short.
func Percent(p int, child Child) SlotNode {
	return SlotNode{child: child, sizing: sizePercent, n: p}
}

// Auto places child at its natural size. It can shrink when space runs
// short, down to its Min, which is zero unless set.
func Auto(child Child) SlotNode { return SlotNode{child: child} }

// Space is an empty slot that fills the space left by its siblings.
func Space() SlotNode { return Fill(nil) }

// Min sets the smallest size the slot shrinks to on the main axis.
func (n SlotNode) Min(cells int) SlotNode { n.min = cells; return n }

// Max sets the largest size the slot grows to on the main axis.
func (n SlotNode) Max(cells int) SlotNode { n.max = cells; return n }

// Collapsible lets the slot be dropped, with the gap beside it, when its
// container cannot fit the minimum sizes of all its children. Slots with
// the lowest priority are dropped first.
func (n SlotNode) Collapsible(priority int) SlotNode { n.collapse = max(priority, 1); return n }

// Align overrides the container's cross-axis alignment for this slot.
func (n SlotNode) Align(a Align) SlotNode { n.align, n.aligned = a, true; return n }

// ID sets the ID mouse events and focus refer to the slot by.
func (n SlotNode) ID[K comparable](id K) SlotNode { n.id = id; return n }

// Animate smoothly moves and resizes the slot when layout changes, reflowing
// its child at each intermediate size. It needs an ID and a retained [Root];
// without either it does nothing. Zero-size slots animate closed; collapsed
// slots and removed nodes disappear immediately. Options default to [anim.NewValue]
// and are captured when the ID first appears. Root controls the clock and FPS.
func (n SlotNode) Animate(opts ...anim.Option) SlotNode {
	n.animation = &layoutAnimation{opts: opts}
	return n
}

// Focusable makes the slot one that [Focus] can move to.
func (n SlotNode) Focusable() SlotNode { n.focusable = true; return n }

// Scope makes the slot a focus scope: [Focus.Within] reports whether focus
// is inside it, and [Focus.Push] confines focus to it. A slot needs an ID to
// be a scope.
func (n SlotNode) Scope() SlotNode { n.scope = true; return n }

// View implements [Child], drawing the slot at its natural size.
func (n SlotNode) View() string { return Render(n, 0, 0) }

// natural reports whether the slot starts at its child's natural size: an
// Auto slot, or a Percent slot in a container of unbounded size, as when a
// layout is drawn at its natural size, which has no size to take a percentage
// of.
func (n SlotNode) natural(avail int) bool {
	return n.sizing == sizeAuto || (n.sizing == sizePercent && avail >= unbounded)
}

// track returns the flex track for the slot, given its natural main-axis size
// and the container's main-axis size.
func (n SlotNode) track(natural, avail int) flex.Track {
	var t flex.Track
	switch {
	case n.natural(avail):
		t = flex.Auto(natural)
	case n.sizing == sizeFixed:
		t = flex.Fixed(n.n)
	case n.sizing == sizeFill:
		t = flex.Fill(n.n)
	default: // sizePercent
		t = flex.Auto(avail * n.n / 100)
	}
	t.Min = max(t.Min, n.min)
	if n.max > 0 {
		t.Max = n.max
	}
	t.Collapse = n.collapse
	return t
}

// FlexNode lays children out in a row or column. Build one with [Row] or
// [Column].
type FlexNode struct {
	children []Child
	vertical bool
	gap      int
	justify  flex.Justify
	align    Align
}

// Row lays children out left to right.
func Row(children ...Child) FlexNode { return FlexNode{children: children} }

// Column lays children out top to bottom.
func Column(children ...Child) FlexNode { return FlexNode{children: children, vertical: true} }

// Gap puts n cells between children.
func (n FlexNode) Gap(cells int) FlexNode { n.gap = cells; return n }

// Justify sets where children go on the main axis when none of them grow.
func (n FlexNode) Justify(j flex.Justify) FlexNode { n.justify = j; return n }

// Align sets where children go on the cross axis. The default is
// [Stretch].
func (n FlexNode) Align(a Align) FlexNode { n.align = a; return n }

// View implements [Child], drawing the node at its natural size.
func (n FlexNode) View() string { return Render(n, 0, 0) }

// BoxNode draws a Lip Gloss style's frame, its margins, border, padding, and
// background, around a child. Build one with [Box].
type BoxNode struct {
	style lipgloss.Style
	child Child
}

// Box draws style's frame around child, which gets the space inside it. A box
// given less space than its frame takes is not drawn, rather than drawn
// broken.
func Box(style lipgloss.Style, child Child) BoxNode { return BoxNode{style: style, child: child} }

// View implements [Child], drawing the node at its natural size.
func (n BoxNode) View() string { return Render(n, 0, 0) }

// inset returns the cells style's frame takes on each side.
func (n BoxNode) inset() (left, top, right, bottom int) {
	s := n.style
	return s.GetMarginLeft() + s.GetBorderLeftSize() + s.GetPaddingLeft(),
		s.GetMarginTop() + s.GetBorderTopSize() + s.GetPaddingTop(),
		s.GetMarginRight() + s.GetBorderRightSize() + s.GetPaddingRight(),
		s.GetMarginBottom() + s.GetBorderBottomSize() + s.GetPaddingBottom()
}

// wrapKind is what a [wrapNode] does with its child.
type wrapKind uint8

const (
	wrapCenter wrapKind = iota
	wrapScroll
	wrapTransition
)

// wrapNode is a node with one child it places or draws differently.
type wrapNode struct {
	kind    wrapKind
	child   Child
	offset  int
	t       float64
	effects []fx.Effect
}

// View implements [Child], drawing the node at its natural size.
func (n wrapNode) View() string { return Render(n, 0, 0) }

// Centered places child at its natural size in the middle of the space it is
// given.
func Centered(child Child) Child { return wrapNode{kind: wrapCenter, child: child} }

// Scroll gives child as much height as it wants and shows it from row
// offset, clipped to the space the scroll is given. The child is measured
// and drawn whole, so content far larger than the screen, such as a long
// log, is best windowed by the component itself.
func Scroll(child Child, offset int) Child {
	return wrapNode{kind: wrapScroll, child: child, offset: max(offset, 0)}
}

// Transition draws child with effects applied to its cells for progress t,
// from 0, as it starts to enter, to 1, when it is fully shown. See
// [fx.Effect].
func Transition(t float64, child Child, effects ...fx.Effect) Child {
	return wrapNode{kind: wrapTransition, child: child, t: t, effects: effects}
}

// Present draws child with p's progress while visible. When hidden, child
// is absent, taking neither space nor a gap in its container.
func Present(p anim.Presence, child Child, effects ...fx.Effect) Child {
	if !p.Visible() {
		return absentNode{}
	}
	return Transition(p.Progress(), child, effects...)
}

type absentNode struct{}

func (absentNode) View() string { return "" }

// absent distinguishes a hidden presence from an empty slot, which still
// takes space. Slots and single-child wrappers preserve absence.
func absent(c Child) bool {
	switch n := c.(type) {
	case absentNode:
		return true
	case SlotNode:
		return absent(n.child)
	case wrapNode:
		return absent(n.child)
	case LayerNode:
		return n.absent
	}
	return false
}

// drawableNode draws a [uv.Drawable] onto the cells it is given.
type drawableNode struct {
	d uv.Drawable
}

// View implements [Child], drawing the node at its natural size.
func (n drawableNode) View() string { return Render(n, 0, 0) }

// Drawable returns a node that draws d directly onto the cells it is given,
// clipped to them. Its natural size is zero unless d implements [Measurer].
func Drawable(d uv.Drawable) Child { return drawableNode{d: d} }

// OverlayNode puts layers over a base. Build one with [Overlay].
type OverlayNode struct {
	base   Child
	layers []LayerNode
}

// Overlay draws layers, in order, over base, which gets all the overlay's
// space. Layers do not take space from the base.
func Overlay(base Child, layers ...LayerNode) OverlayNode {
	if base == nil {
		base = Text("")
	}
	return OverlayNode{base: base, layers: layers}
}

// View implements [Child], drawing the node at its natural size.
func (n OverlayNode) View() string { return Render(n, 0, 0) }

// Edge is a side of a node or of the screen.
type Edge = fx.Edge

// The sides of a node or of the screen.
const (
	Bottom = fx.Bottom
	Top    = fx.Top
	Left   = fx.Left
	Right  = fx.Right
)

// LayerNode is a child of an [Overlay], such as a dialog, placed over the
// base. Build one with [Layer].
type LayerNode struct {
	meta

	child     Child
	x, y      Align
	offsetX   int
	offsetY   int
	margin    int
	w, h      int
	anchor    any
	side      Edge
	modal     bool
	absent    bool
	backdrop  []fx.Effect
	shadowX   int
	shadowY   int
	shadow    []fx.Effect
	t         float64
	effects   []fx.Effect
	animation *layoutAnimation
}

// Layer returns a layer showing child at its natural size, in the middle of
// its overlay.
func Layer(child Child) LayerNode { return LayerNode{child: child, x: Center, y: Center, t: 1} }

// Anchor sets where the layer goes in its overlay on each axis.
func (n LayerNode) Anchor(x, y Align) LayerNode { n.x, n.y = x, y; return n }

// Offset moves the layer from where it is anchored.
func (n LayerNode) Offset(x, y int) LayerNode { n.offsetX, n.offsetY = x, y; return n }

// Margin keeps the layer at least cells away from the overlay's edges,
// shrinking it if needed.
func (n LayerNode) Margin(cells int) LayerNode { n.margin = max(cells, 0); return n }

// Size sets the layer's size; zero for either is the child's natural size.
// The size is clamped to the overlay, less the margin.
func (n LayerNode) Size(w, h int) LayerNode { n.w, n.h = w, h; return n }

// AnchorTo places the layer against side of the node with ID id in the
// overlay's base, such as a dropdown below its input, and aligned with its
// start, shifted to stay on screen. The layer is hidden while no such node
// is on screen.
func (n LayerNode) AnchorTo[K comparable](id K, side Edge) LayerNode {
	n.anchor, n.side = id, side
	return n
}

// Modal makes the layer take every mouse event: those outside it are
// reported as [MouseMsg]s with Outside set. It is also a focus scope, for
// [Focus.Push].
func (n LayerNode) Modal() LayerNode { n.modal, n.scope = true, true; return n }

// Backdrop applies effects to everything drawn beneath the layer, with the
// layer's transition progress as their strength.
func (n LayerNode) Backdrop(effects ...fx.Effect) LayerNode { n.backdrop = effects; return n }

// Shadow applies effects to the frame beneath the layer, offset by x, y, the
// way a drop shadow darkens what is under it.
func (n LayerNode) Shadow(x, y int, effects ...fx.Effect) LayerNode {
	n.shadowX, n.shadowY, n.shadow = x, y, effects
	return n
}

// Transition draws the layer with effects applied to its cells for progress
// t, as with [Transition], and sets the strength of its backdrop and shadow.
func (n LayerNode) Transition(t float64, effects ...fx.Effect) LayerNode {
	n.t, n.effects = min(max(t, 0), 1), effects
	return n
}

// Presence sets the transition from p, omitting the entire layer, including
// its backdrop, shadow, and input entries, while p is not visible.
func (n LayerNode) Presence(p anim.Presence, effects ...fx.Effect) LayerNode {
	n.absent = !p.Visible()
	return n.Transition(p.Progress(), effects...)
}

// ID sets the ID mouse events and focus refer to the layer by.
func (n LayerNode) ID[K comparable](id K) LayerNode { n.id = id; return n }

// Animate smoothly moves and resizes the layer when layout changes. It needs
// an ID and a retained [Root]; without either it does nothing. Options default
// to [anim.NewValue] and are captured when the ID first appears. Root controls
// the clock and FPS. A hidden anchor or removed layer disappears immediately.
func (n LayerNode) Animate(opts ...anim.Option) LayerNode {
	n.animation = &layoutAnimation{opts: opts}
	return n
}

// View implements [Child], drawing the layer's child at its natural size.
func (n LayerNode) View() string { return Render(n.child, 0, 0) }
