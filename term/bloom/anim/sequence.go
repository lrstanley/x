// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package anim

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

type stepKind uint8

const (
	stepTo stepKind = iota
	stepHold
	stepJump
)

// Step is one part of a [Sequence]: moving to a target, holding still, or
// jumping. Build one with [To], [Hold], or [Jump].
type Step struct {
	kind   stepKind
	target float64
	hold   time.Duration
	opts   []Option
}

// To is a step that animates to target, from wherever the value is, ending
// when it gets there. Options such as [Duration], [Ease], and [Spring]
// override the sequence's for this step only.
func To(target float64, opts ...Option) Step {
	return Step{kind: stepTo, target: target, opts: opts}
}

// Hold is a step that leaves the value as it is for d.
func Hold(d time.Duration) Step { return Step{kind: stepHold, hold: max(d, 0)} }

// Jump is a step that sets the value to target without animating, as to
// restart a looping sequence from its start.
func Jump(target float64) Step { return Step{kind: stepJump, target: target} }

// Sequence is a number that runs through a list of steps, one after the
// other: animating, holding, and jumping. It replaces chaining animations
// with timers, and a step starts when the one before it was due to end rather
// than when its frame happened to arrive, so a sequence does not drift.
//
// Use it like a [Value]: return the command from [Sequence.Play] or
// [Sequence.Loop], and pass every message to Update. The zero Sequence has
// no steps; make one with [NewSequence].
type Sequence struct { //nolint:recvcheck // With configures a sequence before it is used, as a builder.
	v    Value // the value being animated, and its id and generation
	base Value // the options steps start from
	// steps is shared between copies of a sequence, and never changed.
	steps []Step

	step    int       // the current step, while running
	at      time.Time // when the current step was due to start
	cycle   time.Time // when this run through the steps started
	running bool
	loop    bool
}

// NewSequence returns a stopped sequence at start that runs steps. Its steps
// default to [NewValue]'s options; change them with [Sequence.With].
func NewSequence(start float64, steps ...Step) Sequence {
	v := NewValue(start)
	return Sequence{v: v, base: v, steps: steps, step: -1}
}

// With sets the options steps start from, such as [Duration] and [Ease], and
// [FPS] and [Clock] for the whole sequence. It returns the changed sequence,
// to be used before it is played.
func (s Sequence) With(opts ...Option) Sequence {
	for _, opt := range opts {
		opt(&s.base)
	}
	s.v.interval, s.v.now = s.base.interval, s.base.now
	return s
}

// NewLoop returns a sequence that runs from 0 to 1 at an even pace over
// period, and then from 0 again, such as the phase of a shimmer or of
// scrolling text. Start it with [Sequence.Loop]; opts are as for
// [Sequence.With].
func NewLoop(period time.Duration, opts ...Option) Sequence {
	return NewSequence(0, Jump(0), To(1, Duration(period), Ease(Linear))).With(opts...)
}

// Float returns the current value.
func (s *Sequence) Float() float64 { return s.v.current }

// Int returns the current value, rounded.
func (s *Sequence) Int() int { return s.v.Int() }

// Animating reports whether the sequence is running, including while it
// holds still.
func (s *Sequence) Animating() bool { return s.running }

// Step returns the index of the step being run, or -1 when stopped.
func (s *Sequence) Step() int {
	if !s.running {
		return -1
	}
	return s.step
}

// Play runs the steps once, from the first, restarting if already running.
// It starts from the current value, so begin with [Jump] to start elsewhere.
// It returns the command for the first frame, or nil if there is nothing to
// wait for.
func (s *Sequence) Play() tea.Cmd { return s.start(false) }

// Loop runs the steps over and over, until [Sequence.Stop] or another call
// to Play or Loop. A run through the steps that takes no time ends the loop.
func (s *Sequence) Loop() tea.Cmd { return s.start(true) }

// Stop stops the sequence where it is.
func (s *Sequence) Stop() {
	s.running, s.step = false, -1
	s.v.velocity = 0
	s.v.gen++
}

// Update advances the sequence for its own [FrameMsg], returning the command
// for the next one while it is running.
func (s *Sequence) Update(msg tea.Msg) tea.Cmd {
	f, ok := msg.(FrameMsg)
	if !ok || f.id != s.v.id || f.gen != s.v.gen || !s.running {
		return nil
	}
	return s.advance()
}

func (s *Sequence) start(loop bool) tea.Cmd {
	s.v.gen++
	if len(s.steps) == 0 || s.v.now == nil {
		s.running, s.step = false, -1
		return nil
	}
	s.running, s.loop, s.step = true, loop, 0
	s.at = s.v.now()
	s.cycle = s.at
	s.enter()
	return s.advance()
}

// advance runs the steps due by now, returning the command for the next
// frame, or nil if the sequence is done.
func (s *Sequence) advance() tea.Cmd {
	now := s.v.now()
	for s.running {
		end, done := s.done(now)
		if !done {
			break
		}
		s.at = end
		s.step++
		if s.step == len(s.steps) {
			if !s.loop || !s.at.After(s.cycle) {
				s.Stop()
				break
			}
			// Every run through the steps ends the same way, so whole runs
			// missed, as while the program was suspended, are skipped rather
			// than replayed.
			if run, missed := s.at.Sub(s.cycle), now.Sub(s.at); missed >= run {
				s.at = s.at.Add(missed - missed%run)
			}
			s.step, s.cycle = 0, s.at
		}
		s.enter()
	}
	if !s.running {
		return nil
	}
	st := s.steps[s.step]
	if st.kind != stepHold {
		return s.v.frame()
	}
	// Nothing changes until the hold ends: wait for exactly that.
	msg := FrameMsg{id: s.v.id, gen: s.v.gen}
	return tea.Tick(max(s.at.Add(st.hold).Sub(now), time.Millisecond), func(time.Time) tea.Msg { return msg })
}

// enter starts the current step, due to start at s.at.
func (s *Sequence) enter() {
	st := s.steps[s.step]
	switch st.kind {
	case stepJump:
		s.v.current, s.v.target, s.v.velocity = st.target, st.target, 0
	case stepTo:
		cfg := s.base
		for _, opt := range st.opts {
			opt(&cfg)
		}
		s.v.duration, s.v.ease = cfg.duration, cfg.ease
		s.v.spring, s.v.frequency, s.v.damping = cfg.spring, cfg.frequency, cfg.damping
		s.v.from, s.v.target, s.v.start, s.v.last = s.v.current, st.target, s.at, s.at
	case stepHold:
	}
}

// done moves the current step to where it is at now, returning when it ended
// if it has.
func (s *Sequence) done(now time.Time) (end time.Time, done bool) {
	switch st := s.steps[s.step]; st.kind {
	case stepHold:
		end = s.at.Add(st.hold)
		return end, !now.Before(end)
	case stepTo:
		if !s.v.advance(now) {
			return time.Time{}, false
		}
		s.v.current, s.v.velocity = s.v.target, 0
		if s.v.spring {
			// A spring ends when it settles, not at a time known in advance.
			return now, true
		}
		return s.at.Add(s.v.duration), true
	case stepJump:
	}
	return s.at, true
}
