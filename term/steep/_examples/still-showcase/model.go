// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/harmonica"
)

const (
	fps          = 20
	frameEvery   = time.Second / fps
	sampleEvery  = 0.15 // seconds between telemetry samples
	historyLen   = 240
	themeFadeDur = 0.6 // seconds a theme switch takes to cross-fade
	toastLife    = 3.2 // seconds a toast stays up
	maxLogLines  = 64
)

type frameMsg time.Time

func nextFrame() tea.Cmd {
	return tea.Tick(frameEvery, func(t time.Time) tea.Msg { return frameMsg(t) })
}

type focusArea int

const (
	focusServices focusArea = iota
	focusPipeline
	focusLogs
	focusCount
)

type toast struct {
	at   float64
	ok   bool
	text string
}

type model struct {
	keys keyMap
	help help.Model

	width, height int

	// start is when the first frame ran; t is seconds since then. All
	// animation reads t, so the model alone owns time.
	start time.Time
	t     float64

	themeIdx  int
	themeFrom palette
	themeAt   float64
	fadeStep  int // of the theme cross-fade, out of fadeSteps
	pal       palette
	// faded are the styles of pal faded in from the background, one per
	// fade step; st is the last, fully in.
	faded [fadeSteps + 1]*styles
	st    *styles

	tel        *telemetry
	nextSample float64
	nextLog    float64
	cpu, rps   []float64
	p50, p99   []float64
	last       sample

	// shownRPS chases the latest throughput on a spring, so the number in the
	// panel title rolls rather than jumps.
	spring            harmonica.Spring
	shownRPS, rpsVel  float64
	toastX, toastVel  float64
	modalIn, modalVel float64
	services          []service
	selected          int
	focus             focusArea
	deploy            *deploy
	sentStages        string
	logs              []logLine
	toast             *toast
	blurred           bool
	blurAmount        float64
}

func newModel() model {
	m := model{
		keys:     newKeyMap(),
		help:     help.New(),
		width:    110,
		height:   32,
		themeAt:  -themeFadeDur,
		fadeStep: fadeSteps,
		pal:      themes[0],
		tel:      newTelemetry(),
		services: newServices(),
		spring:   harmonica.NewSpring(harmonica.FPS(fps), 9, 0.75),
	}
	m.themeFrom = m.pal
	// Prefill the history, so the charts are full the moment they appear.
	for range historyLen {
		m.addSample()
	}
	m.shownRPS = m.last.rps
	m.applyTheme()

	// And the event log, so it opens mid-stream.
	for i := range 8 {
		m.logs = appendCapped(m.logs, m.tel.ambientLog(m.services, -4+float64(i)*0.45), maxLogLines)
	}
	return m
}

func (m *model) addSample() {
	m.last = m.tel.next()
	m.cpu = appendCapped(m.cpu, m.last.cpu, historyLen)
	m.rps = appendCapped(m.rps, m.last.rps, historyLen)
	m.p50 = appendCapped(m.p50, m.last.p50, historyLen)
	m.p99 = appendCapped(m.p99, m.last.p99, historyLen)
	m.tel.step(m.services)
}

// appendCapped appends v to s, dropping the oldest element once s holds n.
func appendCapped[T any](s []T, v T, n int) []T {
	if len(s) == n {
		copy(s, s[1:])
		s = s[:n-1]
	}
	return append(s, v)
}

// applyTheme rebuilds everything derived from the current palette.
func (m *model) applyTheme() {
	for i := range m.faded {
		m.faded[i] = newStyles(m.pal.fade(float64(i) / fadeSteps))
	}
	m.st = m.faded[fadeSteps]
}

// fadedStyles returns the styles faded in from the background by a.
func (m model) fadedStyles(a float64) *styles {
	return m.faded[int(min(max(a, 0), 1)*fadeSteps+0.5)]
}

// log appends an event to the log, stamped now.
func (m *model) log(level logLevel, service, msg string) {
	m.logs = appendCapped(m.logs, logLine{at: m.dash(), level: level, service: service, msg: msg}, maxLogLines)
}

// dash is seconds since the dashboard appeared.
func (m model) dash() float64 { return m.t - bootEnd }

func (m model) Init() tea.Cmd {
	return nextFrame()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.SetWidth(m.layout().contentW)
		return m, nil
	case tea.FocusMsg:
		m.blurred = false
		return m, nil
	case tea.BlurMsg:
		m.blurred = true
		return m, nil
	case frameMsg:
		return m.tick(time.Time(msg))
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) tick(now time.Time) (tea.Model, tea.Cmd) {
	if m.start.IsZero() {
		m.start = now
	}
	m.t = now.Sub(m.start).Seconds()
	var cmds []tea.Cmd

	// Theme cross-fade, rebuilt only when it reaches its next step.
	if step := int(smoothstep((m.t-m.themeAt)/themeFadeDur)*fadeSteps + 0.5); step != m.fadeStep {
		m.fadeStep = step
		m.pal = m.themeFrom.blend(themes[m.themeIdx], float64(step)/fadeSteps)
		m.applyTheme()
	}

	// Telemetry keeps flowing during boot, so the charts are live on reveal.
	if !m.blurred {
		for m.t >= m.nextSample {
			m.nextSample += sampleEvery
			m.addSample()
		}
		if m.dash() >= m.nextLog && m.dash() > 0 {
			m.nextLog = m.dash() + 0.25 + m.tel.rng.Float64()*0.5
			m.logs = appendCapped(m.logs, m.tel.ambientLog(m.services, m.dash()), maxLogLines)
		}
	}
	m.shownRPS, m.rpsVel = m.spring.Update(m.shownRPS, m.rpsVel, m.last.rps)

	// Deploys.
	if d := m.deploy; d != nil {
		m.applyDeploy(d.advance)
		m.tel.load = 0
		if d.active() {
			m.tel.load = 0.12 + 0.1*float64(d.stage)/float64(len(stages))
		}
		if sk := d.stageKey(); sk != m.sentStages {
			m.sentStages = sk
			cmds = append(cmds, tea.Raw(d.stageStatuses()))
		}
	}

	// Springs for the toast and the approval prompt.
	toastTarget := 1.0
	if m.toast != nil && m.t-m.toast.at < toastLife {
		toastTarget = 0
	}
	m.toastX, m.toastVel = m.spring.Update(m.toastX, m.toastVel, toastTarget)
	modalTarget := 0.0
	if m.deploy != nil && m.deploy.state == deployAwaiting {
		modalTarget = 1
	}
	m.modalIn, m.modalVel = m.spring.Update(m.modalIn, m.modalVel, modalTarget)
	blurTarget := 0.0
	if m.blurred {
		blurTarget = 1
	}
	m.blurAmount += (blurTarget - m.blurAmount) * 0.25

	return m, tea.Batch(append(cmds, nextFrame())...)
}

// applyDeploy runs step (advance, approve, or reject) on the deploy, logs
// what happened, and applies a change of state to its service once.
func (m *model) applyDeploy(step func(now float64) []deployEvent) {
	d := m.deploy
	prev := d.state
	for _, ev := range step(m.t) {
		m.log(ev.level, d.name, ev.msg)
	}
	if d.state == prev {
		return
	}
	s := &m.services[d.svc]
	switch d.state {
	case deployRunning, deployAwaiting:
		s.health = deploying
	case deployDone:
		s.version, s.health = d.to, healthy
		m.toast = &toast{at: m.t, ok: true, text: d.name + " " + d.to + " is live"}
	case deployFailed:
		s.health = failing
		m.toast = &toast{at: m.t, text: d.name + " deploy failed"}
	case deployRolledBack:
		s.health = healthy
	}
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Quit) {
		return m, tea.Quit
	}
	if m.t < bootEnd {
		return m, nil
	}
	if key.Matches(msg, m.keys.Theme) {
		m.themeFrom = m.pal
		m.themeIdx = (m.themeIdx + 1) % len(themes)
		m.themeAt = m.t
		return m, nil
	}

	if d := m.deploy; d != nil && d.state == deployAwaiting {
		switch {
		case key.Matches(msg, m.keys.Approve):
			m.applyDeploy(d.approve)
		case key.Matches(msg, m.keys.Reject):
			m.applyDeploy(d.reject)
		}
		return m, nil
	}

	switch {
	case key.Matches(msg, m.keys.Focus):
		step := focusArea(1)
		if msg.String() == "shift+tab" {
			step = focusCount - 1
		}
		m.focus = (m.focus + step) % focusCount
	case key.Matches(msg, m.keys.Up):
		if m.focus == focusServices {
			m.selected = (m.selected + len(m.services) - 1) % len(m.services)
		}
	case key.Matches(msg, m.keys.Down):
		if m.focus == focusServices {
			m.selected = (m.selected + 1) % len(m.services)
		}
	case key.Matches(msg, m.keys.Deploy):
		if m.deploy.active() {
			return m, nil
		}
		d := newDeploy(m.selected, m.services[m.selected], m.t)
		m.deploy = d
		m.services[d.svc].health = deploying
		m.log(levelInfo, d.name, "deploy requested  "+d.from+" → "+d.to+" commit=8f3a2c1")
		m.log(levelInfo, d.name, "build started")
	}
	return m, nil
}

func smoothstep(k float64) float64 {
	k = min(max(k, 0), 1)
	return k * k * (3 - 2*k)
}

func easeOut(k float64) float64 {
	k = min(max(k, 0), 1)
	return 1 - (1-k)*(1-k)*(1-k)
}
