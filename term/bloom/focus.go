// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package bloom

import (
	"slices"

	tea "charm.land/bubbletea/v2"
)

// Focus is which node has the keyboard focus, by ID. Keep it in the model:
// the layout only says which nodes can be focused, with Focusable, and where
// focus scopes are.
//
// [Focus.Push] confines focus to a scope, such as a modal [Layer], until
// [Focus.Pop] puts it back where it was:
//
//	case openConfirmMsg:
//		m.focus.Push(dialogConfirm)
//		m.focus.Set(buttonOK)
//	case closeConfirmMsg:
//		m.focus.Pop()
//	case tea.KeyPressMsg:
//		if msg.String() == "tab" {
//			m.focus.Next(m.ui.Frame())
//		}
type Focus struct {
	current any
	scopes  []focusScope
}

type focusScope struct {
	id, prev any
}

// Current returns the ID of the focused node, or nil.
func (f *Focus) Current() any { return f.current }

// Is reports whether the node with ID id is focused.
func (f *Focus) Is[K comparable](id K) bool { return f.current == any(id) }

// Set focuses the node with ID id.
func (f *Focus) Set[K comparable](id K) { f.current = id }

// Clear removes the focus.
func (f *Focus) Clear() { f.current = nil }

// Push confines focus to the scope with ID scope, clearing it until a node
// there is focused, and remembers the focus to go back to on [Focus.Pop].
func (f *Focus) Push[K comparable](scope K) {
	// Clipping makes append copy, so a copy of the model that still holds
	// the old scopes is not changed by this one.
	f.scopes = append(slices.Clip(f.scopes), focusScope{id: scope, prev: f.current})
	f.current = nil
}

// Pop leaves the innermost scope, focusing what was focused when it was
// pushed.
func (f *Focus) Pop() {
	if n := len(f.scopes); n > 0 {
		f.current = f.scopes[n-1].prev
		f.scopes = f.scopes[:n-1]
	}
}

// Scope returns the ID of the innermost scope pushed, or nil.
func (f *Focus) Scope() any {
	if n := len(f.scopes); n > 0 {
		return f.scopes[n-1].id
	}
	return nil
}

// Within reports whether the focused node is inside the scope with ID scope
// in frame.
func (f *Focus) Within[K comparable](frame *Frame, scope K) bool {
	i := frame.index(f.current)
	return i >= 0 && frame.inScope(i, scope)
}

// Next focuses the next node that can be focused, in the order of frame,
// inside the innermost scope pushed, wrapping around. It reports whether
// there was one.
func (f *Focus) Next(frame *Frame) bool { return f.step(frame, 1) }

// Prev focuses the previous node that can be focused, as [Focus.Next] does.
func (f *Focus) Prev(frame *Frame) bool { return f.step(frame, -1) }

func (f *Focus) step(frame *Frame, dir int) bool {
	if frame == nil {
		return false
	}
	var order []int
	at := -1
	for i := range frame.entries {
		if f.focusable(frame, i) {
			if frame.entries[i].id == f.current {
				at = len(order)
			}
			order = append(order, i)
		}
	}
	if len(order) == 0 {
		return false
	}
	switch {
	case at >= 0:
		at = (at + dir + len(order)) % len(order)
	case dir > 0:
		at = 0
	default:
		at = len(order) - 1
	}
	f.current = frame.entries[order[at]].id
	return true
}

// Click focuses the innermost node that can be focused under a mouse event,
// and reports whether there was one.
func (f *Focus) Click(m MouseMsg) bool {
	if m.frame == nil || m.Outside {
		return false
	}
	for i := m.entry; i >= 0; i = m.frame.entries[i].parent {
		if f.focusable(m.frame, i) {
			f.current = m.frame.entries[i].id
			return true
		}
	}
	return false
}

// focusable reports whether entry i of frame can take focus now: it can be
// focused, is on screen, and is inside the innermost scope pushed.
func (f *Focus) focusable(frame *Frame, i int) bool {
	e := &frame.entries[i]
	if !e.focusable || e.vis.Empty() {
		return false
	}
	scope := f.Scope()
	return scope == nil || frame.inScope(i, scope)
}

// Update handles tab and shift+tab key presses, and routed mouse clicks.
// Tab keys are handled even when frame has no focusable nodes; clicks report
// whether [Focus.Click] found a node in the current scope.
func (f *Focus) Update(msg tea.Msg, frame *Frame) bool {
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		switch m.String() {
		case "tab":
			f.Next(frame)
			return true
		case "shift+tab":
			f.Prev(frame)
			return true
		}
	case MouseMsg:
		if _, ok := m.Msg.(tea.MouseClickMsg); ok {
			return f.Click(m)
		}
	}
	return false
}
