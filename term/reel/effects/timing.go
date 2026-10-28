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

// Timing is an option the effects that work on a [Region] take: [Between]
// limits the effect to a window of the timeline, and [Ease], [EaseIn], and
// [EaseOut] ease it in and out.
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
// [reel.Timeline.Between] cuts it. An empty from is the start of the
// recording, and an empty to is its end. Without Between, the effect covers
// the whole recording.
//
// Without its from mark the effect never comes on, and without its to mark
// it stays on to the end, except that an effect that hides what it covers,
// such as [Redact], covers the whole of an export without its from mark. A
// missing mark is reported as a [reel.NoMarkError] from [reel.Export] or
// [reel.Recording.Stop], which still write the outputs. A [reel.Record]
// looks for the marks as they are appended, so [EaseIn] eases in after a
// from mark rather than before it.
func Between(from, to string) Timing {
	return func(t *timing) { t.from, t.to = from, to }
}

// Ease eases the effect in over d of playback before the start of its
// window, and out over d after its end, so it is at full strength for the
// whole window. Easing adds a frame per step of the delay grid
// ([reel.Context.Step]) it covers. Zero, the default, switches the effect on
// and off at once; a d below zero is an error from Start. Ease(d) is
// [EaseIn](d) and [EaseOut](d), and a later option replaces either.
func Ease(d time.Duration) Timing {
	return func(t *timing) { t.easeIn, t.easeOut = d, d }
}

// EaseIn is [Ease] for the start of the window only.
func EaseIn(d time.Duration) Timing {
	return func(t *timing) { t.easeIn = d }
}

// EaseOut is [Ease] for the end of the window only.
func EaseOut(d time.Duration) Timing {
	return func(t *timing) { t.easeOut = d }
}

func (o Timing) applyHighlight(h *highlight) {
	if o != nil {
		o(&h.timing)
	}
}

// openFrom and openTo are the offsets of a window's missing ends.
const (
	openFrom = time.Duration(math.MinInt64)
	openTo   = time.Duration(math.MaxInt64)
)

// delayOffset is the offset into the delay of f, a frame that is not
// paused, of offset at into the timeline, on the delay grid of ctx, from 0
// to f's delay. at may be openFrom or openTo.
func delayOffset(ctx *reel.Context, f reel.Frame, at time.Duration) time.Duration {
	if at <= f.At {
		return 0
	}
	if at == openTo {
		return f.Delay
	}
	return min(time.Duration(float64(at-f.At)/ctx.Speed).Round(ctx.Step), f.Delay)
}

// coverage is how much of its window an effect must cover.
type coverage int

const (
	// easeAfterMark gives each step of the delay grid the strength at its
	// middle. A live from mark is not known in advance, so it eases in
	// after it.
	easeAfterMark coverage = iota
	// coverWindow gives every step that shows any of the window full
	// strength, for an effect that hides secrets. It cannot ease.
	coverWindow
)

// window is a [timing] resolved for one recording, with offsets and easings
// in timeline time.
type window struct {
	// from is past the from mark by lag, set when the mark was appended
	// after the effect started, so the effect eases in after the mark.
	from, to        time.Duration
	lag             time.Duration
	easeIn, easeOut time.Duration
	step            time.Duration
	speed           float64
	// cover is set for [coverWindow].
	cover bool

	// fromLabel and toLabel are a live window's marks, and wantFrom and
	// wantTo are set while they are still looked for. read counts the
	// events of tl already looked at.
	fromLabel, toLabel string
	wantFrom, wantTo   bool
	tl                 *reel.Timeline
	read               int
}

// start resolves t for the recording of ctx. On a live timeline, marks not
// yet appended are reported missing at [reel.Context.OnClose].
func (t timing) start(ctx *reel.Context, cov coverage) (*window, error) {
	switch {
	case t.easeIn < 0:
		return nil, fmt.Errorf("effects: ease in %v is below zero", t.easeIn)
	case t.easeOut < 0:
		return nil, fmt.Errorf("effects: ease out %v is below zero", t.easeOut)
	case cov == coverWindow && (t.easeIn != 0 || t.easeOut != 0):
		return nil, errors.New("effects: an effect that must cover its window cannot ease: easing would leave part of it uncovered")
	}
	w := &window{
		from:    openFrom,
		to:      openTo,
		easeIn:  time.Duration(float64(t.easeIn) * ctx.Speed),
		easeOut: time.Duration(float64(t.easeOut) * ctx.Speed),
		step:    ctx.Step,
		speed:   ctx.Speed,
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
			w.lag = w.easeIn
		}
		ctx.OnClose(w.missing)
		return w, nil
	}
	cut, err := t.between(ctx.Timeline)
	if errors.Is(err, reel.ErrNoMark) {
		// The recording never reached a mark, which is reported as it
		// closes (err is reassigned below). Without its to mark, the effect
		// stays on to the end, and without its from mark, it never comes
		// on, as in a live window whose mark never comes, but for an effect
		// that must cover its window: where the window would have started
		// is not known, so it covers the whole recording.
		missing := err
		ctx.OnClose(func() error { return missing })
		if cut, err = ctx.Timeline.Between(t.from, ""); errors.Is(err, reel.ErrNoMark) {
			if !w.cover {
				w.from = openTo
			}
			return w, nil
		}
		t.to = ""
	}
	if err != nil {
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

// between returns the part of tl the window of t runs over, with a
// [reel.NoMarkError] for a mark tl's window lacks.
func (t timing) between(tl *reel.Timeline) (*reel.Timeline, error) {
	cut, err := tl.Between(t.from, t.to)
	if errors.Is(err, reel.ErrNoMark) {
		return nil, fmt.Errorf("effects: between %q and %q: %w", t.from, t.to, err)
	}
	return cut, err
}

// checkMarks is [reel.MarkChecker.CheckMarks] for an effect with timing t:
// the marks of [Between] must be in tl's window.
func (t timing) checkMarks(tl *reel.Timeline) error {
	if t.from == "" && t.to == "" {
		return nil
	}
	_, err := t.between(tl)
	return err
}

// update looks for the marks of a live window among the events appended
// since it last looked, as [reel.Timeline.Between] finds them.
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
				w.from, w.wantFrom = m.At()+w.lag, false
			}
		case m.Label == w.toLabel:
			w.to, w.wantTo = m.At(), false
		}
		if !w.wantFrom && !w.wantTo {
			return
		}
	}
}

// missing returns a [reel.NoMarkError] for each mark of a live window the
// recording never had, after looking once more for marks appended after the
// last frame.
func (w *window) missing() error {
	w.update()
	switch {
	case w.wantFrom:
		return fmt.Errorf("effects: between %q and %q: %w", w.fromLabel, w.toLabel, &reel.NoMarkError{Label: w.fromLabel})
	case w.wantTo:
		return fmt.Errorf("effects: between %q and %q: %w after the start of the window", w.fromLabel, w.toLabel, &reel.NoMarkError{Label: w.toLabel})
	}
	return nil
}

// level is the strength of the effect at offset at, from 0 to 1.
func (w *window) level(at time.Duration) float64 {
	lv := 1.0
	if at < w.from {
		lv = eased(w.from-at, w.easeIn)
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

// piece is a stretch of a frame, in playback time, where the effect has one
// strength.
type piece struct {
	start, delay time.Duration
	level        float64
}

// pieces splits f on the delay grid where the effect's strength changes
// (see [coverage]). The last piece takes what is left of f past its last
// step. A paused frame ([reel.Frame.Paused]) shows one moment, so it is one
// piece.
func (w *window) pieces(f reel.Frame) []piece {
	w.update()
	if f.Paused {
		return []piece{{delay: f.Delay, level: w.level(f.At)}}
	}
	steps := max(1, int(f.Delay/w.step))
	end := f.At + time.Duration(float64(f.Delay)*w.speed)
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
