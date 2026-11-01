// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package bloom

import (
	"image"
	"slices"

	tea "charm.land/bubbletea/v2"
)

// entry is a node of a frame that input refers to: one with an ID, or a
// modal layer.
type entry struct {
	id                      any
	rect                    image.Rectangle // where the node was laid out
	vis                     image.Rectangle // the part of rect on screen
	block                   image.Rectangle // for a modal layer, the area it takes events from
	parent                  int             // nearest ancestor entry, or -1
	focusable, scope, modal bool
}

func (r *renderer) addEntry(b *box, vis, clip image.Rectangle, parent int) int {
	e := entry{
		id: b.meta.id, rect: b.rect, vis: vis, parent: parent,
		focusable: b.meta.focusable, scope: b.meta.scope, modal: b.modal,
	}
	if b.modal {
		e.block = clip
	}
	r.entries = append(r.entries, e)
	return len(r.entries) - 1
}

// Frame is the geometry of a laid-out frame: where each node with an ID
// went, which nodes can be focused, and what the mouse can hit. It does not
// change once made, so it is safe to keep.
type Frame struct {
	entries []entry
	cursor  *tea.Cursor
}

// Rect returns where the node with ID id was laid out, which may be partly
// off screen, and whether it is in the frame. Retained layout animations
// report the visual rectangle drawn this frame.
func (f *Frame) Rect[K comparable](id K) (image.Rectangle, bool) {
	if f != nil {
		for i := range f.entries {
			if f.entries[i].id == any(id) {
				return f.entries[i].rect, true
			}
		}
	}
	return image.Rectangle{}, false
}

// hit returns the entry the point p is on, the topmost one drawn, or -1.
// When a modal layer takes the point although it is outside the layer,
// outside is set and the entry is the layer's.
func (f *Frame) hit(p image.Point) (entry int, outside bool) {
	for i := len(f.entries) - 1; i >= 0; i-- {
		e := &f.entries[i]
		if p.In(e.vis) {
			return i, false
		}
		if e.modal && p.In(e.block) {
			return i, true
		}
	}
	return -1, false
}

// path returns the IDs of entry i and its ancestors, outermost first.
func (f *Frame) path(i int) []any {
	var path []any
	for ; i >= 0; i = f.entries[i].parent {
		if id := f.entries[i].id; id != nil {
			path = append(path, id)
		}
	}
	slices.Reverse(path)
	return path
}

// MouseMsg is a mouse event routed to the node under the pointer, sent by
// the [tea.View.OnMouse] handler [Root.View] installs. The [tea.MouseMsg]
// it was made from is still sent to Update too, so handle one or the other.
type MouseMsg struct {
	// Msg is the original event, in screen coordinates.
	Msg tea.MouseMsg
	// Path holds the IDs of the node hit and of the nodes it is inside that
	// have IDs, outermost first, so the last is the node hit.
	Path []any
	// X and Y are where the pointer is, relative to the node hit.
	X, Y int
	// Outside is set when a modal layer took an event outside it, such as a
	// click on the backdrop; Path is the layer's.
	Outside bool

	frame *Frame
	entry int
}

// Target returns the innermost ID in m.Path of type K, which lets a type of
// ID stand for a kind of node, such as one per list item.
func (m MouseMsg) Target[K comparable]() (K, bool) {
	for _, v := range slices.Backward(m.Path) {
		if id, ok := v.(K); ok {
			return id, true
		}
	}
	var zero K
	return zero, false
}

// onMouse routes msg to the node under the pointer as a [MouseMsg].
func (f *Frame) onMouse(msg tea.MouseMsg) tea.Cmd {
	m := msg.Mouse()
	i, outside := f.hit(image.Pt(m.X, m.Y))
	if i < 0 {
		return nil
	}
	origin := f.entries[i].rect.Min
	routed := MouseMsg{
		Msg: msg, Path: f.path(i), X: m.X - origin.X, Y: m.Y - origin.Y,
		Outside: outside, frame: f, entry: i,
	}
	if len(routed.Path) == 0 && !outside {
		return nil
	}
	return func() tea.Msg { return routed }
}

// inScope reports whether entry i is inside the scope with ID scope.
func (f *Frame) inScope(i int, scope any) bool {
	for ; i >= 0; i = f.entries[i].parent {
		if e := &f.entries[i]; e.scope && e.id == scope {
			return true
		}
	}
	return false
}

// index returns the entry with ID id, or -1.
func (f *Frame) index(id any) int {
	if f != nil && id != nil {
		for i := range f.entries {
			if f.entries[i].id == id {
				return i
			}
		}
	}
	return -1
}
