// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"fmt"
	"math"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/lrstanley/x/term/bloom"
	"github.com/lrstanley/x/term/bloom/anim"
	"github.com/lrstanley/x/term/bloom/flex"
	"github.com/lrstanley/x/term/bloom/fx"
)

var (
	accent  = lipgloss.Color("#7aa2f7")
	muted   = lipgloss.Color("#565f89")
	good    = lipgloss.Color("#9ece6a")
	warn    = lipgloss.Color("#e0af68")
	bg      = lipgloss.Color("#1a1b26")
	panelBg = lipgloss.Color("#24283b")

	panel    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(muted).Background(bg)
	focused  = panel.BorderForeground(accent)
	header   = lipgloss.NewStyle().Foreground(bg).Background(accent).Bold(true).Padding(0, 1)
	status   = lipgloss.NewStyle().Foreground(lipgloss.Color("#a9b1d6")).Background(panelBg).Padding(0, 1)
	item     = lipgloss.NewStyle().Foreground(lipgloss.Color("#a9b1d6")).Background(bg).Padding(0, 1)
	selected = item.Foreground(bg).Background(accent).Bold(true)
	dialog   = lipgloss.NewStyle().Border(lipgloss.ThickBorder()).BorderForeground(warn).Background(panelBg).Padding(1, 2)
	button   = lipgloss.NewStyle().Foreground(lipgloss.Color("#c0caf5")).Background(muted).Padding(0, 2)
	active   = button.Foreground(bg).Background(warn).Bold(true)
	toast    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(good).Background(panelBg).Padding(0, 1)
)

var services = []string{"api", "auth", "billing", "cache", "gateway", "ingest", "mailer", "search", "storage", "worker"}

// The IDs nodes are given, for mouse events and focus.
type (
	serviceID int
	buttonID  bool // true for "restart", false for "cancel"
	paneID    string
)

const (
	paneSidebar paneID = "sidebar"
	paneMain    paneID = "main"
	paneTable   paneID = "table"
	paneLogs    paneID = "logs"
	paneDialog  paneID = "dialog"
)

// toastEffects are what the toast shows for keys 1-6.
var toastEffects = []struct {
	name string
	fx   []fx.Effect
}{
	{"fade", []fx.Effect{fx.Fade{}}},
	{"slide", []fx.Effect{fx.Slide{From: fx.Right}}},
	{"wipe", []fx.Effect{fx.Wipe{From: fx.Left}}},
	{"dissolve", []fx.Effect{fx.Dissolve{Seed: 7}}},
	{"typewriter", []fx.Effect{fx.Typewriter{}}},
	{"scramble", []fx.Effect{fx.Scramble{Seed: 3}, fx.Fade{}}},
}

type tickMsg time.Time

type model struct {
	ui       *bloom.Root
	w, h     int
	focus    bloom.Focus
	selected int
	restart  bool // the dialog's selected button
	frame    int
	logs     []string
	drawn    time.Duration // time the last frame took to lay out and draw

	sidebar bool          // whether the sidebar is open
	confirm anim.Presence // the restart dialog
	toast   anim.Presence
	effect  int // the toast's effect
}

func newModel() *model {
	m := &model{
		ui:      bloom.New(bloom.WithColors(lipgloss.Color("#c0caf5"), bg)),
		sidebar: true,
		confirm: anim.NewPresence(anim.Duration(220*time.Millisecond), anim.FPS(120)),
		toast:   anim.NewPresence(anim.Duration(500*time.Millisecond), anim.Ease(anim.Linear), anim.FPS(120)),
	}
	m.focus.Set(paneTable)
	return m
}

func tick() tea.Cmd {
	return tea.Tick(time.Second/30, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *model) Init() tea.Cmd { return tea.Batch(m.ui.Init(), tick()) }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmds := []tea.Cmd{m.ui.Update(msg)}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tickMsg:
		m.frame++
		if m.frame%6 == 0 {
			m.log(fmt.Sprintf("%s  %-8s %3dms  200 OK", time.Time(msg).Format("15:04:05"), services[m.frame%len(services)], 8+m.frame%91))
		}
		cmds = append(cmds, tick())
	case tea.KeyPressMsg:
		if m.confirm.Shown() || !m.focus.Update(msg, m.ui.Frame()) {
			cmds = append(cmds, m.key(msg.String()))
		}
	case bloom.MouseMsg:
		cmds = append(cmds, m.mouse(msg))
	}
	cmds = append(cmds, anim.Update(msg, &m.confirm, &m.toast))
	return m, tea.Batch(cmds...)
}

func (m *model) key(k string) tea.Cmd {
	if m.confirm.Shown() {
		switch k {
		case "left", "right", "tab":
			m.restart = !m.restart
		case "enter":
			return m.closeDialog(m.restart)
		case "esc":
			return m.closeDialog(false)
		}
		return nil
	}
	switch k {
	case "q", "ctrl+c":
		return tea.Quit
	case "up":
		m.selected = (m.selected + len(services) - 1) % len(services)
	case "down":
		m.selected = (m.selected + 1) % len(services)
	case "enter":
		m.restart = true
		m.focus.Push(paneDialog)
		return m.confirm.Show()
	case "ctrl+b":
		m.sidebar = !m.sidebar
	case "1", "2", "3", "4", "5", "6":
		m.effect = int(k[0] - '1')
		return m.toast.ShowFor(1500 * time.Millisecond)
	}
	return nil
}

func (m *model) mouse(msg bloom.MouseMsg) tea.Cmd {
	if _, ok := msg.Msg.(tea.MouseClickMsg); !ok {
		return nil
	}
	if msg.Outside {
		return m.closeDialog(false)
	}
	if b, ok := msg.Target[buttonID](); ok {
		return m.closeDialog(bool(b))
	}
	if s, ok := msg.Target[serviceID](); ok {
		m.selected = int(s)
	}
	m.focus.Update(msg, m.ui.Frame())
	return nil
}

func (m *model) closeDialog(restart bool) tea.Cmd {
	if restart {
		m.log(fmt.Sprintf("restarting %s…", services[m.selected]))
	}
	m.focus.Pop()
	return m.confirm.Hide()
}

func (m *model) log(line string) {
	m.logs = append(m.logs, line)
	if len(m.logs) > 200 {
		m.logs = m.logs[len(m.logs)-200:]
	}
}

func (m *model) View() tea.View {
	start := time.Now()
	v := m.ui.View(m.w, m.h, m.layout())
	m.drawn = time.Since(start)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.BackgroundColor = bg
	return v
}

func (m *model) panel(id paneID) lipgloss.Style {
	if m.focus.Is(id) {
		return focused
	}
	return panel
}

func (m *model) layout() bloom.Child {
	items := make([]bloom.Child, 0, len(services)+1)
	for i, name := range services {
		st := item
		if i == m.selected {
			st = selected
		}
		items = append(items, bloom.Text("● "+name).Style(st).ID(serviceID(i)).Truncate())
	}
	items = append(items, bloom.Space())

	cards := make([]bloom.Child, 0, 4)
	for i, label := range []string{"requests/s", "p99 latency", "errors", "saturation"} {
		cards = append(cards, bloom.Fill(bloom.Box(panel, bloom.Column(
			bloom.Text(label).Style(lipgloss.NewStyle().Foreground(muted).Background(bg)),
			bloom.Text(sparkline(m.frame, i, 40)).Style(lipgloss.NewStyle().Foreground(good).Background(bg)),
		))))
	}

	// The sidebar and the main column move together as the sidebar springs
	// open and shut.
	spring := []anim.Option{anim.Spring(14, 0.8), anim.FPS(120)}
	sidebarWidth := 0
	if m.sidebar {
		sidebarWidth = 22
	}
	base := bloom.Column(
		bloom.Text(fmt.Sprintf("◆ fleet  ·  %d services  ·  %s", len(services), services[m.selected])).Style(header),
		bloom.Fill(bloom.Row(
			bloom.Fixed(sidebarWidth, bloom.Box(m.panel(paneSidebar), bloom.Column(items...))).
				ID(paneSidebar).Focusable().Collapsible(1).Animate(spring...),
			bloom.Fill(bloom.Column(
				bloom.Row(cards...).Gap(1),
				bloom.Fill(bloom.Row(
					bloom.Fill(bloom.Box(m.panel(paneTable), bloom.Scroll(table(m.frame), m.selected))).
						ID(paneTable).Focusable().Min(30),
					bloom.Fixed(46, bloom.Box(m.panel(paneLogs), m.logView())).ID(paneLogs).Focusable().Collapsible(2),
				).Gap(1)),
			)).ID(paneMain).Min(40).Animate(spring...),
		).Gap(1)),
		// The key hints give way (and are cut off) before the timing.
		bloom.Row(
			bloom.Fill(bloom.Text("↑↓ select · enter restart · tab focus · ctrl+b sidebar · 1-6 effects · q quit").Style(status).Truncate()),
			bloom.Text(fmt.Sprintf("frame %.2fms · %.1f%% of 120fps", ms(m.drawn), 100*ms(m.drawn)/8.33)).Style(status),
		),
	)
	return bloom.Overlay(base, m.toastLayer(), m.confirmLayer())
}

func (m *model) toastLayer() bloom.LayerNode {
	e := toastEffects[m.effect]
	return bloom.Layer(bloom.Box(toast, bloom.Text(fmt.Sprintf("✓ %s\nan effect on a layer's cells", e.name)))).
		Anchor(bloom.End, bloom.End).Margin(2).
		Presence(m.toast, e.fx...)
}

func (m *model) confirmLayer() bloom.LayerNode {
	restart, cancel := button, active
	if m.restart {
		restart, cancel = active, button
	}
	body := bloom.Column(
		bloom.Text(fmt.Sprintf("Restart %s?", services[m.selected])).Style(lipgloss.NewStyle().Bold(true).Background(panelBg)),
		bloom.Text("In-flight requests will be drained first.").Style(lipgloss.NewStyle().Foreground(muted).Background(panelBg)),
		bloom.Fixed(1, nil),
		bloom.Row(
			bloom.Text("restart").Style(restart).ID(buttonID(true)),
			bloom.Text("cancel").Style(cancel).ID(buttonID(false)),
		).Gap(2).Justify(flex.End),
	)
	return bloom.Layer(bloom.Box(dialog, body)).
		ID(paneDialog).Modal().
		Backdrop(fx.Dim{Amount: 0.55}, fx.Desaturate{Amount: 0.7}).
		Shadow(2, 1, fx.Dim{Amount: 0.6}).
		Presence(m.confirm, fx.Slide{From: fx.Bottom}, fx.Fade{})
}

func (m *model) logView() bloom.Child {
	return bloom.Text(strings.Join(m.logs[max(len(m.logs)-60, 0):], "\n")).
		Style(lipgloss.NewStyle().Foreground(lipgloss.Color("#9aa5ce")).Background(bg).AlignVertical(lipgloss.Bottom))
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

var bars = []rune("▁▂▃▄▅▆▇█")

func sparkline(frame, series, n int) string {
	var b strings.Builder
	for i := range n {
		v := math.Sin(float64(frame+i)/(4+float64(series))+float64(series)) + math.Sin(float64(frame+i*3)/7)
		b.WriteRune(bars[int((v+2)/4*float64(len(bars)-1))])
	}
	return b.String()
}

func table(frame int) bloom.Child {
	var b strings.Builder
	for i := range 120 {
		svc := services[i%len(services)]
		fmt.Fprintf(&b, "\x1b[38;2;86;95;137m%04d\x1b[m  %-8s  \x1b[38;2;158;206;106m%5d rps\x1b[m  %3dms  %s\n",
			i, svc, 900+(i*37+frame)%400, 5+(i*13+frame)%90, strings.Repeat("▪", 1+(i+frame/8)%12))
	}
	return bloom.Text(strings.TrimSuffix(b.String(), "\n"))
}
