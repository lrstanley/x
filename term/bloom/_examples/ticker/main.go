// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Command ticker is a market ticker driven by looping animation sequences: a
// headline bar scrolling across the window, a watchlist whose long names are
// cut off with "…" except the selected one, which scrolls, a banner that
// slides in, holds, and slides out again, and an indicator pulsing while the
// feed is live. Each is a looping [anim.Sequence] or an [anim.Presence], with
// no timers of its own.
//
// Keys: up/down select, b shows a banner, space pauses the motion, q quits.
// Items can be clicked.
package main

import (
	"fmt"
	"image/color"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/lrstanley/x/term/bloom"
	"github.com/lrstanley/x/term/bloom/anim"
	"github.com/lrstanley/x/term/bloom/fx"
)

var (
	fg      = lipgloss.Color("#c0caf5")
	muted   = lipgloss.Color("#565f89")
	accent  = lipgloss.Color("#7aa2f7")
	good    = lipgloss.Color("#9ece6a")
	bad     = lipgloss.Color("#f7768e")
	warn    = lipgloss.Color("#e0af68")
	bg      = lipgloss.Color("#1a1b26")
	panelBg = lipgloss.Color("#24283b")

	header   = lipgloss.NewStyle().Foreground(bg).Background(accent).Bold(true).Padding(0, 1)
	bar      = lipgloss.NewStyle().Foreground(fg).Background(panelBg).Padding(0, 1)
	badge    = lipgloss.NewStyle().Foreground(bg).Background(warn).Bold(true).Padding(0, 1)
	panel    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(muted).Background(bg)
	item     = lipgloss.NewStyle().Foreground(fg).Background(bg).Padding(0, 1)
	selected = item.Foreground(bg).Background(accent).Bold(true)
	status   = lipgloss.NewStyle().Foreground(muted).Background(bg).Padding(0, 1)
	banner   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(warn).Background(panelBg).Padding(0, 1)
)

// headlines scroll along the headline bar, one after the other.
var headlines = []string{
	"Central bank holds rates steady, citing stubborn services inflation",
	"Chipmaker beats estimates as demand for datacenter accelerators doubles",
	"Oil slips for a third session on weaker than expected import figures",
	"Regulators approve the long-awaited merger of two regional lenders",
}

type stock struct {
	name   string
	price  float64
	change float64 // percent
}

var stocks = []stock{
	{"ACME Corp", 128.40, 1.2},
	{"Globex International Holdings Group", 412.05, -0.8},
	{"Initech", 33.17, 0.4},
	{"Umbrella Pharmaceuticals and Biotechnology Ltd.", 87.92, -2.6},
	{"Hooli", 215.60, 3.1},
	{"Stark Industries Advanced Research Division", 530.00, 0.0},
	{"Wayne Enterprises", 301.75, 1.9},
	{"Soylent Nutritional Sciences, Incorporated", 19.48, -1.1},
}

// itemID is the ID rows are given, for mouse events.
type itemID int

type model struct {
	ui       *bloom.Root
	w, h     int
	selected int
	paused   bool
	alerts   int // banners shown

	headline anim.Sequence // 0 to 1, once around the headline bar, over and over
	name     anim.Sequence // 0 to 1, once around the selected name
	pulse    anim.Sequence // 0 to 1 and back, for the live indicator
	alert    anim.Presence // the banner, shown for a while
}

func newModel() *model {
	linear := []anim.Option{anim.Ease(anim.Linear), anim.FPS(60)}
	return &model{
		ui:       bloom.New(bloom.WithColors(fg, bg)),
		headline: anim.NewLoop(24*time.Second, anim.FPS(60)),
		// The name waits a moment, truncated, before it starts to scroll.
		name: anim.NewSequence(0,
			anim.Jump(0), anim.Hold(900*time.Millisecond), anim.To(1, anim.Duration(7*time.Second)),
		).With(linear...),
		pulse: anim.NewSequence(0,
			anim.To(1, anim.Duration(900*time.Millisecond)), anim.To(0, anim.Duration(900*time.Millisecond)),
		).With(anim.Ease(anim.InOutQuad), anim.FPS(30)),
		alert: anim.NewPresence(anim.Duration(400*time.Millisecond), anim.Ease(anim.OutCubic), anim.FPS(60)),
	}
}

func main() {
	if _, err := tea.NewProgram(newModel(), tea.WithFPS(120)).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.ui.Init(), m.play())
}

// play starts the motion that runs for as long as the feed is live.
func (m *model) play() tea.Cmd {
	return tea.Batch(m.headline.Loop(), m.name.Loop(), m.pulse.Loop())
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmds := []tea.Cmd{m.ui.Update(msg)}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tea.KeyPressMsg:
		cmds = append(cmds, m.key(msg.String()))
	case bloom.MouseMsg:
		if _, click := msg.Msg.(tea.MouseClickMsg); click {
			if id, ok := msg.Target[itemID](); ok {
				cmds = append(cmds, m.selectItem(int(id)))
			}
		}
	}
	cmds = append(cmds, anim.Update(msg, &m.headline, &m.name, &m.pulse, &m.alert))
	return m, tea.Batch(cmds...)
}

func (m *model) key(k string) tea.Cmd {
	switch k {
	case "q", "ctrl+c":
		return tea.Quit
	case "up":
		return m.selectItem((m.selected + len(stocks) - 1) % len(stocks))
	case "down":
		return m.selectItem((m.selected + 1) % len(stocks))
	case "b":
		m.alerts++
		return m.alert.ShowFor(2400 * time.Millisecond)
	case "space", " ":
		m.paused = !m.paused
		if m.paused {
			m.headline.Stop()
			m.name.Stop()
			m.pulse.Stop()
			return nil
		}
		return m.play()
	}
	return nil
}

// selectItem selects an item, whose name starts scrolling from the start.
func (m *model) selectItem(i int) tea.Cmd {
	m.selected = i
	if m.paused {
		return nil
	}
	return m.name.Loop()
}

// phase returns where a looping sequence is, or 0 while the motion is
// paused, which shows names cut off and the feed still.
func (m *model) phase(s *anim.Sequence) float64 {
	if m.paused {
		return 0
	}
	return s.Float()
}

func (m *model) View() tea.View {
	v := m.ui.View(m.w, m.h, m.layout())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.BackgroundColor = bg
	return v
}

func (m *model) layout() bloom.Child {
	rows := make([]bloom.Child, 0, len(stocks)+1)
	for i, s := range stocks {
		st, phase := item, 0.0
		if i == m.selected {
			st, phase = selected, m.phase(&m.name)
		}
		tint := good
		if s.change < 0 {
			tint = bad
		}
		rows = append(rows, bloom.Row(
			bloom.Fill(bloom.Marquee(s.name, phase).Style(st)).ID(itemID(i)),
			bloom.Fixed(11, bloom.Text(fmt.Sprintf("%.2f", s.price)).Style(st.Align(lipgloss.Right))).ID(itemID(i)),
			bloom.Fixed(9, bloom.Text(fmt.Sprintf("%+.1f%%", s.change)).Style(st.Foreground(tint).Align(lipgloss.Right))).ID(itemID(i)),
		))
	}
	rows = append(rows, bloom.Space())

	pulse := blend(muted, good, m.phase(&m.pulse))
	base := bloom.Column(
		bloom.Row(
			bloom.Fill(bloom.Text("◆ markets").Style(header)),
			bloom.Text("•").Style(header.Foreground(pulse).Padding(0)),
			bloom.Text(" live").Style(header.PaddingLeft(0)),
		),
		bloom.Row(
			bloom.Text("NEWS").Style(badge),
			bloom.Fill(bloom.Marquee(strings.Join(headlines, "  ◆  "), m.phase(&m.headline)).Gap("  ◆  ").Style(bar)),
		),
		bloom.Fill(bloom.Box(panel, bloom.Column(rows...))),
		bloom.Text("↑↓ select · b banner · space pause · q quit").Style(status).Truncate(),
	)

	return bloom.Overlay(base, bloom.Layer(bloom.Box(banner,
		bloom.Text(fmt.Sprintf("◆ Alert %d: %s", m.alerts, stocks[m.selected].name)).Style(lipgloss.NewStyle().Background(panelBg)))).
		Anchor(bloom.End, bloom.Start).Offset(0, 3).Margin(1).
		Presence(m.alert, fx.Slide{From: fx.Right}, fx.Fade{}))
}

// blend returns the color t of the way from a to b.
func blend(a, b color.Color, t float64) color.Color {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	mix := func(x, y uint32) uint8 { return uint8((float64(x) + (float64(y)-float64(x))*t) / 257) }
	return color.RGBA{R: mix(ar, br), G: mix(ag, bg), B: mix(ab, bb), A: 255}
}
