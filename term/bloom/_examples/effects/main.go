// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Command effects is a gallery of cell effects. Every card runs its effect
// as a transition, in and out on a loop driven by one animated value; two of
// them are custom effects written with fx.Func, and one card is a
// uv.Drawable placed with bloom.Drawable. A layer on top cycles through the
// backdrop effects, applied to the whole gallery beneath it.
//
// Keys: b cycles the backdrop, space pauses, q quits.
package main

import (
	"fmt"
	"image/color"
	"math"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/term/bloom"
	"github.com/lrstanley/x/term/bloom/anim"
	"github.com/lrstanley/x/term/bloom/canvas"
	"github.com/lrstanley/x/term/bloom/fx"
)

var (
	ink   = lipgloss.Color("#c0caf5")
	muted = lipgloss.Color("#565f89")

	card     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(muted)
	name     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7dcfff"))
	sample   = lipgloss.NewStyle().Foreground(ink).Background(lipgloss.Color("#2f334d")).Padding(0, 1)
	overlay  = lipgloss.NewStyle().Border(lipgloss.DoubleBorder()).BorderForeground(lipgloss.Color("#e0af68")).Padding(1, 3)
	sampleTx = "The quick brown fox\njumps over the lazy\ndog. 0123456789 ✓"
)

// rainbow is a custom effect: glyphs take a hue by column that fades to
// their own color as t reaches 1.
var rainbow = fx.Func(func(s *canvas.Surface, t float64) {
	for y := range s.Height() {
		for x := range s.Width() {
			c := s.Cell(x, y)
			if c == nil || c.Content == " " {
				continue
			}
			hue := math.Mod(float64(x)/float64(max(s.Width(), 1))+t, 1)
			c.Style.Fg = s.Mix(hsv(hue), s.Fg(c), t)
		}
	}
})

// sweep is a custom effect: a bright band crosses the node diagonally,
// leaving it as it was once it is past.
var sweep = fx.Func(func(s *canvas.Surface, t float64) {
	pos := t*float64(s.Width()+s.Height()+6) - 3
	for y := range s.Height() {
		for x := range s.Width() {
			if d := math.Abs(float64(x+y) - pos); d < 3 {
				c := s.Cell(x, y)
				c.Style.Fg = s.Mix(s.Fg(c), color.White, 1-d/3)
				c.Style.Bg = s.Mix(s.Bg(c), lipgloss.Color("#7aa2f7"), (1-d/3)*0.6)
			}
		}
	}
})

// plasma is a [uv.Drawable] painting a moving color field with half blocks,
// and a [bloom.Measurer] for its natural size.
type plasma struct{ t float64 } // t is the loop's phase, from 0 to 1

func (p plasma) View() string { return "" }

func (p plasma) Measure(avail bloom.Size) bloom.Size {
	return bloom.Size{W: min(avail.W, 28), H: min(avail.H, 4)}
}

func (p plasma) Draw(scr uv.Screen, area uv.Rectangle) {
	phase := p.t * 20 * math.Pi // whole cycles of every wave, so the loop is seamless
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			v := func(row float64) float64 {
				return math.Sin(float64(x)/4+phase) + math.Sin((float64(x)+row)/6+phase*1.3) + math.Sin(row/3-phase)
			}
			top, bottom := v(float64(2*y)), v(float64(2*y+1))
			scr.SetCell(x, y, &uv.Cell{Content: "▀", Width: 1, Style: uv.Style{
				Fg: hsv(math.Mod((top+3)/6+p.t*6, 1)),
				Bg: hsv(math.Mod((bottom+3)/6+p.t*6, 1)),
			}})
		}
	}
}

// hsv returns a saturated color of hue h, from 0 to 1.
func hsv(h float64) color.Color {
	f := func(n float64) uint8 {
		k := math.Mod(n+h*6, 6)
		return uint8(255 * (1 - 0.65*max(0, min(k, 4-k, 1))))
	}
	return color.RGBA{R: f(5), G: f(3), B: f(1), A: 0xff}
}

var gallery = []struct {
	name    string
	effects []fx.Effect
}{
	{"Fade", []fx.Effect{fx.Fade{}}},
	{"Slide from the left", []fx.Effect{fx.Slide{From: fx.Left}}},
	{"Wipe from the top", []fx.Effect{fx.Wipe{From: fx.Top}}},
	{"Dissolve", []fx.Effect{fx.Dissolve{Seed: 11}}},
	{"Typewriter", []fx.Effect{fx.Typewriter{}}},
	{"Scramble + Fade", []fx.Effect{fx.Scramble{Seed: 5}, fx.Fade{}}},
	{"Custom: rainbow", []fx.Effect{rainbow}},
	{"Custom: sweep", []fx.Effect{sweep}},
}

var backdrops = []struct {
	name   string
	effect fx.Effect
}{
	{"Dim", fx.Dim{Amount: 0.6}},
	{"Desaturate", fx.Desaturate{Amount: 1}},
	{"Tint", fx.Tint{Color: lipgloss.Color("#7aa2f7"), Amount: 0.45}},
	{"Recolor", fx.Recolor{Fg: lipgloss.Color("#414868"), Bg: lipgloss.Color("#16161e")}},
}

type model struct {
	ui       *bloom.Root
	w, h     int
	t        anim.Sequence // the gallery's progress, in and out, holding at each end
	phase    anim.Sequence // the plasma's phase
	backdrop int           // index into backdrops, the last one shown while the overlay fades out
	overlay  anim.Presence
	paused   bool
}

func newModel() *model {
	return &model{
		ui: bloom.New(),
		t: anim.NewSequence(0,
			anim.To(1), anim.Hold(700*time.Millisecond),
			anim.To(0), anim.Hold(700*time.Millisecond),
		).With(anim.Duration(1400*time.Millisecond), anim.Ease(anim.InOutCubic)),
		phase:   anim.NewLoop(26 * time.Second),
		overlay: anim.NewPresence(anim.Duration(300 * time.Millisecond)),
	}
}

func (m *model) Init() tea.Cmd { return tea.Batch(m.t.Loop(), m.phase.Loop()) }

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
			if m.paused {
				m.t.Stop()
				m.phase.Stop()
			} else {
				cmds = append(cmds, m.t.Loop(), m.phase.Loop())
			}
		case "b":
			switch {
			case !m.overlay.Shown():
				m.backdrop = 0
				cmds = append(cmds, m.overlay.Show())
			case m.backdrop == len(backdrops)-1:
				cmds = append(cmds, m.overlay.Hide())
			default:
				m.backdrop++
			}
		}
	}
	return m, tea.Batch(append(cmds, anim.Update(msg, &m.t, &m.phase, &m.overlay))...)
}

func (m *model) View() tea.View {
	v := m.ui.View(m.w, m.h, m.layout())
	v.AltScreen = true
	return v
}

func (m *model) card(title string, body bloom.Child) bloom.Child {
	return bloom.Box(card, bloom.Column(
		bloom.Text(title).Style(name),
		bloom.Fill(body),
	))
}

func (m *model) layout() bloom.Child {
	t := m.t.Float()
	cards := make([]bloom.Child, 0, len(gallery)+1)
	for _, g := range gallery {
		cards = append(cards, m.card(g.name, bloom.Transition(t, bloom.Text(sampleTx).Style(sample), g.effects...)))
	}
	cards = append(cards, m.card("Drawable + Fade", bloom.Transition(t, bloom.Drawable(plasma{t: m.phase.Float()}), fx.Fade{})))

	status := fmt.Sprintf(" t = %.2f · b: backdrop (%s) · space: pause · q: quit", t, m.backdropName())
	base := bloom.Column(
		bloom.Fill(bloom.Grid(3, cards...).Gap(1, 0)),
		bloom.Text(status).Style(lipgloss.NewStyle().Foreground(muted)).Truncate(),
	)

	b := backdrops[m.backdrop]
	return bloom.Overlay(base, bloom.Layer(bloom.Box(overlay, bloom.Text("Backdrop: "+b.name).Style(name))).
		Backdrop(b.effect).
		Presence(m.overlay, fx.Fade{}))
}

func (m *model) backdropName() string {
	if !m.overlay.Shown() {
		return "none"
	}
	return backdrops[m.backdrop].name
}

func main() {
	if _, err := tea.NewProgram(newModel(), tea.WithFPS(60)).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
