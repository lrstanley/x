// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"fmt"
	"hash/fnv"
	"image/color"
	"math"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/go-nf/glyphs/dev"
	"github.com/lrstanley/go-nf/glyphs/fa"
	"github.com/lrstanley/go-nf/glyphs/oct"
)

const (
	minWidth  = 90
	minHeight = 28
)

type layout struct {
	contentW int
	chartW   [3]int
	chartH   int
	svcW     int
	pipeW    int
	midH     int
	logH     int
}

func (m model) layout() layout {
	l := layout{contentW: m.width - 2, chartH: 9, midH: 11}
	each := (l.contentW - 2) / 3
	l.chartW = [3]int{each, each, l.contentW - 2 - 2*each}
	l.svcW = l.contentW * 59 / 100
	l.pipeW = l.contentW - l.svcW - 1
	// header, gap, charts, services/pipeline, logs, gap, footer.
	l.logH = max(m.height-1-1-l.chartH-l.midH-1-1, 4)
	return l
}

// reveal is how far section i has faded in as the dashboard appears.
func (m model) reveal(i int) float64 {
	return easeOut((m.dash() - float64(i)*0.07) / 0.4)
}

// sectionStyles returns the styles for a section that has faded in by a,
// under the current scrim.
func (m model) sectionStyles(a float64) *styles {
	return m.fadedStyles(a * m.scrim())
}

// scrim is how visible the dashboard is behind the approval prompt and while
// the terminal is unfocused.
func (m model) scrim() float64 {
	return 1 - 0.55*min(max(m.modalIn, 0), 1) - 0.35*m.blurAmount
}

func (m model) View() tea.View {
	var v tea.View
	v.AltScreen = true
	v.ReportFocus = true
	v.BackgroundColor = m.pal.bg
	v.ForegroundColor = m.pal.text
	v.WindowTitle = fa.Rocket.String() + " aurora · prod-us-east"

	switch {
	case m.width < minWidth || m.height < minHeight:
		v.Content = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			m.st.muted.Render(fmt.Sprintf("aurora needs at least %dx%d, have %dx%d", minWidth, minHeight, m.width, m.height)))
	case m.t < bootEnd:
		v.WindowTitle = fa.Rocket.String() + " aurora · booting"
		v.ProgressBar = bootProgress(m.t)
		st := m.fadedStyles(1 - easeOut((m.t-bootFadeFrom)/(bootEnd-bootFadeFrom)))
		v.Content = bootView(st, m.t, m.width, m.height)
	default:
		v.Content = m.dashboardView()
		v.ProgramStatus = m.deploy.status(m.t)
	}
	return v
}

func (m model) dashboardView() string {
	l := m.layout()

	charts := lipgloss.JoinHorizontal(lipgloss.Top,
		m.cpuView(m.sectionStyles(m.reveal(1)), l.chartW[0], l.chartH), " ",
		m.rpsView(m.sectionStyles(m.reveal(2)), l.chartW[1], l.chartH), " ",
		m.latencyView(m.sectionStyles(m.reveal(3)), l.chartW[2], l.chartH),
	)
	mid := lipgloss.JoinHorizontal(lipgloss.Top,
		m.servicesView(m.sectionStyles(m.reveal(4)), l.svcW, l.midH), " ",
		m.pipelineView(m.sectionStyles(m.reveal(5)), l.pipeW, l.midH),
	)
	body := strings.Join([]string{
		m.headerView(m.sectionStyles(m.reveal(0)), l.contentW),
		"",
		charts,
		mid,
		m.logsView(m.reveal(6)*m.scrim(), l.contentW, l.logH),
		"",
		m.footerView(m.sectionStyles(m.reveal(7)), l.contentW),
	}, "\n")
	base := lipgloss.NewStyle().Padding(0, 1).Render(body)

	var overlays []*lipgloss.Layer
	if m.modalIn > 0.02 {
		modal := m.promptView()
		mw, mh := lipgloss.Width(modal), lipgloss.Height(modal)
		y := (m.height-mh)/2 + int((1-min(m.modalIn, 1))*3)
		overlays = append(overlays, lipgloss.NewLayer(modal).X((m.width-mw)/2).Y(y).Z(1))
	}
	if m.toast != nil && m.toastX < 0.98 {
		box := m.toastView()
		bw, bh := lipgloss.Width(box), lipgloss.Height(box)
		// The spring settles around 0 without landing on it, so round, or
		// the box stays a cell short of its right border.
		shown := min(int(math.Round(float64(bw)*(1-m.toastX))), bw)
		if shown > 0 {
			var cut []string
			for line := range strings.SplitSeq(box, "\n") {
				cut = append(cut, ansi.Cut(line, 0, shown))
			}
			// Bottom right, inside the event log, clear of the panel titles.
			y := m.height - 3 - bh
			overlays = append(overlays, lipgloss.NewLayer(strings.Join(cut, "\n")).X(m.width-2-shown).Y(y).Z(2))
		}
	}
	if len(overlays) == 0 {
		return base
	}
	return lipgloss.NewCompositor(append([]*lipgloss.Layer{lipgloss.NewLayer(base)}, overlays...)...).Render()
}

// epoch is the wall clock time the dashboard appears at.
var epoch = time.Date(2026, 10, 24, 14, 2, 7, 0, time.UTC)

// clockAt is the wall clock time sec seconds after the dashboard appears.
func clockAt(sec float64) time.Time {
	return epoch.Add(time.Duration(sec * float64(time.Second)))
}

func (m model) headerView(st *styles, w int) string {
	brand := gradientText(st.pal, "◆ AURORA", m.t*0.18, true) + st.subtle.Render("  ·  ") + st.muted.Render("control room")
	state := st.ok.Render("●") + st.text.Render(" all systems nominal")
	switch {
	case m.blurred:
		state = st.subtle.Render("◌ paused")
	case m.deploy != nil && m.deploy.state == deployFailed:
		state = st.err.Render("●") + st.text.Render(" 1 deploy failed")
	case m.deploy.active():
		state = st.warn.Render("●") + st.text.Render(" deploy in progress")
	}
	right := state + st.subtle.Render("   ") +
		st.muted.Render(fa.Globe.String()+" ") + st.text.Render("prod-us-east") + st.subtle.Render("   ") +
		st.muted.Render(fa.Clock.String()+" ") + st.text.Render(clockAt(m.dash()).Format("15:04:05")) + st.subtle.Render(" UTC")
	return spread(brand, right, w)
}

func (m model) cpuView(st *styles, w, h int) string {
	p := panel{title: oct.Cpu.String() + " cpu", meta: st.bright.Render(fmt.Sprintf("%4.1f%%", m.last.cpu*100))}
	return p.view(st, w, h, barChart(st.pal, m.cpu, w-2, h-2, st.pal.accent[0], st.pal.accent[1], st.pal.accent[2]))
}

func (m model) rpsView(st *styles, w, h int) string {
	meta := st.bright.Render(commas(int(m.shownRPS))) + st.muted.Render(" rps")
	p := panel{title: fa.Bolt.String() + " throughput", meta: meta}
	return p.view(st, w, h, brailleArea(st.pal, m.rps, w-2, h-2, st.pal.accent[1]))
}

func (m model) latencyView(st *styles, w, h int) string {
	p50c, p99c := st.pal.accent[0], st.pal.accent[2]
	meta := lipgloss.NewStyle().Foreground(p50c).Render("p50 ") + st.bright.Render(fmt.Sprintf("%.0f", m.last.p50)) +
		st.subtle.Render(" · ") +
		lipgloss.NewStyle().Foreground(p99c).Render("p99 ") + st.bright.Render(fmt.Sprintf("%.0f", m.last.p99)) + st.muted.Render("ms")
	p := panel{title: fa.Stopwatch.String() + " latency", meta: meta}
	_, hi := valueRange(tail(m.p99, (w-2)*2), 0.2)
	lines := brailleLines(st.pal, [][]float64{m.p50, m.p99}, []color.Color{p50c, p99c}, max(hi, 160), w-2, h-2)
	return p.view(st, w, h, lines)
}

func commas(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// serviceColor gives every service its own stable spot on the gradient.
func serviceColor(p palette, name string) color.Color {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return p.gradient(float64(h.Sum32()%1000) / 999)
}

func (m model) servicesView(st *styles, w, h int) string {
	inner := w - 2
	const nameW, verW, rpsW, p99W = 16, 10, 8, 8
	trendW := max(inner-3-nameW-verW-rpsW-p99W-3, 4)
	statusW := 0
	if trendW > 22 {
		statusW, trendW = 12, trendW-12
	}

	header := "   " + fit(st.label.Render("SERVICE"), nameW) + fit(st.label.Render("VERSION"), verW) +
		lipgloss.PlaceHorizontal(rpsW, lipgloss.Right, st.label.Render("RPS")) +
		lipgloss.PlaceHorizontal(p99W, lipgloss.Right, st.label.Render("P99")) + "   " + st.label.Render("TREND")
	lines := []string{header}

	for i, s := range m.services {
		dot, status := st.ok.Render("●"), st.muted.Render("healthy")
		switch s.health {
		case degraded:
			dot, status = st.warn.Render("●"), st.warn.Render("degraded")
		case deploying:
			pulse := snap(0.5+0.5*math.Sin(m.t*5), 3)
			dot = lipgloss.NewStyle().Foreground(mix(st.pal.accent[1], st.pal.bright, pulse*0.5)).Render("◉")
			status = st.accent.Render(fmt.Sprintf("deploy %2.0f%%", m.deploy.progress(m.t)*100))
		case failing:
			dot, status = st.err.Render("●"), st.err.Render("failed")
		}
		name := st.text.Render(s.name)
		if i == m.selected {
			name = st.bright.Bold(true).Render(s.name)
		}
		p99 := st.text
		if s.p99 > 140 {
			p99 = st.warn
		}
		row := " " + dot + " " + fit(name, nameW) + fit(st.muted.Render(s.version), verW) +
			lipgloss.PlaceHorizontal(rpsW, lipgloss.Right, st.text.Render(commas(int(s.rps)))) +
			lipgloss.PlaceHorizontal(p99W, lipgloss.Right, p99.Render(fmt.Sprintf("%.0fms", s.p99))) + "   " +
			sparkline(s.trend, trendW, mix(serviceColor(st.pal, s.name), st.pal.bg, 0.45), serviceColor(st.pal, s.name))
		if statusW > 0 {
			row += "  " + fit(status, statusW-2)
		}
		row = fit(row, inner)
		if i == m.selected {
			marker := st.subtle.Render("▎")
			if m.focus == focusServices {
				marker = lipgloss.NewStyle().Foreground(st.pal.accent[0]).Render("▎")
			}
			row = withBg(marker+ansi.Cut(row, 1, inner), st.pal.surface)
		}
		lines = append(lines, row)
	}

	up := 0
	for _, s := range m.services {
		if s.health == healthy {
			up++
		}
	}
	p := panel{
		title:   fa.Server.String() + " services",
		meta:    st.muted.Render(fmt.Sprintf("%d/%d healthy", up, len(m.services))),
		focused: m.focus == focusServices,
		phase:   m.t * 0.25,
	}
	return p.view(st, w, h, lines)
}

// withBg gives the whole of s the background c, including the parts after
// a style reset inside it.
func withBg(s string, c color.Color) string {
	bg := bgSeq(c)
	s = strings.ReplaceAll(s, "\x1b[0m", "\x1b[m")
	s = strings.ReplaceAll(s, "\x1b[m", "\x1b[m"+bg)
	return bg + s + bgReset
}

func (m model) pipelineView(st *styles, w, h int) string {
	svc := m.services[m.selected]
	d := m.deploy
	if d.active() {
		svc = m.services[d.svc]
	}
	meta := ""
	if d != nil && d.name == svc.name {
		meta = st.muted.Render(dev.GitBranch.String() + " main@8f3a2c1")
	}
	p := panel{title: fa.Rocket.String() + " pipeline", meta: meta, focused: m.focus == focusPipeline, phase: m.t * 0.25}
	return p.view(st, w, h, pipelineBody(st, d, svc, m.t, w-2, h-2))
}

// logsView draws the event log faded in by a. New lines fade in further.
func (m model) logsView(a float64, w, h int) string {
	st := m.fadedStyles(a)
	inner := h - 2
	logs := m.logs[max(len(m.logs)-inner, 0):]
	lines := make([]string, 0, inner)
	for range inner - len(logs) {
		lines = append(lines, "")
	}
	for _, l := range logs {
		lines = append(lines, " "+logLineView(m.fadedStyles(a*easeOut((m.dash()-l.at)/0.35)), l))
	}
	p := panel{
		title:   fa.List.String() + " events",
		meta:    st.muted.Render("live ") + st.ok.Render("●"),
		focused: m.focus == focusLogs,
		phase:   m.t * 0.25,
	}
	return p.view(st, w, h, lines)
}

func logLineView(st *styles, l logLine) string {
	ts := clockAt(l.at)
	var level string
	msg := st.text
	switch l.level {
	case levelDebug:
		level, msg = st.subtle.Render("DEBUG"), st.muted
	case levelInfo:
		level = lipgloss.NewStyle().Foreground(st.pal.accent[0]).Render("INFO ")
	case levelWarn:
		level, msg = st.warn.Render("WARN "), st.warn
	case levelError:
		level, msg = st.err.Bold(true).Render("ERROR"), st.err
	}
	svc := lipgloss.NewStyle().Foreground(serviceColor(st.pal, l.service)).Render(fit(l.service, 14))

	// key=value pairs get a muted key and a bright value.
	var sb strings.Builder
	for i, word := range strings.Split(l.msg, " ") {
		if i > 0 {
			sb.WriteByte(' ')
		}
		if k, v, ok := strings.Cut(word, "="); ok && k != "" && l.level <= levelInfo {
			sb.WriteString(st.muted.Render(k+"=") + st.bright.Render(v))
			continue
		}
		sb.WriteString(msg.Render(word))
	}
	return st.subtle.Render(ts.Format("15:04:05.000")) + "  " + level + "  " + svc + " " + sb.String()
}

func (m model) footerView(st *styles, w int) string {
	bindings := m.keys.ShortHelp()
	if m.deploy != nil && m.deploy.state == deployAwaiting {
		bindings = m.keys.promptHelp()
	}
	h := m.help
	h.Styles.ShortKey, h.Styles.ShortDesc = st.key, st.keyDesc
	h.Styles.ShortSeparator, h.Styles.Ellipsis = st.subtle, st.subtle
	var swatch strings.Builder
	for i := range 6 {
		paint(&swatch, st.pal.gradient(float64(i)/5), "■")
	}
	right := st.muted.Render("theme ") + swatch.String() + " " + st.text.Render(st.pal.name)
	return spread(h.ShortHelpView(bindings), right, w)
}

func (m model) promptView() string {
	st := m.fadedStyles(m.modalIn)
	d := m.deploy
	const w = 54
	inner := w - 4
	btn := func(k, label string, c color.Color) string {
		return lipgloss.NewStyle().Background(c).Foreground(st.pal.bg).Bold(true).Render(" "+k+" ") + " " + st.text.Render(label)
	}
	body := []string{
		"",
		"  " + st.bold.Render("Promote "+d.name+" "+d.to+"?"),
		"",
		"  " + fit(st.muted.Render("The canary has served 25% of traffic with no"), inner),
		"  " + fit(st.muted.Render("regressions. Promoting rolls it out everywhere."), inner),
		"",
		"  " + st.ok.Render("✓") + st.text.Render(" errors 0.02%") + st.subtle.Render("   ") +
			st.ok.Render("✓") + st.text.Render(fmt.Sprintf(" p99 %.0fms", m.last.p99)) + st.subtle.Render("   ") +
			st.ok.Render("✓") + st.text.Render(" 3/3 regions"),
		"",
		"  " + center(btn("y", "promote", st.pal.ok)+st.subtle.Render("      ")+btn("n", "roll back", st.pal.err), inner),
		"",
	}
	p := panel{title: "approval required", meta: st.warn.Render("? blocked"), focused: true, phase: m.t * 0.35}
	box := p.view(st, w, len(body)+2, body)
	return withBg(box, st.pal.bg)
}

func (m model) toastView() string {
	st := m.st
	icon, c := st.ok.Render("✓"), st.pal.ok
	if !m.toast.ok {
		icon, c = st.err.Render("✗"), st.pal.err
	}
	text := " " + icon + "  " + st.bright.Render(m.toast.text) + " "
	// Only the inside is raised; the border sits on the dashboard, so its
	// rounded corners do not show square cells behind them.
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(c).
		Render(withBg(text, st.pal.surface))
}

// pipelineBody draws the inside of the pipeline panel, w x h cells.
func pipelineBody(st *styles, d *deploy, svc service, now float64, w, h int) []string {
	lines := make([]string, 0, h)
	pad := func(s string) string { return " " + s }
	inner := w - 2

	if d == nil || d.name != svc.name {
		lines = append(lines,
			pad(spread(st.bold.Render(svc.name), st.muted.Render(svc.version), inner)),
			pad(spread(st.muted.Render("last deploy 2h 14m ago"), st.ok.Render("● live"), inner)),
			"",
		)
		for i, s := range stages {
			lines = append(lines, pad(spread(st.subtle.Render(fmt.Sprintf("%d  %s", i+1, s.name)), st.subtle.Render("—"), inner)))
		}
		lines = append(lines, "", pad(st.muted.Render("press ")+st.key.Render("d")+st.muted.Render(" to deploy "+svc.name)))
		return lines
	}

	arrow := st.subtle.Render(" → ")
	to := st.bright.Render(d.to)
	switch d.state {
	case deployDone:
		to = st.ok.Render(d.to)
	case deployFailed, deployRolledBack:
		to = st.err.Render(d.to)
	}
	lines = append(lines,
		pad(spread(st.bold.Render(d.name), st.muted.Render(d.from)+arrow+to, inner)),
		"",
	)

	for i, s := range stages {
		var icon, name, detail string
		switch d.stageState(i) {
		case ansi.ProgramStateError:
			icon, name = st.err.Render("✗"), st.err.Render(s.name)
			detail = st.err.Render(fmt.Sprintf("%d failing", failingTests))
		case ansi.ProgramStateDone:
			icon, name = st.ok.Render("✓"), st.text.Render(s.name)
			detail = st.subtle.Render(fmt.Sprintf("%.1fs", d.stageTimes[i]))
			if i == 1 {
				detail = st.muted.Render(fmt.Sprintf("%d/%d  ", testCount, testCount)) + detail
			}
		case ansi.ProgramStateBlocked:
			icon, name = st.warn.Render("?"), st.warn.Render(s.name)
			detail = st.warn.Render("awaiting approval")
		case ansi.ProgramStateWorking:
			icon, name = st.accent.Render(spinnerFrame(now)), st.bright.Render(s.name)
			detail = st.muted.Render(stageDetail(i, d.within(now)))
		default:
			icon, name = st.subtle.Render("·"), st.subtle.Render(s.name)
		}
		lines = append(lines, pad(spread(icon+"  "+name, detail, inner)))
	}

	pct := d.progress(now)
	label := st.bright.Render(fmt.Sprintf("%3.0f%%", pct*100))
	switch d.state {
	case deployDone:
		label = st.ok.Render("done")
	case deployFailed:
		label = st.err.Render("failed")
	case deployRolledBack:
		label = st.warn.Render("rolled back")
	}
	bar := meter(st.pal, pct, inner-lipgloss.Width(label)-1)
	lines = append(lines, "", pad(bar+" "+label))
	return lines
}

func stageDetail(i int, within float64) string {
	switch i {
	case 0:
		return fmt.Sprintf("layer %d/12", 1+int(within*11))
	case 1:
		return fmt.Sprintf("%d/%d", int(within*testCount), testCount)
	case stageCanary:
		return fmt.Sprintf("%d%% traffic", 5+int(within*20))
	default:
		return fmt.Sprintf("%d%% traffic", 25+int(within*75))
	}
}
