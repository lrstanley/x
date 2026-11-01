// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package anim animates numbers in a Bubble Tea model: a [Value] eases or
// springs toward a target, and a [Presence] is a value from 0 to 1 for
// showing and hiding something, such as a dialog.
//
// Animations are model state, advanced by the messages their commands
// schedule: return the command from [Value.To] or [Presence.Show], and pass
// every message to Update, which schedules the next frame until the
// animation is done. Progress follows the clock, not the number of frames, so
// a slow frame does not slow an animation down.
package anim

import (
	"math"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
)

// FrameMsg advances an animation. Values ignore those that are not theirs.
type FrameMsg struct {
	id, gen uint64
}

// Easing maps linear progress, from 0 to 1, to eased progress.
type Easing func(t float64) float64

// Easings, for [Ease].
var (
	Linear     Easing = func(t float64) float64 { return t }
	InQuad     Easing = func(t float64) float64 { return t * t }
	OutQuad    Easing = func(t float64) float64 { return 1 - (1-t)*(1-t) }
	InOutQuad  Easing = func(t float64) float64 { return inOut(t, 2) }
	OutCubic   Easing = func(t float64) float64 { return 1 - (1-t)*(1-t)*(1-t) }
	InOutCubic Easing = func(t float64) float64 { return inOut(t, 3) }
	// OutBack overshoots the target a little before settling on it.
	OutBack Easing = func(t float64) float64 {
		const c1, c3 = 1.70158, 2.70158
		u := t - 1
		return 1 + c3*u*u*u + c1*u*u
	}
)

func inOut(t, p float64) float64 {
	if t < 0.5 {
		return math.Pow(2, p-1) * math.Pow(t, p)
	}
	return 1 - math.Pow(-2*t+2, p)/2
}

// Option configures a [Value] or [Presence].
type Option func(*Value)

// Duration makes the value ease to each target over d.
func Duration(d time.Duration) Option {
	return func(v *Value) { v.duration, v.spring = max(d, 0), false }
}

// Ease sets the easing of a duration-based value.
func Ease(e Easing) Option {
	return func(v *Value) { v.ease = e }
}

// Spring makes the value move like a damped spring of the given angular
// frequency, where higher is faster, and damping ratio, where 1 settles
// without overshooting and less than 1 bounces. Unlike easing, a spring
// keeps its speed when its target changes mid-flight.
func Spring(frequency, damping float64) Option {
	return func(v *Value) { v.spring, v.frequency, v.damping = true, frequency, damping }
}

// FPS sets how many frames per second the value asks for while animating.
// It need not be more than Bubble Tea renders, 60 by default.
func FPS(n int) Option {
	return func(v *Value) { v.interval = time.Second / time.Duration(max(n, 1)) }
}

// Clock sets the value's source of time, for tests.
func Clock(now func() time.Time) Option {
	return func(v *Value) { v.now = now }
}

var ids atomic.Uint64

// Value is a number that animates toward a target. The zero Value does not
// animate; make one with [NewValue].
type Value struct {
	id, gen uint64

	current, target float64
	from, velocity  float64
	start, last     time.Time
	running         bool

	duration  time.Duration
	ease      Easing
	spring    bool
	frequency float64
	damping   float64
	interval  time.Duration
	now       func() time.Time
}

// NewValue returns a value at v, which by default eases to each target over
// 250ms.
func NewValue(v float64, opts ...Option) Value {
	val := Value{
		id: ids.Add(1), current: v, target: v,
		duration: 250 * time.Millisecond, ease: OutCubic,
		interval: time.Second / 60, now: time.Now,
	}
	for _, opt := range opts {
		opt(&val)
	}
	return val
}

// Float returns the current value.
func (v *Value) Float() float64 { return v.current }

// Int returns the current value, rounded.
func (v *Value) Int() int { return int(math.Round(v.current)) }

// Target returns the value being animated toward.
func (v *Value) Target() float64 { return v.target }

// Animating reports whether the value is moving.
func (v *Value) Animating() bool { return v.running }

// Play jumps to from and starts animating to to, for something that runs
// from the start each time it happens, such as a shake or a burst. It
// returns the command for the first frame.
func (v *Value) Play(from, to float64) tea.Cmd {
	v.Set(from)
	return v.To(to)
}

// Set jumps to x without animating.
func (v *Value) Set(x float64) {
	v.current, v.target, v.velocity, v.running = x, x, 0, false
	v.gen++
}

// To starts animating toward target, from wherever the value is now. It
// returns the command for the first frame, or nil when an animation already
// running will carry on toward the new target.
func (v *Value) To(target float64) tea.Cmd {
	if v.now == nil || target == v.target {
		return nil
	}
	now := v.now()
	v.from, v.target, v.start, v.last = v.current, target, now, now
	if v.running {
		return nil
	}
	v.running = true
	v.gen++
	return v.frame()
}

// SetTarget starts moving toward target at now without scheduling a command.
// It preserves spring velocity when retargeted. Use [Value.Advance] with the
// same clock to drive it from an external frame loop.
func (v *Value) SetTarget(target float64, now time.Time) {
	if v.now == nil || target == v.target {
		return
	}
	v.from, v.target, v.start, v.last = v.current, target, now, now
	v.running = true
	v.gen++
}

// Advance moves the value to now without scheduling a command and reports
// whether it is still animating.
func (v *Value) Advance(now time.Time) bool {
	if v.running && v.advance(now) {
		v.current, v.velocity, v.running = v.target, 0, false
	}
	return v.running
}

// Update advances the value for its own [FrameMsg], returning the command for
// the next frame while it is animating.
func (v *Value) Update(msg tea.Msg) tea.Cmd {
	f, ok := msg.(FrameMsg)
	if !ok || f.id != v.id || f.gen != v.gen || !v.running {
		return nil
	}
	if !v.Advance(v.now()) {
		return nil
	}
	return v.frame()
}

func (v *Value) frame() tea.Cmd {
	msg := FrameMsg{id: v.id, gen: v.gen}
	return tea.Tick(v.interval, func(time.Time) tea.Msg { return msg })
}

// maxStep bounds the time a spring advances in one frame, so a long pause
// does not throw it off.
const maxStep = 100 * time.Millisecond

// advance moves the value to where it is at now, reporting whether it is
// done.
func (v *Value) advance(now time.Time) bool {
	if !v.spring {
		p := 1.0
		if v.duration > 0 {
			p = min(float64(now.Sub(v.start))/float64(v.duration), 1)
		}
		v.current = v.from + (v.target-v.from)*v.ease(p)
		return p >= 1
	}

	dt := min(now.Sub(v.last), maxStep)
	v.last = now
	if dt <= 0 {
		return false
	}
	v.current, v.velocity = springStep(v.current, v.velocity, v.target, dt.Seconds(), v.frequency, v.damping)
	return math.Abs(v.current-v.target) < 1e-3 && math.Abs(v.velocity) < 1e-2
}

// Presence animates something being shown and hidden, as a value from 0,
// hidden, to 1, shown. Keep showing it while [Presence.Visible] reports true,
// so it is still there to animate out after [Presence.Hide], and pass
// [Presence.Progress] to a transition.
type Presence struct {
	v    Value
	hide uint64 // the generation of the timer that hides it after ShowFor
}

// hideMsg hides a [Presence] shown with [Presence.ShowFor].
type hideMsg struct{ id, gen uint64 }

// NewPresence returns a hidden presence, configured as [NewValue] would be.
func NewPresence(opts ...Option) Presence {
	return Presence{v: NewValue(0, opts...)}
}

// Show starts animating in.
func (p *Presence) Show() tea.Cmd {
	p.hide++
	return p.v.To(1)
}

// ShowFor starts animating in, and out again once d has passed, such as for
// a notification. Showing or hiding it again in the meantime cancels hiding
// it after d, and calling ShowFor again restarts d.
func (p *Presence) ShowFor(d time.Duration) tea.Cmd {
	show := p.Show()
	msg := hideMsg{id: p.v.id, gen: p.hide}
	return tea.Batch(show, tea.Tick(d, func(time.Time) tea.Msg { return msg }))
}

// Hide starts animating out.
func (p *Presence) Hide() tea.Cmd {
	p.hide++
	return p.v.To(0)
}

// Toggle starts animating in, or out if shown.
func (p *Presence) Toggle() tea.Cmd {
	if p.Shown() {
		return p.Hide()
	}
	return p.Show()
}

// Update advances the animation, and hides it when the time given to
// [Presence.ShowFor] is up; see [Value.Update].
func (p *Presence) Update(msg tea.Msg) tea.Cmd {
	if h, ok := msg.(hideMsg); ok {
		if h.id != p.v.id || h.gen != p.hide {
			return nil
		}
		return p.Hide()
	}
	return p.v.Update(msg)
}

// Shown reports whether it is shown or animating in.
func (p *Presence) Shown() bool { return p.v.Target() == 1 }

// Visible reports whether any of it is showing: shown, or not yet done
// animating out. A spring that overshoots past hidden is still animating.
func (p *Presence) Visible() bool { return p.Shown() || p.v.Animating() || p.v.Float() > 0 }

// Animating reports whether it is animating in or out.
func (p *Presence) Animating() bool { return p.v.Animating() }

// Progress returns how far in it is, from 0 to 1.
func (p *Presence) Progress() float64 { return min(max(p.v.Float(), 0), 1) }

// Float returns how far in it is, without keeping it between 0 and 1, as a
// spring that overshoots goes past them, such as to bounce something past
// its place.
func (p *Presence) Float() float64 { return p.v.Float() }

// Animation is anything animated by the messages passed to its Update, as
// [Value], [Presence], and [Sequence] are.
type Animation interface {
	Update(msg tea.Msg) tea.Cmd
}

// Update passes msg to each of anims, returning their commands as one:
//
//	return m, tea.Batch(cmd, anim.Update(msg, &m.dialog, &m.toast, &m.sidebar))
func Update(msg tea.Msg, anims ...Animation) tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(anims))
	for _, a := range anims {
		if cmd := a.Update(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}
