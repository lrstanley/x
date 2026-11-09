// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package table

import "charm.land/bubbles/v2/key"

// KeyMap is the keys a [Model] handles. Disable a binding, or leave it
// empty, to turn that key off.
type KeyMap struct {
	// Up and Down move the cursor a row, or scroll a row while selection is
	// off. PageUp and PageDown move it a page, and Home and End to the first
	// and last rows.
	Up, Down, PageUp, PageDown, Home, End key.Binding
	// Left and Right move the cursor a cell, while cell selection is on.
	Left, Right key.Binding
	// Mark marks the selected row, or unmarks it, while multi-select is on.
	Mark key.Binding
	// Activate runs [Model.OnActivate] for the selected row.
	Activate key.Binding
}

// DefaultKeyMap returns the arrow keys, vi's hjkl, and the usual paging keys,
// with space to mark and enter to activate.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		PageUp:   key.NewBinding(key.WithKeys("pgup", "ctrl+u"), key.WithHelp("pgup", "page up")),
		PageDown: key.NewBinding(key.WithKeys("pgdown", "ctrl+d"), key.WithHelp("pgdn", "page down")),
		Home:     key.NewBinding(key.WithKeys("home", "g"), key.WithHelp("home/g", "first")),
		End:      key.NewBinding(key.WithKeys("end", "G"), key.WithHelp("end/G", "last")),
		Left:     key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "cell left")),
		Right:    key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "cell right")),
		Mark:     key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "mark")),
		Activate: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
	}
}

// ShortHelp returns the bindings for the features turned on, for a help
// view from package charm.land/bubbles/v2/help.
func (t *Model[T]) ShortHelp() []key.Binding {
	out := []key.Binding{t.keys.Up, t.keys.Down}
	if t.on.selection && t.on.cellSelection {
		out = append(out, t.keys.Left, t.keys.Right)
	}
	if t.on.selection && t.on.multiSelect {
		out = append(out, t.keys.Mark)
	}
	if t.on.selection && t.onActivate != nil {
		out = append(out, t.keys.Activate)
	}
	return out
}

// FullHelp returns the bindings for the features turned on, in columns, for
// a help view from package charm.land/bubbles/v2/help.
func (t *Model[T]) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{t.keys.Up, t.keys.Down, t.keys.PageUp, t.keys.PageDown, t.keys.Home, t.keys.End},
		t.ShortHelp()[2:],
	}
}
