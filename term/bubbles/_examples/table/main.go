// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Command table is a table of 100,000 jobs, made with package table, with a
// details pane, a filter, and a dialog that cancels a job.
//
// The scrollbar is drawn over the table's border, and can be dragged. A click
// selects a cell, a double click opens a dialog, a right click marks a row,
// the wheel moves the cursor, and a click on a header sorts by it. Callbacks
// on the table fill in the details pane and the status bar.
//
// Keys: arrows or hjkl move the cursor, pgup and pgdown page, home and end
// jump, space marks, enter opens, / filters, 0 to 9 turn the table's
// features on and off, and q quits.
package main

import (
	"cmp"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/lrstanley/bloom"
	"github.com/lrstanley/bloom/anim"
	"github.com/lrstanley/bloom/flex"
	"github.com/lrstanley/bloom/fx"
	"github.com/lrstanley/bloom/on"
	tint "github.com/lrstanley/bubbletint/v2"
	"github.com/lrstanley/x/term/bubbles/table"
)

// theme is the bubbletint tint the example's colors come from.
var theme = tint.TintCatppuccinMocha

var (
	fg     = theme.Fg
	muted  = theme.BrightBlack
	accent = theme.Blue
	dark   = theme.Bg

	panel       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(muted)
	heading     = lipgloss.NewStyle().Bold(true).Foreground(accent)
	label       = lipgloss.NewStyle().Foreground(muted)
	value       = lipgloss.NewStyle().Foreground(fg)
	status      = lipgloss.NewStyle().Foreground(muted).Padding(0, 1)
	hint        = lipgloss.NewStyle().Foreground(muted)
	key         = lipgloss.NewStyle().Foreground(fg).Bold(true)
	enabled     = lipgloss.NewStyle().Foreground(theme.Green)
	dialog      = panel.BorderForeground(theme.Red).Padding(1, 2).Background(lipgloss.Lighten(theme.Bg, 0.05))
	dialogTitle = lipgloss.NewStyle().Bold(true).Foreground(fg)
	dialogText  = lipgloss.NewStyle().Foreground(muted)
	button      = lipgloss.NewStyle().Foreground(fg).Background(lipgloss.Lighten(theme.Bg, 0.15)).Padding(0, 2)
	active      = button.Foreground(dark).Background(theme.Red).Bold(true)

	statusColors = map[Status]lipgloss.Style{
		Running:   lipgloss.NewStyle().Foreground(theme.Green),
		Pending:   lipgloss.NewStyle().Foreground(theme.Yellow),
		Succeeded: lipgloss.NewStyle().Foreground(theme.Cyan),
		Failed:    lipgloss.NewStyle().Foreground(theme.Red).Bold(true),
		Cancelled: lipgloss.NewStyle().Foreground(muted).Strikethrough(true),
	}
)

const (
	paneDialog = "dialog"
	keyEnter   = "enter"
	keyEsc     = "esc"
)

// option is a feature of the table that a key turns on and off.
type option struct {
	key, name string
	on        bool
	set       func(t *table.Model[*Job], on bool) *table.Model[*Job]
}

// options are the table's features the example turns on and off, all on to
// begin with.
func options() []option {
	return []option{
		{"1", "selection", true, (*table.Model[*Job]).Selection},
		{"2", "cells", true, (*table.Model[*Job]).CellSelection},
		{"3", "multi-select", true, (*table.Model[*Job]).MultiSelect},
		{"4", "mouse", true, (*table.Model[*Job]).Mouse},
		{"5", "sorting", true, (*table.Model[*Job]).Sorting},
		{"6", "header", true, (*table.Model[*Job]).Header},
		{"7", "border", true, (*table.Model[*Job]).Border},
		{"8", "scrollbar", true, (*table.Model[*Job]).Scrollbar},
		{"9", "separators", true, (*table.Model[*Job]).Separators},
		{"0", "stripes", true, (*table.Model[*Job]).Striped},
	}
}

type model struct {
	ui      *bloom.Root
	w, h    int
	jobs    *table.Model[*Job]
	opts    []option
	event   string // what the table's callbacks last reported
	editing bool   // typing a filter
	filter  string
	confirm anim.Presence
	cancel  bool // the dialog's cancel button is chosen, rather than keep
}

func newModel(n int) *model {
	m := &model{
		ui:      bloom.New(bloom.WithColors(theme.Fg, theme.Bg)),
		opts:    options(),
		confirm: anim.NewPresence(anim.Duration(200*time.Millisecond), anim.FPS(120)),
		event:   "click a cell, double click a row, right click to mark, or click a header to sort",
	}
	m.jobs = table.New(jobColumns()...).
		Title("jobs").
		Styles(table.Styles{
			Border:      lipgloss.NewStyle().BorderForeground(muted).Foreground(muted),
			Title:       heading,
			Header:      lipgloss.NewStyle().Bold(true).Foreground(theme.Purple),
			HeaderHover: lipgloss.NewStyle().Bold(true).Foreground(dark).Background(theme.Purple),
			Cell:        lipgloss.NewStyle().Foreground(fg),
			Stripe:      lipgloss.Lighten(theme.Bg, 0.025),
			Hover:       lipgloss.Lighten(theme.Bg, 0.09),
			Cursor:      lipgloss.Darken(accent, 0.62),
			CursorCell:  lipgloss.Darken(accent, 0.35),
			Marked:      lipgloss.Darken(theme.Green, 0.7),
			Gutter:      lipgloss.NewStyle().Bold(true).Foreground(accent),
			Thumb:       lipgloss.NewStyle().Foreground(accent),
			Empty:       lipgloss.NewStyle().Foreground(theme.Yellow).Italic(true),
		}).
		OnSelect(func(j *Job) tea.Cmd {
			m.event = fmt.Sprintf("selected job #%d, %s", j.ID, j.Name)
			return nil
		}).
		OnCell(func(c table.Cell[*Job]) tea.Cmd {
			m.event = fmt.Sprintf("job #%d · %s = %s", c.Row.ID, strings.ToLower(c.Title), c.Value)
			return nil
		}).
		OnMark(func(j *Job, marked bool) tea.Cmd {
			m.event = fmt.Sprintf("marked job #%d", j.ID)
			if !marked {
				m.event = "un" + m.event
			}
			return nil
		}).
		OnActivate(func(*Job) tea.Cmd {
			m.cancel = false
			return m.confirm.Show()
		}).
		Rows(mockJobs(n))
	for _, o := range m.opts {
		o.set(m.jobs, o.on)
	}
	return m
}

func jobColumns() []table.Column[*Job] {
	return []table.Column[*Job]{
		table.NewColumn("ID", func(j *Job) string { return strconv.Itoa(j.ID) }).
			Align(lipgloss.Right).
			Compare(func(a, b *Job) int { return cmp.Compare(a.ID, b.ID) }),
		table.NewColumn("NAME", func(j *Job) string { return j.Name }).
			Grow(2).Min(16).
			Compare(func(a, b *Job) int { return strings.Compare(a.Name, b.Name) }),
		table.NewColumn("STATUS", func(j *Job) string { return string(j.Status) }).
			Min(9).
			Style(func(j *Job, s lipgloss.Style) lipgloss.Style { return s.Inherit(statusColors[j.Status]) }).
			Compare(func(a, b *Job) int { return strings.Compare(string(a.Status), string(b.Status)) }),
		table.NewColumn("QUEUE", func(j *Job) string { return j.Queue }).
			Collapse(4).
			Compare(func(a, b *Job) int { return strings.Compare(a.Queue, b.Queue) }),
		table.NewColumn("OWNER", func(j *Job) string { return j.Owner }).
			Collapse(2).Max(12).
			Compare(func(a, b *Job) int { return strings.Compare(a.Owner, b.Owner) }),
		table.NewColumn("CPU", cpuBar).
			Align(lipgloss.Right).Min(11).
			Style(func(j *Job, s lipgloss.Style) lipgloss.Style {
				switch {
				case j.CPU >= 100:
					return s.Foreground(theme.Red)
				case j.CPU >= 75:
					return s.Foreground(theme.Yellow)
				}
				return s
			}).
			Compare(func(a, b *Job) int { return cmp.Compare(a.CPU, b.CPU) }),
		table.NewColumn("MEMORY", func(j *Job) string { return memory(j.Memory) }).
			Align(lipgloss.Right).Collapse(3).
			Compare(func(a, b *Job) int { return cmp.Compare(a.Memory, b.Memory) }),
		table.NewColumn("AGE", func(j *Job) string { return age(j.Age) }).
			Align(lipgloss.Right).
			Compare(func(a, b *Job) int { return cmp.Compare(a.Age, b.Age) }),
		table.NewColumn("NODE", func(j *Job) string { return j.Node }).
			Collapse(1).
			Compare(func(a, b *Job) int { return strings.Compare(a.Node, b.Node) }),
		table.NewColumn("IMAGE", func(j *Job) string { return j.Image }).
			Grow(3).Min(10).Collapse(5).
			Style(func(_ *Job, s lipgloss.Style) lipgloss.Style { return s.Foreground(muted) }),
	}
}

// cpuBar is a job's CPU use as a small bar and a percentage.
func cpuBar(j *Job) string {
	if j.Status != Running {
		return "-"
	}
	const cells = 4
	n := min(int(j.CPU/100*cells+0.5), cells)
	return strings.Repeat("▰", n) + strings.Repeat("▱", cells-n) + fmt.Sprintf(" %3.0f%%", j.CPU)
}

func (m *model) Init() tea.Cmd { return m.ui.Init() }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmds := []tea.Cmd{m.ui.Update(msg)}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tea.KeyPressMsg:
		cmds = append(cmds, m.key(msg))
	}
	cmds = append(cmds, anim.Update(msg, &m.confirm))
	return m, tea.Batch(cmds...)
}

func (m *model) key(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	switch {
	case m.confirm.Shown():
		switch k {
		case "left", "right", "tab", "h", "l":
			m.cancel = !m.cancel
		case keyEnter:
			return m.closeDialog(m.cancel)
		case "y":
			return m.closeDialog(true)
		case keyEsc, "n", "q":
			return m.closeDialog(false)
		}
		return nil
	case m.editing:
		switch k {
		case keyEsc:
			m.editing, m.filter = false, ""
		case keyEnter:
			m.editing = false
		case "backspace":
			m.filter = m.filter[:max(len(m.filter)-1, 0)]
		case "up", "down", "pgup", "pgdown":
			return m.jobs.Update(msg, m.ui.Frame())
		default:
			if msg.Text != "" {
				m.filter += msg.Text
			}
		}
		m.jobs.Filter(m.filter)
		return nil
	}
	switch k {
	case "q", "ctrl+c":
		return tea.Quit
	case "/":
		m.editing = true
		return nil
	case keyEsc:
		m.filter = ""
		m.jobs.Filter("")
		return nil
	}
	for i := range m.opts {
		if o := &m.opts[i]; o.key == k {
			o.on = !o.on
			o.set(m.jobs, o.on)
			m.event = fmt.Sprintf("%s turned %s", o.name, map[bool]string{true: "on", false: "off"}[o.on])
			return nil
		}
	}
	return m.jobs.Update(msg, m.ui.Frame())
}

func (m *model) closeDialog(cancel bool) tea.Cmd {
	if j, ok := m.jobs.Selected(); ok && cancel {
		j.Status, j.CPU = Cancelled, 0
		m.jobs.Refresh()
		m.event = fmt.Sprintf("cancelled job #%d, %s", j.ID, j.Name)
	}
	return m.confirm.Hide()
}

func (m *model) View() tea.View {
	v := m.ui.View(m.w, m.h, m.layout())
	v.AltScreen = true
	v.BackgroundColor = theme.Bg
	// All motion, not only while a button is held, for hovering rows.
	v.MouseMode = tea.MouseModeAllMotion
	return v
}

func (m *model) layout() bloom.Child {
	hints := bloom.Text(keys("↑↓", "move", "←→", "cell", "space", "mark", "enter", "open", "/", "filter", "0-9", "options", "q", "quit")).Truncate()
	top := bloom.Row(
		bloom.Text("◆ bloom table").Style(heading),
		bloom.Fill(hints).Align(bloom.End),
	).Gap(2)

	rows := (len(m.opts) + 1) / 2
	base := bloom.Column(
		bloom.Fixed(1, top),
		bloom.Fill(bloom.Row(
			bloom.Fill(m.jobs).Min(min(m.jobs.MinWidth(), m.w)),
			bloom.Fixed(38, bloom.Column(
				bloom.Fill(m.details()),
				bloom.Fixed(rows+2, m.options()),
			)).Collapsible(1),
		)),
		bloom.Fixed(1, m.statusLine()),
	)
	return bloom.Overlay(base, m.confirmLayer())
}

// keys formats pairs of keys and what they do.
func keys(pairs ...string) string {
	parts := make([]string, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, key.Render(pairs[i])+" "+hint.Render(pairs[i+1]))
	}
	return strings.Join(parts, hint.Render("  ·  "))
}

func (m *model) statusLine() bloom.Child {
	switch {
	case m.editing:
		return bloom.Text(key.Render("/ ") + value.Render(m.filter+"▏") + hint.Render("   enter keeps · esc clears")).Style(status).Truncate()
	case m.filter != "":
		return bloom.Text(key.Render("/ ") + value.Render(m.filter) + hint.Render("  ·  "+m.event)).Style(status).Truncate()
	}
	return bloom.Text(m.event).Style(status).Truncate()
}

// options is the pane listing the table's features, in two columns, with
// the key that turns each on and off.
func (m *model) options() bloom.Child {
	item := func(o option) bloom.Child {
		mark, st := "○", label
		if o.on {
			mark, st = "●", enabled
		}
		return bloom.Text(key.Render(o.key) + " " + st.Render(mark+" "+o.name)).Truncate()
	}
	rows := (len(m.opts) + 1) / 2
	left, right := make([]bloom.Child, 0, rows), make([]bloom.Child, 0, rows)
	for i, o := range m.opts {
		if i < rows {
			left = append(left, item(o))
		} else {
			right = append(right, item(o))
		}
	}
	return bloom.Box(panel.Padding(0, 1), bloom.Row(
		bloom.Fill(bloom.Column(left...)),
		bloom.Fill(bloom.Column(right...)),
	)).Edge(bloom.Top, bloom.Start, bloom.Text(" options ").Style(heading))
}

// details is the pane showing the selected job in full, which the table
// cuts off.
func (m *model) details() bloom.Child {
	j, ok := m.jobs.Selected()
	if !ok {
		return bloom.Box(panel, bloom.Centered(bloom.Text("nothing selected").Style(label)))
	}
	field := func(name, v string, st lipgloss.Style) bloom.Child {
		return bloom.Column(bloom.Text(name).Style(label), bloom.Text(v).Style(st))
	}
	marked := len(m.jobs.Marked())
	rows := []bloom.Child{
		bloom.Text(j.Name).Style(heading),
		field("status", string(j.Status), statusColors[j.Status]),
		bloom.Row(
			bloom.Fill(field("owner", j.Owner, value)),
			bloom.Fill(field("queue", j.Queue, value)),
		),
		bloom.Row(
			bloom.Fill(field("cpu", cpuBar(j), value)),
			bloom.Fill(field("memory", memory(j.Memory), value)),
		),
		field("node", j.Node, value),
		field("image", j.Image, value),
	}
	box := bloom.Box(panel.Padding(0, 1), bloom.Column(rows...).Gap(1)).
		Edge(bloom.Top, bloom.Start, bloom.Text(fmt.Sprintf(" job #%d ", j.ID)).Style(heading))
	if marked > 0 {
		box = box.Edge(bloom.Bottom, bloom.End, bloom.Text(fmt.Sprintf(" ● %d marked ", marked)).Style(enabled))
	}
	return box
}

func (m *model) confirmLayer() bloom.LayerNode {
	j, ok := m.jobs.Selected()
	if !ok {
		return bloom.Layer(bloom.Text("")).Presence(anim.Presence{})
	}
	cancel, keep := button, active
	if m.cancel {
		cancel, keep = active, button
	}
	body := bloom.Column(
		bloom.Text(fmt.Sprintf("Cancel job #%d?", j.ID)).Style(dialogTitle),
		bloom.Text(j.Name+" is "+string(j.Status)+" on "+j.Node+".").Style(dialogText),
		bloom.Fixed(1, nil),
		bloom.Row(
			bloom.Text("cancel job").Style(cancel).On(on.Click(func() tea.Cmd { return m.closeDialog(true) })),
			bloom.Text("keep").Style(keep).On(on.Click(func() tea.Cmd { return m.closeDialog(false) })),
		).Gap(2).Justify(flex.End),
	)
	return bloom.Layer(bloom.Box(dialog, body)).
		ID(paneDialog).
		Modal().
		On(on.ClickOutside(func() tea.Cmd { return m.closeDialog(false) })).
		Backdrop(fx.Dim{Amount: 0.5}, fx.Desaturate{Amount: 0.6}).
		Shadow(2, 1, fx.Dim{Amount: 0.6}).
		Presence(m.confirm, fx.Slide{From: fx.Bottom}, fx.Fade{})
}

func main() {
	p := tea.NewProgram(newModel(100_000), tea.WithFilter(bloom.ThrottleMouse(15*time.Millisecond)))
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
