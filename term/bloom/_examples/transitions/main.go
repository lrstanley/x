// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Command transitions is a gallery of reveal transitions and of the
// combinators that sequence, stagger, ease and reverse effects. Every card
// runs its transition in and out on a loop driven by one animated value.
// Two dialogs open with transitions of their own: one with an iris that
// grows from the button that opened it, the other unfolding.
//
// Keys: d opens the iris dialog, u the unfolding one, esc closes it, space
// pauses with the gallery fully shown, q quits.
package main

import (
	"fmt"
	"image"
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
	ink    = lipgloss.Color("#c0caf5")
	muted  = lipgloss.Color("#565f89")
	accent = lipgloss.Color("#7dcfff")

	card    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(muted)
	name    = lipgloss.NewStyle().Bold(true).Foreground(accent)
	panel   = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("#7aa2f7")).Foreground(ink).Background(lipgloss.Color("#2f334d")).Padding(0, 1)
	status  = lipgloss.NewStyle().Foreground(muted)
	button  = lipgloss.NewStyle().Foreground(lipgloss.Color("#1a1b26")).Background(accent).Padding(0, 1)
	dialog  = lipgloss.NewStyle().Border(lipgloss.DoubleBorder()).BorderForeground(lipgloss.Color("#e0af68")).Padding(1, 3)
	rowEven = lipgloss.NewStyle().Foreground(ink).Background(lipgloss.Color("#2f334d"))
	rowOdd  = lipgloss.NewStyle().Foreground(ink).Background(lipgloss.Color("#3b4261"))

	sampleTx = "The quick brown fox\njumps over the lazy\ndog. 0123456789 ✓\nWide glyphs: 世界 ✓\nthe border stays whole"
	items    = []string{"Inbox", "Drafts", "Sent", "Archive", "Spam", "Trash"}
)

// buttonID identifies the buttons that open dialogs.
type buttonID int

const (
	buttonIris buttonID = iota
	buttonUnfold
)

// gallery is what the cards show. Body is a list instead of a panel when
// list is set.
var gallery = []struct {
	name    string
	list    bool
	effects []fx.Effect
}{
	{"Unfold", false, []fx.Effect{fx.Unfold{}}},
	{"Iris", false, []fx.Effect{fx.Iris{}}},
	{"Shade", false, []fx.Effect{fx.Shade{Seed: 7}}},
	{"Blinds from the top", false, []fx.Effect{fx.Blinds{From: fx.Top}}},
	{"Wave from the bottom", false, []fx.Effect{fx.Wave{From: fx.Bottom}}},
	{"Wave from the left", false, []fx.Effect{fx.Wave{From: fx.Left}}},
	{"Stagger: Slide, by rows", true, []fx.Effect{fx.Stagger{Effect: fx.Slide{From: fx.Left}, From: fx.Top}}},
	{"Stagger: Shade, by columns", false, []fx.Effect{fx.Stagger{Effect: fx.Shade{Seed: 3}, From: fx.Left, Spread: 0.7}}},
	{"Delay: Fade, then Scramble", false, []fx.Effect{
		fx.Delay{Effect: fx.Fade{}, To: 0.5},
		fx.Delay{Effect: fx.Scramble{Seed: 5}, From: 0.5},
	}},
	{"Reverse: Wipe, leaving", false, []fx.Effect{fx.Reverse{Effect: fx.Wipe{From: fx.Left}}}},
	{"Ease: Slide with OutBack", false, []fx.Effect{fx.Ease{Effect: fx.Slide{From: fx.Left}, Easing: anim.OutBack}}},
	{"Blinds from the left", false, []fx.Effect{fx.Blinds{From: fx.Left, Size: 2}}},
}

type model struct {
	ui     *bloom.Root
	w, h   int
	t      anim.Sequence // the gallery's progress, in and out
	iris   anim.Presence
	unfold anim.Presence
	irisAt image.Point // frame coordinates of the button that opened the iris
	paused bool
}

func newModel() *model {
	return &model{
		ui:     bloom.New(),
		t:      progress(0, false),
		iris:   anim.NewPresence(anim.Duration(500*time.Millisecond), anim.Ease(anim.InOutCubic)),
		unfold: anim.NewPresence(anim.Duration(500*time.Millisecond), anim.Ease(anim.InOutCubic)),
	}
}

// progress returns the gallery's progress from start: in and out over and
// over, holding at each end, or when paused, settling fully shown.
func progress(start float64, paused bool) anim.Sequence {
	steps := []anim.Step{anim.To(1), anim.Hold(900 * time.Millisecond), anim.To(0), anim.Hold(900 * time.Millisecond)}
	if paused {
		steps = []anim.Step{anim.To(1, anim.Duration(300*time.Millisecond))}
	}
	return anim.NewSequence(start, steps...).With(anim.Duration(1600*time.Millisecond), anim.Ease(anim.InOutCubic))
}

func (m *model) Init() tea.Cmd { return tea.Batch(m.ui.Init(), m.t.Loop()) }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "space":
			m.paused = !m.paused
			m.t = progress(m.t.Float(), m.paused)
			if m.paused {
				cmds = append(cmds, m.t.Play())
			} else {
				cmds = append(cmds, m.t.Loop())
			}
		case "d":
			if !m.iris.Visible() && !m.unfold.Visible() {
				m.irisAt = center(m.ui.Frame(), buttonIris)
				cmds = append(cmds, m.iris.Show())
			}
		case "u":
			if !m.iris.Visible() && !m.unfold.Visible() {
				cmds = append(cmds, m.unfold.Show())
			}
		case "esc":
			cmds = append(cmds, m.iris.Hide(), m.unfold.Hide())
		}
	}

	cmds = append(cmds, m.ui.Update(msg), anim.Update(msg, &m.t, &m.iris, &m.unfold))
	return m, tea.Batch(cmds...)
}

// center returns the middle of the node with the given id in frame, or the
// zero point, which an iris takes to mean the middle of what it reveals.
func center(f *bloom.Frame, id buttonID) image.Point {
	r, ok := f.Rect(id)
	if !ok {
		return image.Point{}
	}
	return image.Pt((r.Min.X+r.Max.X)/2, (r.Min.Y+r.Max.Y)/2)
}

func (m *model) View() tea.View {
	v := m.ui.View(m.w, m.h, m.layout())
	v.AltScreen = true
	return v
}

// body returns what a card transitions: a bordered panel, or a list.
func body(list bool) bloom.Child {
	if !list {
		return bloom.Box(panel, bloom.Text(sampleTx))
	}
	rows := make([]bloom.Child, len(items))
	for i, item := range items {
		st := rowEven
		if i%2 == 1 {
			st = rowOdd
		}
		rows[i] = bloom.Text(fmt.Sprintf(" %-*s", 26, item)).Style(st)
	}
	return bloom.Column(rows...)
}

func (m *model) layout() bloom.Child {
	t := m.t.Float()
	cards := make([]bloom.Child, len(gallery))
	for i, g := range gallery {
		cards[i] = bloom.Box(card, bloom.Column(
			bloom.Text(g.name).Style(name),
			bloom.Fill(bloom.Column(bloom.Transition(t, body(g.list), g.effects...))),
		))
	}

	bar := bloom.Row(
		bloom.Text("d").Style(button), bloom.Text("Iris dialog").ID(buttonIris),
		bloom.Text("u").Style(button), bloom.Text("Unfold dialog").ID(buttonUnfold),
		bloom.Text(fmt.Sprintf("t = %.2f · space: pause · q: quit", t)).Style(status).Truncate(),
	).Gap(1)
	base := bloom.Column(bloom.Fill(bloom.Grid(4, cards...).Gap(1, 0)), bar)

	return bloom.Overlay(base,
		m.dialog("Iris dialog", "Opened by an iris that grows\nfrom the button that opened it.").Presence(m.iris, fx.Iris{At: m.irisAt}),
		m.dialog("Unfold dialog", "Opened by unfolding from\nits middle row outward.").Presence(m.unfold, fx.Unfold{}),
	)
}

// dialog returns a modal layer over a dimmed gallery.
func (m *model) dialog(title, text string) bloom.LayerNode {
	content := bloom.Column(
		bloom.Text(title).Style(name),
		bloom.Text(strings.TrimSpace(text)),
		bloom.Text("esc closes").Style(status),
	)
	return bloom.Layer(bloom.Box(dialog, content)).Modal().Backdrop(fx.Dim{Amount: 0.6})
}

func main() {
	if _, err := tea.NewProgram(newModel(), tea.WithFPS(60)).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
