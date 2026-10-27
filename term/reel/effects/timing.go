// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/lrstanley/x/term/reel"
)

// Timing is an option every effect that works on a [Region] takes, such as
// [Highlight]: [Between] limits it to a window of the timeline, and [Ease],
// [EaseIn], and [EaseOut] ease it in and out.
type Timing func(*timing)

// timing is when a region effect is on.
type timing struct {
	// from and to are the labels of the marks the window runs between, empty
	// for the start and end of the recording.
	from, to        string
	easeIn, easeOut time.Duration
}

// Between limits the effect to the part of the timeline from the first mark
// labeled from to the first mark labeled to after it, as
// [reel.Timeline.Between] cuts it: an empty from is the start of the
// recording, and an empty to is its end. Without Between, the effect covers
// the whole recording.
//
// For an [reel.Export], the marks are looked for when the effect starts, and
// a label that names no mark in the timeline is an error from Start,
// wrapping [reel.ErrNoMark]. A [reel.Record] runs while its timeline grows,
// so the marks are looked for as they are appended: the window opens when
// the from mark comes, and closes when the to mark does. A mark still
// missing when the recording stops is an error from
// [reel.Recording.Stop], wrapping [reel.ErrNoMark], which still writes the
// outputs. A from mark appended after the effect started was not known in
// advance, so [EaseIn] eases the effect in after it rather than before, so
// it is at full strength one easing after the mark.
func Between(from, to string) Timing {
	return func(t *timing) { t.from, t.to = from, to }
}

// Ease eases the effect in over d of playback before the start of its
// window (see [Between]), and out over d after its end, on the delay grid
// ([reel.Context.Step]), so the effect is at full strength for the whole
// window. Easing splits the frames it covers into a frame per step, each a
// little further along an ease-in-out curve. Zero, the default, switches the
// effect on and off at once. A d below zero is an error from Start. A window
// that starts with the recording, or ends with it, has nothing to ease there,
// and one whose start was not known in advance eases in after it (see
// [Between]). Ease(d) is [EaseIn](d) and [EaseOut](d), and a later option
// replaces either.
func Ease(d time.Duration) Timing {
	return func(t *timing) { t.easeIn, t.easeOut = d, d }
}

// EaseIn is [Ease] for the start of the window only: it eases the effect in
// over d, and leaves how it eases out alone.
func EaseIn(d time.Duration) Timing {
	return func(t *timing) { t.easeIn = d }
}

// EaseOut is [Ease] for the end of the window only: it eases the effect out
// over d, and leaves how it eases in alone.
func EaseOut(d time.Duration) Timing {
	return func(t *timing) { t.easeOut = d }
}

// eases reports whether t eases the effect in or out.
func (t timing) eases() bool { return t.easeIn != 0 || t.easeOut != 0 }

func (o Timing) applyHighlight(h *highlight) {
	if o != nil {
		o(&h.timing)
	}
}

// openFrom and openTo are the offsets of the ends of a window that has
// none.
const (
	openFrom = time.Duration(math.MinInt64)
	openTo   = time.Duration(math.MaxInt64)
)

// coverage is how much of its window an effect must cover.
type coverage int

const (
	// easeAfterMark gives each step of the delay grid the strength at its
	// middle, and eases in after a from mark appended after the effect
	// started, in a live recording ([reel.Context.Live]), whose easing
	// cannot play before a mark nobody knew the time of.
	easeAfterMark coverage = iota
	// coverWindow gives every step that shows any of the window full
	// strength, and starts at full strength on a late from mark, for an
	// effect that must never leave part of its window uncovered, such as
	// one that hides secrets.
	coverWindow
)

// window is a [timing] resolved for one recording: the offsets into the
// timeline its effect is on between, with the easings in timeline time, and
// the recording's delay grid, speed, and hold. In a live recording, the
// marks not yet appended are looked for by [window.update].
type window struct {
	from, to time.Duration
	// easeIn is how long the effect eases in before from, or after it when
	// late, and easeOut how long it eases out after to.
	easeIn, easeOut time.Duration
	step            time.Duration
	speed           float64
	hold            time.Duration
	// cover is set to cover every step that shows any of the window.
	cover bool
	// late is set when the from mark was appended after the effect
	// started.
	late bool

	// from and to label a live window's marks, and wantFrom and wantTo are
	// set while they are still looked for in tl, of whose events read have
	// been looked at.
	fromLabel, toLabel string
	wantFrom, wantTo   bool
	tl                 *reel.Timeline
	read               int
}

// start resolves t for the recording of ctx. On a fixed timeline it finds
// the marks of t, which must be there. On a live one, it finds the marks
// appended so far, and registers a check with [reel.Context.OnClose] that
// reports those that never came. cov is how much of the window the effect
// must cover.
func (t timing) start(ctx *reel.Context, cov coverage) (*window, error) {
	switch {
	case t.easeIn < 0:
		return nil, fmt.Errorf("effects: ease in %v is below zero", t.easeIn)
	case t.easeOut < 0:
		return nil, fmt.Errorf("effects: ease out %v is below zero", t.easeOut)
	}
	w := &window{
		from:    openFrom,
		to:      openTo,
		easeIn:  time.Duration(float64(t.easeIn) * ctx.Speed),
		easeOut: time.Duration(float64(t.easeOut) * ctx.Speed),
		step:    ctx.Step,
		speed:   ctx.Speed,
		hold:    ctx.Hold,
		cover:   cov == coverWindow,
	}
	if t.from == "" && t.to == "" {
		return w, nil
	}
	if ctx.Live {
		w.fromLabel, w.toLabel, w.tl = t.from, t.to, ctx.Timeline
		w.wantFrom, w.wantTo = t.from != "", t.to != ""
		if w.wantFrom {
			// Off until the mark comes.
			w.from = openTo
		}
		w.update()
		if w.wantFrom {
			w.late = true
			if w.cover {
				w.easeIn = 0
			}
		}
		ctx.OnClose(w.missing)
		return w, nil
	}
	cut, err := ctx.Timeline.Between(t.from, t.to)
	if err != nil {
		if errors.Is(err, reel.ErrNoMark) {
			return nil, fmt.Errorf("effects: between %q and %q: %w", t.from, t.to, err)
		}
		return nil, err
	}
	from, to := cut.Window()
	if t.from != "" {
		w.from = from
	}
	if t.to != "" {
		w.to = to
	}
	return w, nil
}

// update looks for the marks of a live window among the events appended
// since it last looked: the first mark labeled from, and then the first
// labeled to after it, as [reel.Timeline.Between] finds them. A mark found
// past the frame given last changes nothing until a frame reaches it.
func (w *window) update() {
	if !w.wantFrom && !w.wantTo {
		return
	}
	for ev := range w.tl.EventsFrom(w.read) {
		w.read++
		m, ok := ev.(reel.Mark)
		switch {
		case !ok:
		case w.wantFrom:
			if m.Label == w.fromLabel {
				w.from, w.wantFrom = m.At(), false
			}
		case m.Label == w.toLabel:
			w.to, w.wantTo = m.At(), false
		}
		if !w.wantFrom && !w.wantTo {
			return
		}
	}
}

// missing returns an error wrapping [reel.ErrNoMark] for the marks of a live
// window that the recording never had, or nil. It looks once more, for the
// marks appended after the last frame.
func (w *window) missing() error {
	w.update()
	switch {
	case w.wantFrom:
		return fmt.Errorf("effects: between %q and %q: %w: %q", w.fromLabel, w.toLabel, reel.ErrNoMark, w.fromLabel)
	case w.wantTo:
		return fmt.Errorf("effects: between %q and %q: %w: %q after the start of the window", w.fromLabel, w.toLabel, reel.ErrNoMark, w.toLabel)
	}
	return nil
}

// level is the strength of the effect at offset at: 1 in the window, 0 well
// outside it, and in between on the easing curve. A late window is off
// before its start, and eases in after it.
func (w *window) level(at time.Duration) float64 {
	if at < w.from {
		if w.late {
			return 0
		}
		return eased(w.from-at, w.easeIn)
	}
	lv := 1.0
	if w.late {
		lv = 1 - eased(at-w.from, w.easeIn)
	}
	if at > w.to {
		lv *= eased(at-w.to, w.easeOut)
	}
	return lv
}

// eased is the strength at a distance d outside the edge of a window eased
// over ease: 1 at the edge, falling to 0 at ease away.
func eased(d, ease time.Duration) float64 {
	if d >= ease {
		return 0
	}
	x := 1 - float64(d)/float64(ease)
	return x * x * (3 - 2*x)
}

// piece is a stretch of a frame, from its start to the start plus delay in
// playback time, where the effect has one strength.
type piece struct {
	start, delay time.Duration
	level        float64
}

// pieces splits f on the delay grid where the effect's strength changes.
// Each step has the strength at its middle, or for a window that must be
// covered, the strength at its point nearest the window, so a step that
// shows any of the window is at full strength. The last piece takes what is
// left of f past its last step. The hold of the last frame is past the end
// of the timeline, so it has the strength at that end. In a live recording,
// the marks appended since the frame before are looked for first.
func (w *window) pieces(f reel.Frame) []piece {
	steps := max(1, int(f.Delay/w.step))
	span := f.Delay
	if f.Last {
		span = max(0, span-w.hold)
	}
	end := f.At + time.Duration(float64(span)*w.speed)
	w.update()
	stepAt := func(k float64) time.Duration {
		return min(f.At+time.Duration(k*float64(w.step)*w.speed), end)
	}
	var out []piece
	for k := range steps {
		start, delay := time.Duration(k)*w.step, w.step
		at := stepAt(float64(k) + 0.5)
		if k == steps-1 {
			delay = f.Delay - start
		}
		if w.cover {
			last := stepAt(float64(k + 1))
			if k == steps-1 {
				last = end
			}
			at = min(max(w.from, stepAt(float64(k))), last)
		}
		lv := w.level(at)
		if n := len(out); n > 0 && out[n-1].level == lv {
			out[n-1].delay += delay
			continue
		}
		out = append(out, piece{start: start, delay: delay, level: lv})
	}
	return out
}
