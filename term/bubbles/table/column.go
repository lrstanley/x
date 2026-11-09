// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package table

import "charm.land/lipgloss/v2"

// Column is a column of a [Model] of rows of type T. Make one with
// [NewColumn], and configure it with its methods, each of which returns the
// column changed.
type Column[T any] struct {
	title    string
	value    func(row T) string
	align    lipgloss.Position
	min, max int
	grow     int
	collapse int
	style    func(row T, s lipgloss.Style) lipgloss.Style
	compare  func(a, b T) int
}

// NewColumn returns a column titled title, where value returns what the cell
// of a row shows.
func NewColumn[T any](title string, value func(row T) string) Column[T] {
	return Column[T]{title: title, value: value}
}

// Align sets where values go in their cells. The default is left.
func (c Column[T]) Align(pos lipgloss.Position) Column[T] { c.align = pos; return c }

// Min lets the column shrink to n cells when the table is narrow, cutting
// its values off with "…". Without it, the column keeps the width of its
// widest value until it collapses. It does not count the cell's padding.
func (c Column[T]) Min(n int) Column[T] { c.min = max(n, 0); return c }

// Max caps the column's width at n cells, not counting the cell's padding.
func (c Column[T]) Max(n int) Column[T] { c.max = max(n, 0); return c }

// Grow sets the column's share of the width left over once every column
// has its natural width. The default is none.
func (c Column[T]) Grow(n int) Column[T] { c.grow = max(n, 0); return c }

// Collapse lets the column be dropped when even the columns' minimum widths
// do not fit, those with the lowest n first. The default, zero, never drops
// it.
func (c Column[T]) Collapse(n int) Column[T] { c.collapse = max(n, 0); return c }

// Style sets fn to change the style of the cell of a row, such as to color
// a status.
func (c Column[T]) Style(fn func(row T, s lipgloss.Style) lipgloss.Style) Column[T] {
	c.style = fn
	return c
}

// Compare sets how rows sort by the column, which [Model.Sort] and a click
// on its title use. Without it, the column cannot be sorted by.
func (c Column[T]) Compare(fn func(a, b T) int) Column[T] { c.compare = fn; return c }

// Cell is a cell of a [Model], as its callbacks report it.
type Cell[T any] struct {
	// Row is the cell's row.
	Row T
	// Col is the index of the cell's column, in the order the columns were
	// given.
	Col int
	// Title is the title of the cell's column.
	Title string
	// Value is what the cell shows.
	Value string
}
