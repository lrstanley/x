// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package table

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"bloomfx.dev/bloom"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type item struct {
	id         int
	name, kind string
}

func items(n int) []item {
	kinds := []string{"alpha", "beta", "gamma"}
	out := make([]item, n)
	for i := range out {
		out[i] = item{id: i + 1, name: fmt.Sprintf("item-%05d", i+1), kind: kinds[i%len(kinds)]}
	}
	return out
}

// newTable is a table of n items: ID, KIND, which collapses first, NAME,
// which grows, and TAG.
func newTable(n int) *Model[item] {
	return New(
		NewColumn("ID", func(i item) string { return strconv.Itoa(i.id) }).
			Align(lipgloss.Right).
			Compare(func(a, b item) int { return cmp.Compare(a.id, b.id) }),
		NewColumn("KIND", func(i item) string { return i.kind }).Collapse(1),
		NewColumn("NAME", func(i item) string { return i.name }).
			Grow(1).
			Compare(func(a, b item) int { return strings.Compare(a.name, b.name) }),
		NewColumn("TAG", func(i item) string { return "tag-" + i.kind }),
	).Rows(items(n))
}

// harness draws a table in a w×h window with a root, as a program does.
type harness struct {
	r    *bloom.Root
	tbl  *Model[item]
	w, h int
	out  string // the frame last drawn, without styles
}

func newHarness(tbl *Model[item], w, h int) *harness {
	hs := &harness{r: bloom.New(), tbl: tbl, w: w, h: h}
	hs.draw()
	return hs
}

func (hs *harness) draw() { hs.out = ansi.Strip(hs.r.Render(hs.w, hs.h, hs.tbl)) }

var keyCodes = map[string]rune{
	"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
	"pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown, "home": tea.KeyHome, "end": tea.KeyEnd,
	"enter": tea.KeyEnter, "space": tea.KeySpace,
}

// key presses the keys named, drawing after each.
func (hs *harness) key(names ...string) {
	for _, name := range names {
		code, ok := keyCodes[name]
		if !ok {
			panic("no key " + name)
		}
		msg := tea.KeyPressMsg{Code: code}
		if name == "space" {
			msg.Text = " "
		}
		hs.tbl.Update(msg, hs.r.Frame())
		hs.draw()
	}
}

// mouse sends the mouse messages given through the root, drawing after each.
func (hs *harness) mouse(msgs ...tea.Msg) {
	for _, msg := range msgs {
		hs.r.Update(msg)
		hs.draw()
	}
}

func (hs *harness) wheel(b tea.MouseButton, n int) {
	for range n {
		hs.mouse(tea.MouseWheelMsg{X: 10, Y: 6, Button: b})
	}
}

func (hs *harness) click(x, y int) {
	hs.r.ClickAt(x, y)
	hs.draw()
}

func (hs *harness) rightClick(x, y int) {
	hs.mouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseRight}, tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseRight})
}

// col returns the column on screen where s starts on row y.
func (hs *harness) col(y int, s string) int {
	line := strings.Split(hs.out, "\n")[y]
	i := strings.Index(line, s)
	if i < 0 {
		panic(fmt.Sprintf("no %q on row %d:\n%s", s, y, hs.out))
	}
	return ansi.StringWidth(line[:i])
}

// view returns which rows the list showed.
func (hs *harness) view() bloom.ListView {
	v, _ := hs.r.Frame().List(hs.tbl.part(partBody))
	return v
}

// cursorAt returns the cursor's row, and how far down the rows in sight it
// is drawn.
func (hs *harness) cursorAt() (row, onScreen int) {
	return hs.tbl.cursor, hs.tbl.cursor - hs.view().First
}

// The default table has a 3 row header box, so its rows start on row 3, and
// its first cell after the border and gutter at column 3.
const firstRow = 3

func TestScrolling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		do   func(hs *harness)
		row  int
		// onScreen is how far down the rows in sight the cursor is, or -1 for
		// the last row in sight.
		onScreen int
	}{
		{
			name: "the wheel moves the cursor 3 rows a step before any key is pressed",
			do:   func(hs *harness) { hs.wheel(tea.MouseWheelDown, 3) },
			row:  9, onScreen: 9,
		},
		{
			name: "the cursor stays on the last row in sight while scrolling down",
			do:   func(hs *harness) { hs.wheel(tea.MouseWheelDown, 40) },
			row:  120, onScreen: -1,
		},
		{
			name: "and moves up from it when scrolling back up",
			do: func(hs *harness) {
				hs.wheel(tea.MouseWheelDown, 40)
				hs.wheel(tea.MouseWheelUp, 5)
			},
			row: 105, onScreen: 10,
		},
		{
			name: "the keys carry on from where the wheel left the cursor",
			do: func(hs *harness) {
				hs.wheel(tea.MouseWheelDown, 40)
				hs.key("down")
			},
			row: 121, onScreen: -1,
		},
		{
			name: "rows in sight are counted again after a resize",
			do: func(hs *harness) {
				hs.key("down")
				hs.h = 20
				hs.draw()
				hs.wheel(tea.MouseWheelDown, 30)
			},
			row: 91, onScreen: -1,
		},
		{
			name: "a page is the rows in sight but one",
			do:   func(hs *harness) { hs.key("pgdown") },
			row:  25, onScreen: -1,
		},
		{
			name: "end and home",
			do:   func(hs *harness) { hs.key("end", "home") },
			row:  0, onScreen: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hs := newHarness(newTable(1000), 60, 30)
			tt.do(hs)
			want := tt.onScreen
			if want < 0 {
				want = hs.view().Visible - 1
			}
			if row, onScreen := hs.cursorAt(); row != tt.row || onScreen != want {
				t.Errorf("cursor on row %d, %d down the rows in sight; want row %d, %d down", row, onScreen, tt.row, want)
			}
		})
	}
}

func TestScrollbarBeforeAnyKey(t *testing.T) {
	t.Parallel()

	// The scrollbar is drawn over the rows' right border.
	hs := newHarness(newTable(1000), 60, 30)
	body, _ := hs.r.Frame().Rect(hs.tbl.part(partBody))
	x, y := body.Max.X, body.Min.Y
	hs.mouse(
		tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft},
		tea.MouseMotionMsg{X: x, Y: y + 12, Button: tea.MouseLeft},
		tea.MouseReleaseMsg{X: x, Y: y + 12, Button: tea.MouseLeft},
	)
	if row, onScreen := hs.cursorAt(); row == 0 || onScreen != 0 {
		t.Errorf("cursor on row %d, %d down; want it taken along on the first row in sight", row, onScreen)
	}
}

func TestCellSelectionSkipsCollapsed(t *testing.T) {
	t.Parallel()

	for _, header := range []bool{true, false} {
		t.Run(fmt.Sprintf("header %v", header), func(t *testing.T) {
			t.Parallel()

			// Too narrow for KIND, which collapses: the cursor steps over it.
			hs := newHarness(newTable(10).CellSelection(true).Header(header), 36, 10)
			if strings.Contains(hs.out, " alpha ") || !strings.Contains(hs.out, "tag-alpha") {
				t.Fatalf("want KIND collapsed and TAG shown:\n%s", hs.out)
			}

			var right, left []int
			for range 4 {
				hs.key("right")
				right = append(right, hs.tbl.col)
			}
			for range 4 {
				hs.key("left")
				left = append(left, hs.tbl.col)
			}
			if !slices.Equal(right, []int{2, 3, 3, 3}) || !slices.Equal(left, []int{2, 0, 0, 0}) {
				t.Errorf("right went %v, left %v; want [2 3 3 3] and [2 0 0 0]", right, left)
			}
		})
	}
}

func TestSelection(t *testing.T) {
	t.Parallel()

	t.Run("off, the keys and the wheel scroll, and nothing is selected", func(t *testing.T) {
		t.Parallel()
		var selected int
		hs := newHarness(newTable(100).Selection(false).OnSelect(func(item) tea.Cmd { selected++; return nil }), 40, 12)
		if strings.Contains(hs.out, "›") {
			t.Errorf("cursor drawn with selection off:\n%s", hs.out)
		}
		hs.key("down")
		if first := hs.view().First; first != 1 {
			t.Errorf("down scrolled to row %d, want 1", first)
		}
		hs.wheel(tea.MouseWheelDown, 2)
		if first := hs.view().First; first != 7 {
			t.Errorf("the wheel scrolled to row %d, want 7", first)
		}
		hs.key("end")
		if v := hs.view(); v.First+v.Visible != 100 {
			t.Errorf("end scrolled to %+v, want the last rows", v)
		}
		hs.click(5, firstRow)
		if _, ok := hs.tbl.Selected(); ok || selected != 0 {
			t.Errorf("a row is selected, or OnSelect ran %d times", selected)
		}
		if strings.Contains(hs.out, "/100") {
			t.Errorf("position drawn with selection off:\n%s", hs.out)
		}
	})

	t.Run("on, a click selects a row and runs OnSelect", func(t *testing.T) {
		t.Parallel()
		var got []int
		hs := newHarness(newTable(100).OnSelect(func(i item) tea.Cmd { got = append(got, i.id); return nil }), 40, 12)
		hs.click(5, firstRow+2)
		hs.key("down")
		if !slices.Equal(got, []int{3, 4}) {
			t.Errorf("OnSelect got %v, want [3 4]", got)
		}
		if row, ok := hs.tbl.Selected(); !ok || row.id != 4 {
			t.Errorf("selected %v, %v; want item 4", row, ok)
		}
	})
}

func TestCellSelection(t *testing.T) {
	t.Parallel()

	t.Run("off, the cursor stays in its column and OnCell does not run", func(t *testing.T) {
		t.Parallel()
		var cells int
		hs := newHarness(newTable(10).OnCell(func(Cell[item]) tea.Cmd { cells++; return nil }), 60, 12)
		hs.key("right")
		hs.click(30, firstRow+1)
		if hs.tbl.col != 0 || cells != 0 {
			t.Errorf("cursor in column %d, OnCell ran %d times", hs.tbl.col, cells)
		}
		if _, ok := hs.tbl.SelectedCell(); ok {
			t.Error("a cell is selected with cell selection off")
		}
	})

	t.Run("on, the keys and a click move between cells and report them", func(t *testing.T) {
		t.Parallel()
		var got []Cell[item]
		hs := newHarness(newTable(10).CellSelection(true).OnCell(func(c Cell[item]) tea.Cmd { got = append(got, c); return nil }), 60, 12)
		hs.key("right")
		hs.click(hs.col(firstRow+1, "item-00002"), firstRow+1)
		hs.click(hs.col(firstRow+1, "item-00002"), firstRow+1)
		// The second click is on the cell under the cursor, which still
		// reports it.
		want := []string{"1 KIND alpha", "2 NAME item-00002", "2 NAME item-00002"}
		var have []string
		for _, c := range got {
			have = append(have, fmt.Sprintf("%d %s %s", c.Row.id, c.Title, c.Value))
		}
		if !slices.Equal(have, want) {
			t.Errorf("OnCell got %q, want %q", have, want)
		}
		if c, ok := hs.tbl.SelectedCell(); !ok || c.Col != 2 {
			t.Errorf("selected cell %+v, %v; want column 2", c, ok)
		}
	})
}

func TestMultiSelect(t *testing.T) {
	t.Parallel()

	t.Run("off, space and a right click mark nothing", func(t *testing.T) {
		t.Parallel()
		hs := newHarness(newTable(10), 40, 12)
		hs.key("space")
		hs.rightClick(5, firstRow+2)
		if n := len(hs.tbl.Marked()); n != 0 {
			t.Errorf("%d rows marked", n)
		}
	})

	t.Run("on, space and a right click mark, and turning it off clears them", func(t *testing.T) {
		t.Parallel()
		var events []string
		hs := newHarness(newTable(10).MultiSelect(true).OnMark(func(i item, marked bool) tea.Cmd {
			events = append(events, fmt.Sprintf("%d %v", i.id, marked))
			return nil
		}), 40, 12)
		hs.key("space", "down", "space", "space")
		hs.rightClick(5, firstRow+4)
		if !slices.Equal(events, []string{"1 true", "2 true", "2 false", "5 true"}) {
			t.Errorf("OnMark got %q", events)
		}
		var ids []int
		for _, i := range hs.tbl.Marked() {
			ids = append(ids, i.id)
		}
		if !slices.Equal(ids, []int{1, 5}) {
			t.Errorf("marked %v, want [1 5]", ids)
		}
		if !strings.Contains(hs.out, "●") {
			t.Errorf("no mark drawn:\n%s", hs.out)
		}
		hs.tbl.MultiSelect(false)
		if n := len(hs.tbl.Marked()); n != 0 {
			t.Errorf("%d rows still marked after turning multi-select off", n)
		}
	})

	t.Run("without selection, a right click still marks, and the gutter stays", func(t *testing.T) {
		t.Parallel()
		hs := newHarness(newTable(10).Selection(false).MultiSelect(true), 40, 12)
		hs.key("space")
		hs.rightClick(5, firstRow+1)
		if ids := hs.tbl.Marked(); len(ids) != 1 || ids[0].id != 2 {
			t.Errorf("marked %v, want item 2 only", ids)
		}
	})
}

func TestMouse(t *testing.T) {
	t.Parallel()

	t.Run("off, clicks, the wheel, hovering, and the titles do nothing", func(t *testing.T) {
		t.Parallel()
		var activated int
		hs := newHarness(newTable(100).Mouse(false).MultiSelect(true).OnActivate(func(item) tea.Cmd { activated++; return nil }), 40, 12)
		hs.click(5, firstRow+2)
		hs.mouse(tea.MouseClickMsg{X: 5, Y: firstRow + 2, Button: tea.MouseLeft}, tea.MouseReleaseMsg{X: 5, Y: firstRow + 2, Button: tea.MouseLeft})
		hs.rightClick(5, firstRow+3)
		hs.wheel(tea.MouseWheelDown, 2)
		hs.mouse(tea.MouseMotionMsg{X: 5, Y: firstRow + 4})
		hs.click(hs.col(1, "NAME"), 1)
		if hs.tbl.cursor != 0 || activated != 0 || len(hs.tbl.Marked()) != 0 || hs.tbl.hover != -1 || hs.tbl.sortCol != -1 {
			t.Errorf("cursor %d, activated %d, marked %d, hover %d, sorted by %d; want nothing changed",
				hs.tbl.cursor, activated, len(hs.tbl.Marked()), hs.tbl.hover, hs.tbl.sortCol)
		}
	})

	t.Run("hover follows the pointer, unless turned off", func(t *testing.T) {
		t.Parallel()
		on := newHarness(newTable(10), 40, 12)
		on.mouse(tea.MouseMotionMsg{X: 5, Y: firstRow + 4})
		off := newHarness(newTable(10).Hover(false), 40, 12)
		off.mouse(tea.MouseMotionMsg{X: 5, Y: firstRow + 4})
		if on.tbl.hover != 4 || off.tbl.hover != -1 {
			t.Errorf("hovered row %d with hover on, %d off; want 4 and none", on.tbl.hover, off.tbl.hover)
		}
	})

	t.Run("a double click or enter activates", func(t *testing.T) {
		t.Parallel()
		var got []int
		hs := newHarness(newTable(10).OnActivate(func(i item) tea.Cmd { got = append(got, i.id); return nil }), 40, 12)
		hs.click(5, firstRow+3)
		hs.click(5, firstRow+3)
		hs.key("enter")
		if !slices.Equal(got, []int{4, 4}) {
			t.Errorf("activated %v, want [4 4]", got)
		}
	})
}

func TestSorting(t *testing.T) {
	t.Parallel()

	// Sorting moves the cursor to the first row.
	first := func(hs *harness) int { row, _ := hs.tbl.Selected(); return row.id }

	hs := newHarness(newTable(30), 40, 12)
	hs.click(hs.col(1, "ID"), 1)
	hs.click(hs.col(1, "ID"), 1)
	if got := first(hs); got != 30 || !strings.Contains(hs.out, "ID ▼") {
		t.Errorf("first row %d after two clicks on ID, want 30, sorted descending:\n%s", got, hs.out)
	}

	off := newHarness(newTable(30).Sorting(false), 40, 12)
	off.click(off.col(1, "ID"), 1)
	if got := first(off); got != 1 || strings.Contains(off.out, "▲") {
		t.Errorf("first row %d after a click on ID with sorting off, want 1", got)
	}
	off.tbl.Sort(0, true)
	off.draw()
	if got := first(off); got != 30 {
		t.Errorf("first row %d after Sort, want 30", got)
	}
}

func TestDrawing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		tbl       func(*Model[item]) *Model[item]
		has, hasn []string
	}{
		{
			name: "by default",
			tbl:  func(m *Model[item]) *Model[item] { return m.Title("things") },
			has:  []string{"╭─ things ", "100 rows ─╮", "├", "NAME", "┃", " 1/100 ─╯", "›"},
			hasn: []string{"┬", "●"},
		},
		{
			name: "filtered",
			tbl:  func(m *Model[item]) *Model[item] { return m.Filter("item-0000") },
			has:  []string{"9 of 100 rows", " 1/9 "},
		},
		{
			name: "with nothing matching the filter",
			tbl:  func(m *Model[item]) *Model[item] { return m.Filter("zzz") },
			has:  []string{`no rows match "zzz"`, " 0/0 "},
		},
		{
			name: "with separators",
			tbl:  func(m *Model[item]) *Model[item] { return m.Separators(true) },
			has:  []string{"┬", "┼", "┴", "│ item-00001"},
		},
		{
			name: "without labels",
			tbl:  func(m *Model[item]) *Model[item] { return m.RowCount(false).Position(false) },
			hasn: []string{"rows", "/100"},
		},
		{
			name: "without a header",
			tbl:  func(m *Model[item]) *Model[item] { return m.Header(false).Separators(true) },
			has:  []string{"╭", "100 rows", "┬", "┴"},
			hasn: []string{"NAME", "├", "┼"},
		},
		{
			name: "without a border",
			tbl:  func(m *Model[item]) *Model[item] { return m.Border(false).Title("things") },
			has:  []string{"NAME", "┃", "│"},
			hasn: []string{"╭", "things", "rows", "/100"},
		},
		{
			name: "without a scrollbar",
			tbl:  func(m *Model[item]) *Model[item] { return m.Scrollbar(false) },
			hasn: []string{"┃"},
		},
		{
			name: "without a border or a scrollbar",
			tbl:  func(m *Model[item]) *Model[item] { return m.Border(false).Scrollbar(false) },
			hasn: []string{"┃", "│", "╭"},
		},
		{
			name: "without selection or multi-select, there is no gutter",
			tbl: func(m *Model[item]) *Model[item] {
				return m.Selection(false).Border(false).Header(false).Scrollbar(false)
			},
			has:  []string{"    1  alpha"},
			hasn: []string{"      1  alpha"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hs := newHarness(tt.tbl(newTable(100)), 60, 12)
			for _, s := range tt.has {
				if !strings.Contains(hs.out, s) {
					t.Errorf("missing %q:\n%s", s, hs.out)
				}
			}
			for _, s := range tt.hasn {
				if strings.Contains(hs.out, s) {
					t.Errorf("has %q:\n%s", s, hs.out)
				}
			}
		})
	}
}

func TestFilterKeepsCursorRow(t *testing.T) {
	t.Parallel()

	hs := newHarness(newTable(100), 40, 12)
	hs.key("down", "down", "down", "down", "down") // item 6
	hs.tbl.Filter("alpha")                         // items 1, 4, 7, …: item 6 is gone
	hs.draw()
	if row, ok := hs.tbl.Selected(); !ok || row.id != 1 {
		t.Errorf("selected %v, %v after its row was filtered out, want item 1", row, ok)
	}
	hs.tbl.Filter("")
	hs.key("home", "pgdown", "pgdown", "pgdown", "up", "up", "up")
	if row, ok := hs.tbl.Selected(); !ok || row.id != 19 {
		t.Fatalf("selected %v, %v; want item 19", row, ok)
	}
	_, before := hs.cursorAt()
	hs.tbl.Filter("1") // item 19 is still shown, with rows before and after it
	hs.draw()
	row, ok := hs.tbl.Selected()
	if _, after := hs.cursorAt(); !ok || row.id != 19 || after != before {
		t.Errorf("selected %v, %v, %d down; want item 19, still %d down", row, ok, after, before)
	}
}

func TestHelp(t *testing.T) {
	t.Parallel()

	keys := func(m *Model[item]) (out []string) {
		for _, b := range m.ShortHelp() {
			out = append(out, b.Help().Desc)
		}
		return out
	}
	if got := keys(newTable(1)); !slices.Equal(got, []string{"up", "down"}) {
		t.Errorf("default help %q", got)
	}
	all := newTable(1).CellSelection(true).MultiSelect(true).OnActivate(func(item) tea.Cmd { return nil })
	if got := keys(all); !slices.Equal(got, []string{"up", "down", "cell left", "cell right", "mark", "open"}) {
		t.Errorf("help with every feature %q", got)
	}
}
