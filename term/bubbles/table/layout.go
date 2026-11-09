// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package table

import (
	"fmt"
	"strconv"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/lrstanley/bloom"
	"github.com/lrstanley/bloom/flex"
	"github.com/lrstanley/bloom/on"
)

// gutter returns the width of the column left of the rows, for the cursor and
// mark glyphs, which is there only while one of them can be shown.
func (t *Model[T]) gutter() int {
	if t.on.selection || t.on.multiSelect {
		return 2
	}
	return 0
}

// chrome returns the cells the table takes around its columns: the border,
// the gutter, and the scrollbar's column across, and the border and header
// down.
func (t *Model[T]) chrome() (w, h int) {
	w = t.gutter()
	switch {
	case t.on.border:
		w, h = w+2, 2
		if t.on.header {
			h += 2 // the titles, and the line under them
		}
	case t.on.header:
		h = 1
	}
	if !t.on.border && t.on.scrollbar {
		w++
	}
	return w, h
}

// Measure implements [bloom.Measurer]: the table wants its columns' natural
// widths, and a row for each row. Without it, the table would be laid out to
// be measured, at whatever size it is offered.
func (t *Model[T]) Measure(avail bloom.Size) bloom.Size {
	w, h := t.chrome()
	for c := range t.cols {
		w += t.natural[c] + 2
	}
	if t.on.separators {
		w += max(len(t.cols)-1, 0)
	}
	return bloom.Size{W: min(w, avail.W), H: min(len(t.view)+h, avail.H)}
}

// MinWidth returns the narrowest the table can be with every column that
// cannot collapse at its minimum width, for the slot it is placed in.
func (t *Model[T]) MinWidth() int {
	w, _ := t.chrome()
	n := 0
	for c, col := range t.cols {
		if col.collapse > 0 {
			continue
		}
		lo := t.natural[c]
		if col.min > 0 {
			lo = min(col.min, lo)
		}
		w += lo + 2
		n++
	}
	if t.on.separators {
		w += max(n-1, 0)
	}
	return w
}

// View implements [bloom.Child], drawing the table at its natural size.
func (t *Model[T]) View() string { return bloom.Render(t, 0, 0) }

// Layout implements [bloom.Layouter]. It must not change the table: the
// handlers it makes do, when they run, reading the frame on screen then
// through ctx.
func (t *Model[T]) Layout(ctx bloom.Context) bloom.Child {
	cw, ch := t.chrome()
	if ctx.Height-ch < 1 || ctx.Width-cw < 4 {
		return bloom.Centered(bloom.Text("table too small").Style(t.styles.Empty).Truncate())
	}
	widths, seps := t.widths(ctx.Width - cw)
	body := t.body(ctx, widths)
	if t.on.border {
		return t.boxed(ctx, widths, seps, body)
	}

	cells := []bloom.Child{bloom.Fill(body)}
	if t.on.scrollbar {
		track := lipgloss.NewStyle().Foreground(t.styles.Border.GetBorderTopForeground())
		cells = append(cells, bloom.Fixed(1, t.scrollbar(ctx).Track("│", track)))
	}
	rows := bloom.Row(cells...)
	if t.on.mouse {
		rows = rows.On(t.onWheel(ctx))
	}
	if !t.on.header {
		return rows
	}
	return bloom.Column(bloom.Fixed(1, t.header(widths)), bloom.Fill(rows))
}

// boxed lays out the rows in body inside the border, under the header in a
// box of its own that the rows' box carries on from, with the labels and the
// scrollbar on the border, and junctions where the separators at seps meet
// it.
func (t *Model[T]) boxed(ctx bloom.Context, widths, seps []int, body bloom.Child) bloom.Child {
	edge := func(s string) bloom.Child { return bloom.Text(s).Style(t.styles.Border) }

	var head bloom.BoxNode
	rows := bloom.Box(t.styles.Border.Border(lipgloss.RoundedBorder()), body)
	if t.on.header {
		// The header's box ends with ├───┤, so the rows' box below it carries
		// on from its border with no top border of its own.
		border := lipgloss.RoundedBorder()
		border.BottomLeft, border.BottomRight = "├", "┤"
		head = bloom.Box(t.styles.Border.Border(border), t.header(widths))
		rows = bloom.Box(t.styles.Border.Border(lipgloss.RoundedBorder(), false, true, true, true), body)
	}
	if t.on.mouse {
		rows = rows.On(t.onWheel(ctx))
	}
	if t.on.scrollbar {
		rows = rows.Edge(bloom.Right, bloom.Stretch, t.scrollbar(ctx))
	}

	// Junctions go before the labels, so the labels cover any beneath them.
	for _, x := range seps {
		rows = rows.EdgeAt(bloom.Bottom, x, edge("┴"))
		if t.on.header {
			head = head.EdgeAt(bloom.Top, x, edge("┬")).EdgeAt(bloom.Bottom, x, edge("┼"))
		} else {
			rows = rows.EdgeAt(bloom.Top, x, edge("┬"))
		}
	}

	top := &rows
	if t.on.header {
		top = &head
	}
	if t.title != "" {
		*top = top.Edge(bloom.Top, bloom.Start, bloom.Text(" "+t.title+" ").Style(t.styles.Title))
	}
	shown, total := t.Len()
	if t.on.rowCount {
		count := fmt.Sprintf(" %s rows ", commas(total))
		if shown != total {
			count = fmt.Sprintf(" %s of %s rows ", commas(shown), commas(total))
		}
		*top = top.Edge(bloom.Top, bloom.End, edge(count))
	}
	if t.on.position && t.on.selection {
		position := " 0/0 "
		if shown > 0 {
			position = fmt.Sprintf(" %s/%s ", commas(t.cursor+1), commas(shown))
		}
		rows = rows.Edge(bloom.Bottom, bloom.End, edge(position))
	}

	if !t.on.header {
		return rows
	}
	return bloom.Column(bloom.Fixed(3, head), bloom.Fill(rows))
}

// scrollbar is the scrollbar for the rows, which can be dragged while the
// table takes the mouse.
func (t *Model[T]) scrollbar(ctx bloom.Context) bloom.ScrollbarNode {
	sb := bloom.ScrollbarOf(t.part(partBody)).Thumb("┃", t.styles.Thumb)
	if t.on.mouse {
		sb = sb.OnScroll(func(offset int) tea.Cmd { return t.scrollTo(ctx.Frame(), offset) })
	}
	return sb
}

// onWheel moves the cursor a few rows a step of the wheel, so the view
// follows it rather than leaving it behind, or scrolls without a cursor.
func (t *Model[T]) onWheel(ctx bloom.Context) on.Handler {
	return on.Scroll(func(_, dy int) tea.Cmd { return t.scrollWheel(ctx.Frame(), dy) })
}

// widths splits width between the columns with package flex, the same
// sharing that rows and columns use, so columns grow, shrink to their
// minimums, and then collapse, lowest priority first. A collapsed column is
// [flex.Collapsed]. seps are the x of each separator from the border's
// outer corner, for the junctions on it.
func (t *Model[T]) widths(width int) (widths, seps []int) {
	tracks := make([]flex.Track, len(t.cols))
	for c, col := range t.cols {
		natural := t.natural[c]
		if col.max > 0 {
			natural = min(natural, col.max)
		}
		// Only columns given a minimum shrink, down to it; the rest keep
		// their natural width until they collapse.
		lo := natural
		if col.min > 0 {
			lo = min(col.min, natural)
		}
		tr := flex.Auto(natural + 2)
		tr.Min, tr.Grow, tr.Collapse = lo+2, col.grow, col.collapse
		if col.max > 0 {
			tr.Max = col.max + 2
		}
		tracks[c] = tr
	}
	gap := 0
	if t.on.separators {
		gap = 1
	}
	widths = flex.Split(nil, width, gap, tracks)
	// When even the columns that cannot collapse do not fit, the rightmost
	// go, rather than the table overflowing its border.
	for last := len(tracks) - 1; last > 0 && overflows(widths, width, gap); last-- {
		tracks[last] = flex.Track{} // empty: no width, and no gap beside it
		widths = flex.Split(widths, width, gap, tracks)
	}
	for c := range tracks {
		if tracks[c] == (flex.Track{}) {
			widths[c] = flex.Collapsed
		}
	}
	if t.on.separators {
		x, first := 1+t.gutter(), true // the border, then the gutter
		for _, w := range widths {
			if w == flex.Collapsed {
				continue
			}
			if !first {
				seps = append(seps, x)
				x++
			}
			x, first = x+w, false
		}
	}
	return widths, seps
}

// overflows reports whether widths, with gap between those shown, are wider
// than width.
func overflows(widths []int, width, gap int) bool {
	total, shown := 0, 0
	for _, w := range widths {
		if w != flex.Collapsed {
			total += w
			shown++
		}
	}
	return total+gap*max(shown-1, 0) > width
}

// line lays out a row of cells: the gutter, then the cell of each column
// that is not collapsed, made by cell, with separators between them.
func (t *Model[T]) line(widths []int, bg lipgloss.Style, gutter bloom.Child, cell func(c int) bloom.Child) bloom.FlexNode {
	kids := make([]bloom.Child, 0, 2*len(widths)+2)
	if g := t.gutter(); g > 0 {
		kids = append(kids, bloom.Fixed(g, gutter))
	}
	first := true
	for c, w := range widths {
		if w == flex.Collapsed {
			continue
		}
		if t.on.separators && !first {
			kids = append(kids, bloom.Fixed(1, bloom.Text("│").Style(bg.Foreground(t.styles.Border.GetBorderTopForeground()))))
		}
		kids = append(kids, bloom.Fixed(w, cell(c)))
		first = false
	}
	// Space the columns leave, with the row's background.
	kids = append(kids, bloom.Fill(bloom.Text("").Style(bg)))
	return bloom.Row(kids...)
}

// header is the row of titles, which sort the table when clicked.
func (t *Model[T]) header(widths []int) bloom.Child {
	return t.line(widths, lipgloss.NewStyle(), bloom.Text(""), func(c int) bloom.Child {
		col := t.cols[c]
		title := col.title
		if c == t.sortCol {
			title += map[bool]string{false: " ▲", true: " ▼"}[t.sortDesc]
		}
		st := t.styles.Header.Padding(0, 1).Align(col.align)
		cell := bloom.Text(title).Style(st).Truncate()
		if !t.on.mouse || !t.on.sorting || col.compare == nil {
			return cell
		}
		return cell.HoverStyle(t.styles.HeaderHover.Padding(0, 1).Align(col.align)).On(on.Click(func() tea.Cmd {
			return t.sort(c, c == t.sortCol && !t.sortDesc)
		}))
	})
}

// body is the rows, of which bloom's list lays out only those in sight.
func (t *Model[T]) body(ctx bloom.Context, widths []int) bloom.Child {
	if len(t.view) == 0 {
		msg := "no rows"
		if t.filter != "" {
			msg = fmt.Sprintf("no rows match %q", t.filter)
		}
		return bloom.Column(bloom.Fill(bloom.Centered(bloom.Text(msg).Style(t.styles.Empty).Truncate()))).
			ID(t.part(partBody))
	}
	list := bloom.List(len(t.view), func(i int) bloom.Child { return t.row(ctx, widths, i) }).
		Offset(t.offset).
		ID(t.part(partBody))
	if t.on.selection {
		list = list.Keep(t.cursor)
	}
	return list
}

// row lays out row i of view, with handlers to select its cells, activate
// it, and mark it.
func (t *Model[T]) row(ctx bloom.Context, widths []int, i int) bloom.Child {
	ri := t.view[i]
	row := t.rows[ri]
	cursor := t.on.selection && i == t.cursor
	marked := t.on.multiSelect && t.marked[ri]

	bg := lipgloss.NewStyle()
	switch {
	case cursor:
		bg = bg.Background(t.styles.Cursor)
	case marked:
		bg = bg.Background(t.styles.Marked)
	case t.on.mouse && t.on.hover && ri == t.hover:
		bg = bg.Background(t.styles.Hover)
	case t.on.striped && i%2 == 1:
		bg = bg.Background(t.styles.Stripe)
	}

	glyph := "  "
	switch {
	case marked:
		glyph = " ●"
	case cursor:
		glyph = " ›"
	}
	gutter := bloom.Text(glyph).Style(t.styles.Gutter.Inherit(bg))

	line := t.line(widths, bg, gutter, func(c int) bloom.Child {
		col := t.cols[c]
		st := t.styles.Cell.Inherit(bg).Padding(0, 1).Align(col.align)
		if col.style != nil {
			st = col.style(row, st)
		}
		if cursor && t.on.cellSelection && c == t.col {
			st = st.Background(t.styles.CursorCell).Bold(true)
		}
		cell := bloom.Text(col.value(row)).Style(st).Truncate()
		if !t.on.mouse {
			return cell
		}
		return cell.On(
			on.Click(func() tea.Cmd { return t.click(ctx.Frame(), ri, c) }),
			on.DoubleClick(func() tea.Cmd {
				if t.onActivate == nil {
					return nil
				}
				return t.onActivate(row)
			}),
			on.RightClick(func() tea.Cmd { return t.toggleMark(ri) }),
		)
	})
	if !t.on.mouse || !t.on.hover {
		return line
	}
	return line.On(on.Hover(func(over bool) tea.Cmd {
		if over {
			t.hover = ri
		} else if t.hover == ri {
			t.hover = -1
		}
		return nil
	}))
}

// commas formats n with thousands separators.
func commas(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
