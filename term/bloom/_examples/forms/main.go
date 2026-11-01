// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Command forms is a profile form with stacked confirmation dialogs, in the
// style of vex. Inputs are focusable and show the terminal cursor, the
// language field suggests matches in a dropdown anchored below it, and
// saving asks for confirmation in a modal dialog, then again in a second one
// stacked on top when a profile was saved before. Each dialog is a focus
// scope: tab only moves between its buttons, and closing it gives focus
// back to where it was. The top dialog recolors everything beneath it, as
// vex does, and the second one springs into place.
//
// Keys: tab and shift+tab move focus, typing edits the focused input, enter
// presses the focused button or picks the first suggestion, esc closes the
// top dialog, ctrl+c quits. A click outside a dialog closes it.
package main

import (
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/bloom"
	"github.com/lrstanley/x/term/bloom/anim"
	"github.com/lrstanley/x/term/bloom/fx"
)

var (
	fg     = lipgloss.Color("#c0caf5")
	muted  = lipgloss.Color("#565f89")
	accent = lipgloss.Color("#bb9af7")
	warn   = lipgloss.Color("#ff9e64")

	panel     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(muted).Padding(1, 2)
	heading   = lipgloss.NewStyle().Bold(true).Foreground(accent)
	label     = lipgloss.NewStyle().Foreground(muted)
	input     = lipgloss.NewStyle().Foreground(fg).Background(lipgloss.Color("#24283b")).Padding(0, 1)
	inputOn   = input.Background(lipgloss.Color("#414868"))
	hint      = input.Foreground(muted)
	button    = lipgloss.NewStyle().Foreground(fg).Background(muted).Padding(0, 2)
	buttonOn  = button.Foreground(lipgloss.Color("#1a1b26")).Background(accent).Bold(true)
	dropdown  = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(accent)
	option    = lipgloss.NewStyle().Foreground(fg).Padding(0, 1)
	dialog    = lipgloss.NewStyle().Border(lipgloss.ThickBorder()).BorderForeground(accent).Padding(1, 3)
	danger    = dialog.BorderForeground(warn)
	toastBox  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#9ece6a")).Padding(0, 1)
	statusBar = lipgloss.NewStyle().Foreground(muted)

	// The vex backdrop: everything beneath the top dialog in two colors.
	backdrop = fx.Recolor{Fg: lipgloss.Color("#3b4261"), Bg: lipgloss.Color("#16161e")}
)

// The IDs nodes are given.
type (
	fieldID  int
	buttonID string
	optionID int
	scopeID  string
)

const (
	fieldName fieldID = iota
	fieldEmail
	fieldLanguage
)

const (
	scopeForm      scopeID = "form"
	scopeConfirm   scopeID = "confirm"
	scopeOverwrite scopeID = "overwrite"
)

var (
	fieldLabels  = []string{"Name", "Email", "Language"}
	placeholders = []string{"Ada Lovelace", "ada@example.com", "start typing…"}
	languages    = []string{"Go", "Rust", "Python", "TypeScript", "Zig", "Haskell", "OCaml", "Elixir", "Gleam", "Kotlin"}
)

type model struct {
	ui     *bloom.Root
	w, h   int
	focus  bloom.Focus
	values [3]string
	saved  int

	confirm   anim.Presence
	overwrite anim.Presence // a spring, so it overshoots
	toast     anim.Presence
}

func newModel() *model {
	m := &model{
		ui:        bloom.New(),
		confirm:   anim.NewPresence(anim.Duration(200 * time.Millisecond)),
		overwrite: anim.NewPresence(anim.Spring(9, 0.35)),
		toast:     anim.NewPresence(anim.Duration(600*time.Millisecond), anim.Ease(anim.Linear)),
	}
	m.focus.Set(fieldName)
	return m
}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tea.KeyPressMsg:
		if !m.focus.Update(msg, m.ui.Frame()) {
			cmds = append(cmds, m.key(msg))
		}
	case bloom.MouseMsg:
		cmds = append(cmds, m.mouse(msg))
	}
	cmds = append(cmds, anim.Update(msg, &m.confirm, &m.overwrite, &m.toast))
	return m, tea.Batch(cmds...)
}

func (m *model) key(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c":
		return tea.Quit
	case "esc":
		return m.closeTop()
	case "enter":
		if b, ok := m.focus.Current().(buttonID); ok {
			return m.press(b)
		}
		if m.focus.Is(fieldLanguage) {
			if s := m.suggestions(); len(s) > 0 {
				m.values[fieldLanguage] = s[0]
			}
		}
	case "backspace":
		if f, ok := m.focus.Current().(fieldID); ok {
			v := []rune(m.values[f])
			m.values[f] = string(v[:max(len(v)-1, 0)])
		}
	default:
		if f, ok := m.focus.Current().(fieldID); ok && msg.Text != "" {
			m.values[f] += msg.Text
		}
	}
	return nil
}

func (m *model) mouse(msg bloom.MouseMsg) tea.Cmd {
	if _, ok := msg.Msg.(tea.MouseClickMsg); !ok {
		return nil
	}
	switch {
	case msg.Outside:
		return m.closeTop()
	case m.focus.Update(msg, m.ui.Frame()):
		if b, ok := msg.Target[buttonID](); ok {
			return m.press(b)
		}
	default:
		if o, ok := msg.Target[optionID](); ok {
			m.values[fieldLanguage] = m.suggestions()[o]
		}
	}
	return nil
}

func (m *model) press(b buttonID) tea.Cmd {
	switch b {
	case "save":
		m.focus.Push(scopeConfirm)
		m.focus.Set(buttonID("confirm"))
		return m.confirm.Show()
	case "reset":
		m.values = [3]string{}
		m.focus.Set(fieldName)
	case "confirm":
		if m.saved == 0 {
			return m.save()
		}
		m.focus.Push(scopeOverwrite)
		m.focus.Set(buttonID("back"))
		return m.overwrite.Show()
	case "overwrite":
		return m.save()
	case "cancel", "back":
		return m.closeTop()
	}
	return nil
}

// closeTop closes the topmost dialog, giving focus back to where it was.
func (m *model) closeTop() tea.Cmd {
	switch m.focus.Scope() {
	case scopeOverwrite:
		m.focus.Pop()
		return m.overwrite.Hide()
	case scopeConfirm:
		m.focus.Pop()
		return m.confirm.Hide()
	}
	return nil
}

func (m *model) save() tea.Cmd {
	m.saved++
	return tea.Batch(m.closeTop(), m.closeTop(), m.toast.ShowFor(2*time.Second))
}

func (m *model) suggestions() []string {
	q := strings.ToLower(m.values[fieldLanguage])
	var out []string
	for _, l := range languages {
		if q != "" && strings.Contains(strings.ToLower(l), q) && !strings.EqualFold(l, q) {
			out = append(out, l)
		}
	}
	return out
}

func (m *model) View() tea.View {
	v := m.ui.View(m.w, m.h, m.layout())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m *model) field(id fieldID) bloom.Child {
	text, st := m.values[id], input
	if text == "" {
		text, st = placeholders[id], hint
	}
	in := bloom.Text(text).Style(st).ID(id).Focusable()
	if m.focus.Is(id) {
		if m.values[id] == "" {
			st = inputOn.Foreground(muted)
		} else {
			st = inputOn
		}
		in = in.Style(st).Cursor(tea.NewCursor(1+ansi.StringWidth(m.values[id]), 0))
	}
	return bloom.Row(bloom.Fixed(10, bloom.Text(fieldLabels[id]).Style(label)), bloom.Fill(in))
}

func (m *model) button(id buttonID, text string) bloom.Child {
	st := button
	if m.focus.Is(id) {
		st = buttonOn
	}
	return bloom.Text(text).Style(st).ID(id).Focusable()
}

func (m *model) layout() bloom.Child {
	form := bloom.Box(panel, bloom.Column(
		bloom.Text("Profile").Style(heading),
		bloom.Fixed(1, nil),
		m.field(fieldName),
		bloom.Fixed(1, nil),
		m.field(fieldEmail),
		bloom.Fixed(1, nil),
		m.field(fieldLanguage),
		bloom.Fixed(1, nil),
		bloom.Row(bloom.Space(), m.button("reset", "reset"), m.button("save", "save")).Gap(2),
	))

	base := bloom.Column(
		bloom.Fill(bloom.Centered(bloom.Row(bloom.Fixed(60, form).ID(scopeForm).Scope()))),
		bloom.Fixed(1, bloom.Text(fmt.Sprintf(" focus: %v · in the form: %v · dialogs open: %d · saved %d times",
			m.focus.Current(), m.focus.Within(m.ui.Frame(), scopeForm), m.dialogs(), m.saved)).Style(statusBar)),
	)
	return bloom.Overlay(base, m.layers()...)
}

func (m *model) dialogs() int {
	n := 0
	if m.confirm.Shown() {
		n++
	}
	if m.overwrite.Shown() {
		n++
	}
	return n
}

func (m *model) layers() []bloom.LayerNode {
	var layers []bloom.LayerNode

	// The dropdown is a plain layer under the language input, not modal.
	if s := m.suggestions(); m.focus.Is(fieldLanguage) && len(s) > 0 {
		opts := make([]bloom.Child, len(s))
		for i, l := range s {
			opts[i] = bloom.Text(l).Style(option).ID(optionID(i))
		}
		layers = append(layers, bloom.Layer(bloom.Box(dropdown, bloom.Column(opts...))).
			AnchorTo(fieldLanguage, bloom.Bottom))
	}

	confirm := bloom.Layer(bloom.Box(dialog, bloom.Column(
		bloom.Text("Save profile?").Style(heading),
		bloom.Text(fmt.Sprintf("%s <%s>, %s", m.values[fieldName], m.values[fieldEmail], m.values[fieldLanguage])),
		bloom.Fixed(1, nil),
		bloom.Row(bloom.Space(), m.button("cancel", "cancel"), m.button("confirm", "confirm")).Gap(2),
	))).ID(scopeConfirm).Modal().Shadow(2, 1, fx.Dim{Amount: 0.7}).
		Presence(m.confirm, fx.Fade{}, fx.Slide{From: fx.Bottom})
	// Like vex, only the top dialog has a backdrop, which recolors the
	// dialogs beneath it too.
	if !m.overwrite.Visible() {
		confirm = confirm.Backdrop(backdrop)
	}

	// The spring overshoots 1, which bounces the dialog past its place.
	overwrite := bloom.Layer(bloom.Box(danger, bloom.Column(
		bloom.Text("Overwrite the saved profile?").Style(heading.Foreground(warn)),
		bloom.Text(fmt.Sprintf("It was saved %d time(s) before.", m.saved)),
		bloom.Fixed(1, nil),
		bloom.Row(bloom.Space(), m.button("back", "back"), m.button("overwrite", "overwrite")).Gap(2),
	))).ID(scopeOverwrite).Modal().Offset(0, int(math.Round((1-m.overwrite.Float())*-10))).
		Backdrop(backdrop).Shadow(2, 1, fx.Dim{Amount: 0.7}).
		Presence(m.overwrite, fx.Fade{})

	toast := bloom.Layer(bloom.Box(toastBox, bloom.Text("✓ Profile saved"))).
		Anchor(bloom.End, bloom.End).Margin(1).
		Presence(m.toast, fx.Typewriter{}, fx.Fade{})

	return append(layers, confirm, overwrite, toast)
}

func main() {
	if _, err := tea.NewProgram(newModel()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
