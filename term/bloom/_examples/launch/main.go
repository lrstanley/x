// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Command launch is a deploy console that puts the motion effects to work.
// Cards shimmer as placeholders until their data arrives, and their numbers
// then roll over like an odometer as they change. Deploying needs the
// service's name typed in as confirmation, and a wrong one shakes the input.
// A tour spotlights each card in turn, decoding its explanation into place,
// and a successful deploy bursts confetti from the button and glitches a
// LIVE badge into the header.
//
// Keys: tab and shift+tab move focus, typing edits the input, enter deploys,
// ctrl+t steps the tour, ctrl+p pauses the numbers, ctrl+r starts over, esc
// ends the tour, ctrl+c quits.
package main

import (
	"fmt"
	"image"
	"os"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/bloom"
	"github.com/lrstanley/x/term/bloom/anim"
	"github.com/lrstanley/x/term/bloom/fx"
)

const service = "payments"

var (
	fg     = lipgloss.Color("#c0caf5")
	muted  = lipgloss.Color("#565f89")
	accent = lipgloss.Color("#7aa2f7")
	good   = lipgloss.Color("#9ece6a")
	warn   = lipgloss.Color("#ff9e64")
	dark   = lipgloss.Color("#1a1b26")

	card     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(muted).Padding(0, 2)
	heading  = lipgloss.NewStyle().Bold(true).Foreground(accent)
	label    = lipgloss.NewStyle().Foreground(muted)
	big      = lipgloss.NewStyle().Bold(true).Foreground(fg)
	skeleton = lipgloss.NewStyle().Foreground(lipgloss.Color("#2b3048"))
	input    = lipgloss.NewStyle().Foreground(fg).Background(lipgloss.Color("#24283b")).Padding(0, 1)
	inputOn  = input.Background(lipgloss.Color("#414868"))
	hint     = input.Foreground(muted)
	button   = lipgloss.NewStyle().Foreground(fg).Background(muted).Padding(0, 2)
	buttonOn = button.Foreground(dark).Background(accent).Bold(true)
	staged   = lipgloss.NewStyle().Foreground(muted).Padding(0, 1)
	live     = lipgloss.NewStyle().Foreground(dark).Background(good).Bold(true).Padding(0, 1)
	failure  = lipgloss.NewStyle().Foreground(warn)
	success  = lipgloss.NewStyle().Foreground(good)
	tour     = lipgloss.NewStyle().Border(lipgloss.ThickBorder()).BorderForeground(accent).Padding(0, 2)
	status   = lipgloss.NewStyle().Foreground(muted)
)

// The IDs nodes are given.
type (
	cardID int
	// formID identifies the input and the button of the form.
	formID string
)

const (
	inputConfirm formID = "confirm"
	buttonDeploy formID = "deploy"
)

// cards describe the statistics shown, with their values at the start.
var cards = [...]struct {
	title, unit string
	start       int
	// next is the value after n updates.
	next func(n int) int
}{
	{"Requests", "per minute", 1021, func(n int) int { return 1021 + n*37 + (n*n*11)%53 }},
	{"p95 latency", "milliseconds", 148, func(n int) int { return 120 + (n*37)%61 }},
	{"Errors", "per hour", 7, func(n int) int { return 3 + (n*5)%9 }},
}

// tourSteps are what the tour says about each card.
var tourSteps = [...][2]string{
	{"Traffic", "Requests served each minute, live."},
	{"Latency", "How long the slowest 5% of requests take."},
	{"Errors", "Failed requests in the past hour."},
}

const (
	shimmerPeriod = 1400 * time.Millisecond
	loadDelay     = 1500 * time.Millisecond
)

type (
	loadedMsg struct{}
	statsMsg  int // the generation of the ticker that sent it.
)

type model struct {
	ui    *bloom.Root
	w, h  int
	focus bloom.Focus

	loaded  bool
	shimmer anim.Sequence // loops until the data arrives.

	// every is how often the numbers change. Each change rolls from the
	// previous values to the new ones.
	every   time.Duration
	paused  bool
	gen     int
	updates int
	vals    [len(cards)]int
	prev    [len(cards)]int
	roll    anim.Value

	text  string // what was typed into the input.
	err   string
	shake anim.Value

	deployed bool
	burst    anim.Value
	badge    anim.Value
	bursts   uint32

	step     int // the tour step: 0 is off, then 1 for the first card.
	tourOn   anim.Presence
	tourText anim.Value
}

func newModel() *model {
	m := &model{
		ui:       bloom.New(),
		shimmer:  anim.NewLoop(shimmerPeriod),
		every:    time.Second,
		roll:     anim.NewValue(1, anim.Duration(700*time.Millisecond), anim.Ease(anim.Linear)),
		shake:    anim.NewValue(1, anim.Duration(450*time.Millisecond), anim.Ease(anim.Linear)),
		burst:    anim.NewValue(1, anim.Duration(1800*time.Millisecond), anim.Ease(anim.Linear)),
		badge:    anim.NewValue(1, anim.Duration(700*time.Millisecond), anim.Ease(anim.Linear)),
		tourOn:   anim.NewPresence(anim.Duration(250 * time.Millisecond)),
		tourText: anim.NewValue(1, anim.Duration(800*time.Millisecond), anim.Ease(anim.Linear)),
	}
	for i, c := range cards {
		m.vals[i], m.prev[i] = c.start, c.start
	}
	m.focus.Set(inputConfirm)
	return m
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(
		m.ui.Init(),
		m.shimmer.Loop(),
		tea.Tick(loadDelay, func(time.Time) tea.Msg { return loadedMsg{} }),
	)
}

// schedule returns the command for the next change of the numbers, which
// replaces any earlier one still waiting.
func (m *model) schedule() tea.Cmd {
	m.gen++
	gen := m.gen
	return tea.Tick(m.every, func(time.Time) tea.Msg { return statsMsg(gen) })
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case loadedMsg:
		m.loaded = true
		m.shimmer.Stop()
		cmds = append(cmds, m.schedule())
	case statsMsg:
		if int(msg) == m.gen && !m.paused {
			m.updates++
			m.prev = m.vals
			for i, c := range cards {
				m.vals[i] = c.next(m.updates)
			}
			cmds = append(cmds, m.roll.Play(0, 1), m.schedule())
		}
	case tea.KeyPressMsg:
		if !m.focus.Update(msg, m.ui.Frame()) {
			cmds = append(cmds, m.key(msg))
		}
	case bloom.MouseMsg:
		if m.focus.Update(msg, m.ui.Frame()) {
			if id, found := msg.Target[formID](); found && id == buttonDeploy {
				cmds = append(cmds, m.deploy())
			}
		}
	}
	cmds = append(cmds, m.ui.Update(msg), anim.Update(msg, &m.shimmer, &m.roll, &m.shake, &m.burst,
		&m.badge, &m.tourOn, &m.tourText))
	return m, tea.Batch(cmds...)
}

func (m *model) key(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c":
		return tea.Quit
	case "enter":
		return m.deploy()
	case "esc":
		return m.endTour()
	case "ctrl+t":
		return m.nextStep()
	case "ctrl+p":
		m.paused = !m.paused
		if !m.paused && m.loaded {
			return m.schedule()
		}
	case "ctrl+r":
		m.text, m.err, m.deployed = "", "", false
		m.focus.Set(inputConfirm)
		m.burst.Set(1)
		m.badge.Set(1)
	case "backspace":
		if m.focus.Is(inputConfirm) && !m.deployed {
			r := []rune(m.text)
			m.text = string(r[:max(len(r)-1, 0)])
		}
	default:
		if m.focus.Is(inputConfirm) && !m.deployed && msg.Text != "" {
			m.text += msg.Text
			m.err = ""
		}
	}
	return nil
}

// deploy deploys the service if its name was typed in, and shakes the input
// if not.
func (m *model) deploy() tea.Cmd {
	if m.deployed {
		return nil
	}
	if m.text != service {
		m.err = fmt.Sprintf("✗ type %q to confirm", service)
		return m.shake.Play(0, 1)
	}
	m.deployed, m.err = true, ""
	m.bursts++
	return tea.Batch(m.burst.Play(0, 1), m.badge.Play(0, 1))
}

// nextStep moves the tour on to the next card, or ends it after the last.
func (m *model) nextStep() tea.Cmd {
	m.step++
	if m.step > len(cards) {
		return m.endTour()
	}
	return tea.Batch(m.tourOn.Show(), m.tourText.Play(0, 1))
}

func (m *model) endTour() tea.Cmd {
	m.step = 0
	return m.tourOn.Hide()
}

func (m *model) View() tea.View {
	v := m.ui.View(m.w, m.h, m.layout())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// number formats n with a comma between each group of thousands.
func number(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// card is the card for statistic i: a placeholder that shimmers until the
// data arrives, then its number, rolling from the previous one while it
// changes.
func (m *model) card(i int) bloom.Child {
	c := cards[i]
	if !m.loaded {
		bar := func(n int) bloom.Child { return bloom.Text(strings.Repeat("█", n)).Style(skeleton) }
		return bloom.Transition(m.shimmer.Float(), bloom.Box(card, bloom.Column(
			bar(8), bloom.Fixed(1, nil), bar(5), bar(11),
		)), fx.Shimmer{Color: accent, Width: 10})
	}
	num := bloom.Child(bloom.Text(number(m.vals[i])).Style(big))
	if m.roll.Animating() {
		num = bloom.Transition(m.roll.Float(), num, fx.Roll{From: number(m.prev[i])})
	}
	return bloom.Box(card, bloom.Column(
		bloom.Text(c.title).Style(heading), bloom.Fixed(1, nil), num, bloom.Text(c.unit).Style(label),
	))
}

func (m *model) badgeNode() bloom.Child {
	if !m.deployed {
		return bloom.Text("STAGED").Style(staged)
	}
	b := bloom.Child(bloom.Text("LIVE").Style(live))
	if m.badge.Animating() {
		b = bloom.Transition(m.badge.Float(), b, fx.Glitch{Seed: m.bursts})
	}
	return b
}

func (m *model) form() bloom.Child {
	text, st := m.text, input
	if text == "" {
		text, st = service, hint
	}
	in := bloom.Text(text).Style(st).ID(inputConfirm).Focusable()
	if m.focus.Is(inputConfirm) {
		if m.text == "" {
			st = inputOn.Foreground(muted)
		} else {
			st = inputOn
		}
		in = in.Style(st).Cursor(tea.NewCursor(1+ansi.StringWidth(m.text), 0))
	}
	field := bloom.Child(in)
	if m.shake.Animating() {
		field = bloom.Transition(m.shake.Float(), field, fx.Shake{Amount: 3, Times: 3})
	}

	btn := button
	if m.focus.Is(buttonDeploy) {
		btn = buttonOn
	}
	msg := bloom.Text(" ")
	switch {
	case m.deployed:
		msg = bloom.Text("✓ " + service + " is live").Style(success)
	case m.err != "":
		msg = bloom.Text(m.err).Style(failure)
	}
	return bloom.Column(
		bloom.Text("Deploy "+service+" to production").Style(heading),
		bloom.Text("Type the service name to confirm.").Style(label),
		bloom.Fixed(1, nil),
		bloom.Row(bloom.Fill(field), bloom.Text("deploy").Style(btn).ID(buttonDeploy).Focusable()).Gap(2),
		msg,
	)
}

func (m *model) layout() bloom.Child {
	slots := make([]bloom.Child, len(cards))
	for i := range cards {
		slots[i] = bloom.Fill(m.card(i)).ID(cardID(i))
	}
	cardRow := bloom.Row(slots...).Gap(2)

	paused := ""
	if m.paused {
		paused = " (paused)"
	}
	base := bloom.Child(bloom.Column(
		bloom.Row(bloom.Text("◆ deploy").Style(heading), bloom.Text("· production").Style(label), bloom.Space(), m.badgeNode()).Gap(1),
		bloom.Fixed(1, nil),
		cardRow,
		bloom.Fixed(1, nil),
		m.form(),
		bloom.Space(),
		bloom.Text(fmt.Sprintf("ctrl+t tour · ctrl+p pause · ctrl+r reset · updates: %d%s · tour: %d", m.updates, paused, m.step)).Style(status),
	))

	// Confetti bursts from the middle of the button, over everything.
	if m.deployed && m.burst.Animating() {
		if r, ok := m.ui.Frame().Rect(buttonDeploy); ok {
			base = bloom.Transition(m.burst.Float(), base, fx.Burst{
				At: image.Pt((r.Min.X+r.Max.X)/2, (r.Min.Y+r.Max.Y)/2), Count: 90, Seed: m.bursts,
			})
		}
	}
	return bloom.Overlay(base, m.tourLayer())
}

// tourLayer is the tour's: a note at the bottom, over a backdrop that
// leaves only the card it explains lit.
func (m *model) tourLayer() bloom.LayerNode {
	step := min(max(m.step, 1), len(cards)) // while the last note animates out, it stays on the last card.
	note := bloom.Box(tour, bloom.Transition(m.tourText.Float(), bloom.Column(
		bloom.Text(tourSteps[step-1][0]).Style(heading),
		bloom.Text(tourSteps[step-1][1]),
	), fx.Decode{Seed: uint32(step)})) //nolint:gosec // step is small.
	l := bloom.Layer(note).Anchor(bloom.Center, bloom.End).Margin(2).Presence(m.tourOn, fx.Fade{})
	if r, ok := m.ui.Frame().Rect(cardID(step - 1)); ok {
		l = l.Backdrop(fx.Spotlight{Rect: r.Inset(-1), Soft: 5})
	}
	return l
}

func main() {
	if _, err := tea.NewProgram(newModel(), tea.WithFPS(60)).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
