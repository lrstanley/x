// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package table

import (
	"slices"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/lrstanley/bloom"
	"github.com/lrstanley/bloom/flex"
)

// part is the ID of a part of table t, for finding it in a [bloom.Frame].
type part[T any] struct {
	t    *Model[T]
	name string
}

// partBody is the part that holds the rows in sight.
const partBody = "body"

func (t *Model[T]) part(name string) part[T] { return part[T]{t: t, name: name} }

// Update handles the keys of [KeyMap], given f, the frame on screen, which
// tells how many rows are in sight and which columns are shown.
func (t *Model[T]) Update(msg tea.Msg, f *bloom.Frame) tea.Cmd {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	v := t.from(f)
	page := max(v.Visible-1, 1)
	switch {
	case key.Matches(k, t.keys.Up):
		return t.step(v, -1)
	case key.Matches(k, t.keys.Down):
		return t.step(v, 1)
	case key.Matches(k, t.keys.PageUp):
		return t.step(v, -page)
	case key.Matches(k, t.keys.PageDown):
		return t.step(v, page)
	case key.Matches(k, t.keys.Home):
		return t.step(v, -len(t.view))
	case key.Matches(k, t.keys.End):
		return t.step(v, len(t.view))
	case key.Matches(k, t.keys.Left):
		return t.moveCol(f, -1)
	case key.Matches(k, t.keys.Right):
		return t.moveCol(f, 1)
	case key.Matches(k, t.keys.Mark):
		if t.selectable() {
			return t.toggleMark(t.view[t.cursor])
		}
	case key.Matches(k, t.keys.Activate):
		if row, found := t.Selected(); found && t.onActivate != nil {
			return t.onActivate(row)
		}
	}
	return nil
}

// from returns which rows of the list f showed, and takes where it scrolled
// to as the offset to scroll from, which the list may have moved since the
// table last did, such as to keep the cursor in sight once the window was
// resized. Everything that moves the cursor or scrolls calls it first with
// the frame on screen, rather than keeping what an earlier frame said. For a
// nil f, or a frame without the list, it returns an empty view.
func (t *Model[T]) from(f *bloom.Frame) bloom.ListView {
	v, ok := f.List(t.part(partBody))
	if ok {
		t.offset = v.First
	}
	return v
}

// step moves the cursor n rows, or scrolls n rows while selection is off,
// given v, the rows in sight.
func (t *Model[T]) step(v bloom.ListView, n int) tea.Cmd {
	if t.on.selection {
		return t.move(t.cursor+n, t.col)
	}
	t.offset = min(max(t.offset+n, 0), max(len(t.view)-v.Visible, 0))
	return nil
}

// move puts the cursor on row i of view and column col, and runs the
// callbacks for what changed. The list keeps the cursor in sight, scrolling
// from the offset as little as it can.
func (t *Model[T]) move(i, col int) tea.Cmd {
	if len(t.view) == 0 || len(t.cols) == 0 {
		return nil
	}
	i = min(max(i, 0), len(t.view)-1)
	col = min(max(col, 0), len(t.cols)-1)
	rowChanged, colChanged := i != t.cursor, col != t.col
	t.cursor, t.col = i, col
	if !t.on.selection {
		return nil
	}

	row := t.rows[t.view[i]]
	var cmds []tea.Cmd
	if rowChanged && t.onSelect != nil {
		cmds = append(cmds, t.onSelect(row))
	}
	if (rowChanged || colChanged) && t.on.cellSelection && t.onCell != nil {
		cmds = append(cmds, t.onCell(t.cell(row, col)))
	}
	return tea.Batch(cmds...)
}

// moveCol moves the cursor to the nearest column in direction dir that f, the
// frame on screen, shows, skipping those collapsed, while cell selection is
// on. Without a frame, every column counts as shown.
func (t *Model[T]) moveCol(f *bloom.Frame, dir int) tea.Cmd {
	if !t.selectable() || !t.on.cellSelection {
		return nil
	}
	// The columns share the width the rows were given, but the gutter.
	var widths []int
	if body, ok := f.Rect(t.part(partBody)); ok {
		widths, _ = t.widths(body.Dx() - t.gutter())
	}
	for c := t.col + dir; c >= 0 && c < len(t.cols); c += dir {
		if c >= len(widths) || widths[c] != flex.Collapsed {
			return t.move(t.cursor, c)
		}
	}
	return nil
}

// scrollWheel moves the cursor, or scrolls, dy steps of the wheel.
func (t *Model[T]) scrollWheel(f *bloom.Frame, dy int) tea.Cmd {
	return t.step(t.from(f), dy*t.wheel)
}

// scrollTo scrolls so that row offset of view is the first in sight on f,
// the frame on screen, as the scrollbar does, taking the cursor along where
// it would leave sight.
func (t *Model[T]) scrollTo(f *bloom.Frame, offset int) tea.Cmd {
	v := t.from(f)
	if v.Visible <= 0 {
		return nil
	}
	t.offset = min(max(offset, 0), max(len(t.view)-v.Visible, 0))
	if !t.on.selection {
		return nil
	}
	return t.move(min(max(t.cursor, t.offset), t.offset+v.Visible-1), t.col)
}

// click selects the cell of row ri, an index into rows, in column c, or its
// row without cell selection. It finds the row in view again, as the view
// may have scrolled since the frame the click was on was drawn. f is the
// frame on screen.
func (t *Model[T]) click(f *bloom.Frame, ri, c int) tea.Cmd {
	if !t.on.selection {
		return nil
	}
	t.from(f)
	i := slices.Index(t.view, ri)
	if i < 0 {
		return nil
	}
	if !t.on.cellSelection {
		c = t.col
	}
	if i == t.cursor && c == t.col && t.on.cellSelection && t.onCell != nil {
		// A click on the cell under the cursor still reports it.
		return t.onCell(t.cell(t.rows[ri], c))
	}
	return t.move(i, c)
}

// toggleMark marks row ri, an index into rows, or unmarks it, while
// multi-select is on.
func (t *Model[T]) toggleMark(ri int) tea.Cmd {
	if !t.on.multiSelect || ri < 0 || ri >= len(t.rows) {
		return nil
	}
	marked := !t.marked[ri]
	if marked {
		t.marked[ri] = true
	} else {
		delete(t.marked, ri)
	}
	if t.onMark != nil {
		return t.onMark(t.rows[ri], marked)
	}
	return nil
}
