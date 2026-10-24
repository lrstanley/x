// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import "charm.land/bubbles/v2/key"

type keyMap struct {
	Focus   key.Binding
	Up      key.Binding
	Down    key.Binding
	Deploy  key.Binding
	Theme   key.Binding
	Approve key.Binding
	Reject  key.Binding
	Quit    key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Focus:   key.NewBinding(key.WithKeys("tab", "shift+tab"), key.WithHelp("tab", "focus")),
		Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Deploy:  key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "deploy")),
		Theme:   key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "theme")),
		Approve: key.NewBinding(key.WithKeys("y", "enter"), key.WithHelp("y", "promote")),
		Reject:  key.NewBinding(key.WithKeys("n", "esc"), key.WithHelp("n", "roll back")),
		Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Focus, k.Up, k.Down, k.Deploy, k.Theme, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }

// promptHelp is the help shown while a deploy waits for approval.
func (k keyMap) promptHelp() []key.Binding {
	return []key.Binding{k.Approve, k.Reject, k.Theme, k.Quit}
}
