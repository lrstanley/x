// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"time"

	"github.com/lrstanley/x/term/reel"
)

// Outro returns an effect that shows a card of lines for d after the last
// frame (see [CardRenderer.Card]). Without lines, or with a d of zero or
// less, there is no card. The speed does not scale d. See [OutroFunc] for
// lines that depend on the grid, and [HoldLast] to hold the last screen
// before the card.
func Outro(d time.Duration, lines ...Line) reel.Effect {
	return OutroFunc(d, func(int, int) []Line { return lines })
}

// OutroFunc is [Outro] with the card's lines from fn, given the grid the
// card is laid out on (see [CardRenderer.Grid]), such as to list only as
// much as fits. fn returning no lines is no card.
func OutroFunc(d time.Duration, fn func(cols, rows int) []Line) reel.Effect {
	return outro{d: d, lines: fn}
}

type outro struct {
	d     time.Duration
	lines func(cols, rows int) []Line
}

func (e outro) Start(ctx *reel.Context) (reel.Stage, error) {
	if e.d <= 0 || e.lines == nil {
		return nil, nil //nolint:nilnil // A nil stage leaves the recording alone.
	}
	cards, err := NewCardRenderer(ctx)
	if err != nil {
		return nil, err
	}
	return func(f reel.Frame, emit func(reel.Frame)) {
		emit(f)
		if !f.Last {
			return
		}
		if lines := e.lines(cards.Grid(f.Cells)); len(lines) > 0 {
			emit(reel.Frame{Image: cards.Card(f.Cells, lines...), Delay: e.d, Isolated: true})
		}
	}, nil
}

// HoldLast returns an effect that plays the last frame of the terminal for
// at least d, such as so a failing screen can be read. A last frame that is
// not the terminal's, such as a card, is left alone, so list HoldLast before
// [Outro] to hold the screen before the card. The speed does not scale d.
func HoldLast(d time.Duration) reel.Effect { return holdLast(d) }

type holdLast time.Duration

func (e holdLast) Start(*reel.Context) (reel.Stage, error) {
	if e <= 0 {
		return nil, nil //nolint:nilnil // A nil stage leaves the recording alone.
	}
	return func(f reel.Frame, emit func(reel.Frame)) {
		if f.Last && f.Cells != nil {
			f.Delay = max(f.Delay, time.Duration(e))
		}
		emit(f)
	}, nil
}
