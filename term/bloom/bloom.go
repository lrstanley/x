// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package bloom lays out, composites, and routes input for Bubble Tea
// views.
//
// A view is described each frame as a tree of nodes built from model state:
// [Row] and [Column] share space between children, [Box] draws a Lip Gloss
// frame around one, [Overlay] puts [Layer]s such as dialogs over a base, and
// any Bubble Tea component with a View() string method is a leaf.
//
//	bloom.Column(
//		bloom.Row(
//			bloom.Fixed(20, m.sidebar).ID(paneSidebar).Collapsible(1),
//			bloom.Fill(m.reader).ID(paneReader).Min(40),
//		),
//		m.statusbar,
//	)
//
// # Who sizes what
//
// Constraints go down, sizes come up, and parents place. A parent decides
// each child's rectangle, whether it is collapsed, and how it is clipped. How
// a child takes part in sharing space is set where it is placed, by wrapping
// it in a slot such as [Fixed] or [Fill], not by the child itself. A child
// only knows the size it was given: a component that wants it can implement
// [Layouter], and one that knows its natural size can implement [Measurer].
//
// # Rendering
//
// [Root] turns a tree into a [tea.View] each frame. Leaf strings are parsed
// into cells once and cached, nodes are composited in tree order with every
// node clipped to its parent, and [fx] effects run on a node's cells or on
// the frame beneath a layer. A frame identical to the previous one is not
// composited again. Mouse events are routed to the node under the pointer as
// [MouseMsg]s, using the geometry of the frame on screen.
//
// Layout is a pure function of the tree, so [Solve] can compute the same
// rectangles in Update, for components that need their size there.
package bloom

import (
	tea "charm.land/bubbletea/v2"
)

// Child is anything that can be placed in a layout: every node of this
// package, and any Bubble Tea component, which is drawn from its View
// string. Use [Text] for a plain string.
type Child interface {
	View() string
}

// Layouter is a component that builds its own layout for the space it is
// given, which lets it change with its size. Layout must not change the
// component: it is called while measuring as well as for the final layout.
type Layouter interface {
	Child
	Layout(ctx Context) Child
}

// Measurer is a component that knows its natural size, so its View is not
// measured. Measure returns the size the component wants within avail; the
// result is clamped to avail. Return what the component needs, not avail
// itself: when a layout is drawn at its natural size, avail is unbounded.
type Measurer interface {
	Measure(avail Size) Size
}

// Size is a width and height in cells.
type Size struct {
	W, H int
}

// Context is what a [Layouter] is given to build its layout for.
type Context struct {
	// Width and Height are the space available. Once layout is done they
	// are the exact size the component was given.
	Width, Height int
}

// unbounded is the space given to content that may be larger than its
// viewport, such as the content of a [Scroll].
const unbounded = 1 << 20

// Align is where a child goes on the cross axis of a [Row] or [Column], or
// on either axis of a [Layer].
type Align uint8

const (
	// Stretch makes a child as big as the space it is in. It is the default
	// for children of a row or column.
	Stretch Align = iota
	// Start puts a child at the top or left.
	Start
	// Center puts a child in the middle.
	Center
	// End puts a child at the bottom or right.
	End
)

// offset returns where a child of size n goes in space of size avail.
func (a Align) offset(n, avail int) int {
	switch a {
	case Center:
		return (avail - n) / 2
	case End:
		return avail - n
	case Stretch, Start:
	}
	return 0
}

// meta is what any node can be given for input: an ID to route mouse events
// and focus by, whether it can be focused, and the cursor it shows.
type meta struct {
	id        any
	focusable bool
	scope     bool
	cursor    *tea.Cursor
}
