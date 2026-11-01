// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Command responsive is a reader that adapts to the terminal's size. The
// details panel collapses first and the article list second as the terminal
// narrows; the toolbar is a Layouter that rebuilds itself for the space it
// gets; the tags wrap onto as many lines as they need; the status bar is a
// Measurer; and paging uses
// bloom.Solve in Update to know how tall the reader is.
//
// Keys: up/down pick an article, pgup/pgdown or j/k scroll, q quits. The
// mouse wheel scrolls the reader and a click picks an article.
package main

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/lrstanley/x/term/bloom"
)

var (
	accent = lipgloss.Color("#7dcfff")
	muted  = lipgloss.Color("#565f89")

	panel    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(muted)
	bar      = lipgloss.NewStyle().Foreground(lipgloss.Color("#c0caf5")).Background(lipgloss.Color("#24283b"))
	title    = bar.Bold(true).Foreground(accent).Padding(0, 1)
	action   = bar.Foreground(lipgloss.Color("#9aa5ce")).Padding(0, 1)
	item     = lipgloss.NewStyle().Foreground(lipgloss.Color("#a9b1d6")).Padding(0, 1)
	selected = item.Foreground(lipgloss.Color("#1a1b26")).Background(accent).Bold(true)
	prose    = lipgloss.NewStyle().Foreground(lipgloss.Color("#c0caf5")).Padding(0, 1)
	chip     = lipgloss.NewStyle().Foreground(lipgloss.Color("#1a1b26")).Background(lipgloss.Color("#bb9af7")).Padding(0, 1)
)

type (
	paneID    string
	articleID int
)

const (
	paneNav     paneID = "nav"
	paneReader  paneID = "reader"
	paneDetails paneID = "details"
	paneContent paneID = "content" // the scrolled text, to know its height
)

type article struct {
	title string
	tags  []string
	body  string
}

var articles = []article{
	{"Constraints go down", []string{"layout", "design", "flutter", "gio"}, essay("constraints", 18)},
	{"Sizes come up", []string{"sizing", "measure"}, essay("measurement", 26)},
	{"Parents place", []string{"arrange", "flex", "slots", "collapse", "ownership"}, essay("placement", 22)},
	{"Cells, not pixels", []string{"terminal", "unicode", "compositor"}, essay("cells", 30)},
}

func essay(topic string, paragraphs int) string {
	var b strings.Builder
	for i := range paragraphs {
		fmt.Fprintf(&b, "§%d. A paragraph about %s, long enough to wrap at most widths so that the reader has to reflow it as the terminal narrows and widens.\n\n", i+1, topic)
	}
	return strings.TrimSpace(b.String())
}

// toolbar is a [bloom.Layouter]: it shows its actions with labels when there
// is room, and only their icons when there is not.
type toolbar struct{ title string }

func (t toolbar) View() string { return t.title }

func (t toolbar) Layout(ctx bloom.Context) bloom.Child {
	actions := []string{"⟳ refresh", "✎ edit", "⇪ share", "⚙ settings"}
	children := []bloom.Child{bloom.Text("◆ " + t.title).Style(title), bloom.Fill(bloom.Text("").Style(bar))}
	for _, a := range actions {
		if ctx.Width < 70 {
			a, _, _ = strings.Cut(a, " ")
		}
		children = append(children, bloom.Text(a).Style(action))
	}
	return bloom.Row(children...)
}

// chips returns tags as chips that wrap onto lines to fit.
func chips(tags []string) bloom.Child {
	cs := make([]bloom.Child, len(tags))
	for i, tag := range tags {
		cs[i] = bloom.Text("#" + tag).Style(chip)
	}
	return bloom.Wrap(cs...).Gap(1, 1)
}

// statusbar is a [bloom.Measurer]: one row, of any width, as the column
// it is in stretches it across.
type statusbar struct{ text string }

func (s statusbar) View() string { return bar.Render(" " + s.text) }

func (statusbar) Measure(bloom.Size) bloom.Size { return bloom.Size{H: 1} }

type model struct {
	ui       *bloom.Root
	w, h     int
	selected int
	offset   int // the reader's scroll position, in rows
}

func newModel() *model { return &model{ui: bloom.New()} }

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "up":
			m.pick(m.selected - 1)
		case "down":
			m.pick(m.selected + 1)
		case "j":
			m.offset++
		case "k":
			m.offset--
		case "pgdown", "space":
			m.offset += m.geometry().page
		case "pgup":
			m.offset -= m.geometry().page
		}
	case bloom.MouseMsg:
		switch ev := msg.Msg.(type) {
		case tea.MouseWheelMsg:
			if p, ok := msg.Target[paneID](); ok && (p == paneReader || p == paneContent) {
				if ev.Button == tea.MouseWheelDown {
					m.offset += 3
				} else {
					m.offset -= 3
				}
			}
		case tea.MouseClickMsg:
			if a, ok := msg.Target[articleID](); ok {
				m.pick(int(a))
			}
		}
	}
	m.offset = min(max(m.offset, 0), m.geometry().maxOffset)
	return m, nil
}

func (m *model) pick(i int) {
	m.selected = min(max(i, 0), len(articles)-1)
	m.offset = 0
}

// geometry is what Update needs to know about the layout: how many rows the
// reader shows, how far it can scroll, and which panels fit.
type geometry struct {
	page, maxOffset int
	panes           []string
}

func (m *model) geometry() geometry {
	// The status bar reports the geometry, so it is solved with an empty one,
	// which as a Measurer is the same size.
	frame := bloom.Solve(m.layout(statusbar{}), m.w, m.h)
	var g geometry
	if r, ok := frame.Rect(paneReader); ok {
		g.page = max(r.Dy()-panel.GetVerticalFrameSize(), 1)
	}
	if r, ok := frame.Rect(paneContent); ok {
		g.maxOffset = max(r.Dy()-g.page, 0)
	}
	for _, p := range []paneID{paneNav, paneReader, paneDetails} {
		if _, ok := frame.Rect(p); ok {
			g.panes = append(g.panes, string(p))
		}
	}
	return g
}

func (m *model) layout(status statusbar) bloom.Child {
	art := articles[m.selected]

	items := make([]bloom.Child, 0, len(articles)+1)
	for i, a := range articles {
		st := item
		if i == m.selected {
			st = selected
		}
		items = append(items, bloom.Text(a.title).Style(st).ID(articleID(i)))
	}
	items = append(items, bloom.Space())

	return bloom.Column(
		toolbar{title: "reader"},
		bloom.Fill(bloom.Row(
			bloom.Fixed(24, bloom.Box(panel, bloom.Column(items...))).ID(paneNav).Collapsible(2),
			bloom.Fill(bloom.Box(panel, bloom.Scroll(bloom.Text(art.body).Style(prose).ID(paneContent), m.offset))).
				ID(paneReader).Min(40),
			bloom.Percent(28, bloom.Box(panel.Padding(0, 1), bloom.Column(
				bloom.Text(art.title).Style(lipgloss.NewStyle().Bold(true)),
				bloom.Fixed(1, nil),
				chips(art.tags),
				bloom.Space(),
				bloom.Text(fmt.Sprintf("%d paragraphs", strings.Count(art.body, "§"))).Style(lipgloss.NewStyle().Foreground(muted)),
			))).ID(paneDetails).Min(28).Collapsible(1),
		).Gap(1)),
		status,
	)
}

func (m *model) status() statusbar {
	g := m.geometry()
	return statusbar{text: fmt.Sprintf("%d×%d · showing %s · row %d of %d · page %d",
		m.w, m.h, strings.Join(g.panes, ", "), m.offset+1, g.maxOffset+1, g.page)}
}

func (m *model) View() tea.View {
	v := m.ui.View(m.w, m.h, m.layout(m.status()))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func main() {
	if _, err := tea.NewProgram(newModel()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
