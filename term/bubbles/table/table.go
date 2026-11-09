// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package table is a table of rows for bloom layouts, which lays out only the
// rows in sight, however many there are. Columns share the width with package
// flex: they grow, shrink, cut their values off with "…", and collapse when
// the table is narrow. Every feature can be turned off: the cursor, cell
// selection, marking rows, the mouse, sorting, the header, the border, the
// scrollbar, and more.
//
// A table is configured with its methods, each of which returns the table:
//
//	jobs := table.New(
//		table.NewColumn("NAME", func(j *Job) string { return j.Name }).Grow(1),
//		table.NewColumn("AGE", func(j *Job) string { return j.Age }).Align(lipgloss.Right),
//	).Title("jobs").MultiSelect(true).Rows(rows)
//
// Keep it in the model by pointer, place it in the layout as any bloom child,
// and pass it the model's keys with the frame on screen:
//
//	func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
//		cmd := m.ui.Update(msg)
//		if msg, ok := msg.(tea.KeyPressMsg); ok {
//			cmd = tea.Batch(cmd, m.jobs.Update(msg, m.ui.Frame()))
//		}
//		return m, cmd
//	}
package table

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Model is a table of rows of type T. Make one with [New]. It is a
// [bloom.Layouter], so it lays itself out for the space it is given.
type Model[T any] struct {
	cols   []Column[T]
	rows   []T
	styles Styles
	keys   KeyMap
	title  string
	on     features
	wheel  int

	onSelect   func(row T) tea.Cmd
	onCell     func(c Cell[T]) tea.Cmd
	onActivate func(row T) tea.Cmd
	onMark     func(row T, marked bool) tea.Cmd

	search  []string     // each row's values, lowercased, for filtering
	natural []int        // each column's widest value, or title
	view    []int        // indexes into rows, filtered and sorted
	marked  map[int]bool // by index into rows

	cursor, col int // the cursor's index into view, and its column
	offset      int // the first row of view to scroll from; see from
	hover       int // the index into rows of the row under the pointer, or -1
	filter      string
	sortCol     int // -1 for the order rows were given in
	sortDesc    bool
}

// features is what a [Model] has turned on.
type features struct {
	selection, cellSelection, multiSelect bool
	mouse, hover, sorting                 bool
	header, border, scrollbar, separators bool
	striped, rowCount, position           bool
}

// New returns a table with the columns given and no rows. It has a cursor
// that selects rows, takes the mouse, sorts by a column whose title is
// clicked, and draws a header, a border, and a scrollbar on it, with the
// row count and the cursor's position as labels on the border and every
// other row striped. Cell selection, multi-select, and column separators
// are off.
func New[T any](cols ...Column[T]) *Model[T] {
	t := &Model[T]{
		styles:  DefaultStyles(),
		keys:    DefaultKeyMap(),
		wheel:   3,
		marked:  map[int]bool{},
		hover:   -1,
		sortCol: -1,
		on: features{
			selection: true, mouse: true, hover: true, sorting: true,
			header: true, border: true, scrollbar: true,
			striped: true, rowCount: true, position: true,
		},
	}
	return t.Columns(cols...)
}

// Columns replaces the table's columns, measuring them against the rows.
// The table stops sorting by a column it no longer has.
func (t *Model[T]) Columns(cols ...Column[T]) *Model[T] {
	t.cols = cols
	if t.sortCol >= len(cols) {
		t.sortCol = -1
	}
	t.col = min(t.col, max(len(cols)-1, 0))
	return t.Refresh()
}

// Rows replaces the table's rows, measuring each column's natural width once,
// here, rather than for each frame. The marks are cleared, and the cursor
// stays on the row with the same index where there still is one.
func (t *Model[T]) Rows(rows []T) *Model[T] {
	t.rows = rows
	clear(t.marked)
	return t.Refresh()
}

// Refresh measures the rows again, and filters and sorts them again, after
// their values changed.
func (t *Model[T]) Refresh() *Model[T] {
	t.natural = slices.Grow(t.natural[:0], len(t.cols))[:len(t.cols)]
	for c, col := range t.cols {
		t.natural[c] = ansi.StringWidth(col.title)
		if col.compare != nil {
			t.natural[c] += 2 // room for a sort arrow
		}
	}
	t.search = slices.Grow(t.search[:0], len(t.rows))[:len(t.rows)]
	var b strings.Builder
	for i, row := range t.rows {
		b.Reset()
		for c, col := range t.cols {
			v := col.value(row)
			t.natural[c] = max(t.natural[c], ansi.StringWidth(v))
			b.WriteString(v)
			b.WriteByte(0)
		}
		t.search[i] = strings.ToLower(b.String())
	}
	t.apply()
	return t
}

// Title sets the title drawn on the top border. The default is none.
func (t *Model[T]) Title(s string) *Model[T] { t.title = s; return t }

// Styles sets how the table looks.
func (t *Model[T]) Styles(s Styles) *Model[T] { t.styles = s; return t }

// KeyMap sets the keys the table handles.
func (t *Model[T]) KeyMap(k KeyMap) *Model[T] { t.keys = k; return t }

// WheelStep sets how many rows a step of the mouse wheel moves the cursor, or
// scrolls while selection is off. The default is 3.
func (t *Model[T]) WheelStep(n int) *Model[T] { t.wheel = max(n, 1); return t }

// Selection turns the cursor on or off. Without it, no row is selected, the
// keys and the wheel scroll the rows, and cell selection, marking with a
// key, and activating with a key are off too.
func (t *Model[T]) Selection(on bool) *Model[T] { t.on.selection = on; return t }

// CellSelection turns on or off the cursor's moving between the cells of a
// row, with the keys or a click, which highlights the selected cell and runs
// [Model.OnCell]. It needs selection.
func (t *Model[T]) CellSelection(on bool) *Model[T] { t.on.cellSelection = on; return t }

// MultiSelect turns on or off marking rows, with [KeyMap.Mark] or a right
// click, for [Model.Marked]. Turning it off clears the marks.
func (t *Model[T]) MultiSelect(on bool) *Model[T] {
	t.on.multiSelect = on
	if !on {
		clear(t.marked)
	}
	return t
}

// Mouse turns on or off every mouse handler: the wheel, clicks, hovering
// rows, sorting by a click on a column's title, and dragging the scrollbar.
func (t *Model[T]) Mouse(on bool) *Model[T] {
	t.on.mouse = on
	if !on {
		t.hover = -1
	}
	return t
}

// Hover turns on or off highlighting the row under the pointer.
func (t *Model[T]) Hover(on bool) *Model[T] {
	t.on.hover = on
	if !on {
		t.hover = -1
	}
	return t
}

// Sorting turns on or off sorting by a click on a column's title. The table
// keeps its order when it is turned off, and [Model.Sort] still sorts it.
func (t *Model[T]) Sorting(on bool) *Model[T] { t.on.sorting = on; return t }

// Header turns the row of column titles on or off.
func (t *Model[T]) Header(on bool) *Model[T] { t.on.header = on; return t }

// Border turns the border on or off, and the labels on it with it: the
// title, the row count, and the cursor's position. Without it, the
// scrollbar is drawn in a column of its own.
func (t *Model[T]) Border(on bool) *Model[T] { t.on.border = on; return t }

// Scrollbar turns the scrollbar on or off.
func (t *Model[T]) Scrollbar(on bool) *Model[T] { t.on.scrollbar = on; return t }

// Separators turns on or off the lines between columns, which are joined to
// the border.
func (t *Model[T]) Separators(on bool) *Model[T] { t.on.separators = on; return t }

// Striped turns on or off the background of every other row.
func (t *Model[T]) Striped(on bool) *Model[T] { t.on.striped = on; return t }

// RowCount turns on or off the label on the top border with how many rows
// there are, and how many the filter shows.
func (t *Model[T]) RowCount(on bool) *Model[T] { t.on.rowCount = on; return t }

// Position turns on or off the label on the bottom border with the cursor's
// position, while selection is on.
func (t *Model[T]) Position(on bool) *Model[T] { t.on.position = on; return t }

// OnSelect sets fn to run when the cursor moves to another row.
func (t *Model[T]) OnSelect(fn func(row T) tea.Cmd) *Model[T] { t.onSelect = fn; return t }

// OnCell sets fn to run when the cursor moves to another cell, or a cell is
// clicked, while cell selection is on.
func (t *Model[T]) OnCell(fn func(c Cell[T]) tea.Cmd) *Model[T] { t.onCell = fn; return t }

// OnActivate sets fn to run for a row on [KeyMap.Activate] or a double click.
func (t *Model[T]) OnActivate(fn func(row T) tea.Cmd) *Model[T] { t.onActivate = fn; return t }

// OnMark sets fn to run when a row is marked or unmarked.
func (t *Model[T]) OnMark(fn func(row T, marked bool) tea.Cmd) *Model[T] { t.onMark = fn; return t }

// Filter shows only the rows with a value containing s, ignoring case. The
// cursor stays on its row where that row is still shown, and where it was
// in sight, as far as it can.
func (t *Model[T]) Filter(s string) *Model[T] {
	if s != t.filter {
		t.filter = s
		t.apply()
	}
	return t
}

// Sort sorts the rows by column col, descending when desc is set, or puts
// them back in the order they were given for -1. A column without
// [Column.Compare] leaves them in that order. The cursor moves to the top.
func (t *Model[T]) Sort(col int, desc bool) *Model[T] {
	t.sort(col, desc)
	return t
}

func (t *Model[T]) sort(col int, desc bool) tea.Cmd {
	if col < 0 || col >= len(t.cols) {
		col = -1
	}
	t.sortCol, t.sortDesc = col, desc
	t.apply()
	return t.move(0, t.col)
}

// apply filters and sorts the rows into view, keeping the cursor on the row
// it was on where that row is still shown, at the same place among the rows
// in sight as far as it can.
func (t *Model[T]) apply() {
	cur, at := -1, t.cursor-t.offset
	if t.cursor < len(t.view) {
		cur = t.view[t.cursor]
	}
	t.view = t.view[:0]
	q := strings.ToLower(t.filter)
	for i := range t.rows {
		if q == "" || strings.Contains(t.search[i], q) {
			t.view = append(t.view, i)
		}
	}
	if t.sortCol >= 0 {
		if cmp := t.cols[t.sortCol].compare; cmp != nil {
			slices.SortStableFunc(t.view, func(a, b int) int {
				if t.sortDesc {
					return cmp(t.rows[b], t.rows[a])
				}
				return cmp(t.rows[a], t.rows[b])
			})
		}
	}
	t.cursor = max(slices.Index(t.view, cur), 0)
	t.offset = max(t.cursor-max(at, 0), 0)
}

// Len returns how many rows the filter shows, and how many there are.
func (t *Model[T]) Len() (shown, total int) { return len(t.view), len(t.rows) }

// Selected returns the row under the cursor, or false while selection is
// off or no row is shown.
func (t *Model[T]) Selected() (row T, ok bool) {
	if !t.on.selection || t.cursor >= len(t.view) {
		return row, false
	}
	return t.rows[t.view[t.cursor]], true
}

// SelectedCell returns the cell under the cursor, or false while cell
// selection is off or no row is shown.
func (t *Model[T]) SelectedCell() (c Cell[T], ok bool) {
	if !t.on.cellSelection || len(t.cols) == 0 {
		return c, false
	}
	row, ok := t.Selected()
	if !ok {
		return c, false
	}
	return t.cell(row, t.col), true
}

// Marked returns the marked rows, in the order they were given.
func (t *Model[T]) Marked() []T {
	out := make([]T, 0, len(t.marked))
	for i := range t.rows {
		if t.marked[i] {
			out = append(out, t.rows[i])
		}
	}
	return out
}

func (t *Model[T]) cell(row T, col int) Cell[T] {
	c := t.cols[col]
	return Cell[T]{Row: row, Col: col, Title: c.title, Value: c.value(row)}
}

// selectable reports whether the table has a cursor, and a row for it.
func (t *Model[T]) selectable() bool { return t.on.selection && len(t.view) > 0 }
