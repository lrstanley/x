// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/vt/screen"
)

// Intro returns an effect that shows a card of lines for d before the first
// frame (see [CardRenderer.Card]), sized like the first frame. Without lines,
// the card is the recording's title ([reel.Context.Title]) in bold, and a
// recording without a title gets no card, so one profile can introduce the
// recordings that have titles. A d of zero or less is no card either. The
// speed does not scale d.
//
// The card is palettized on its own GIF palette ([reel.Frame.Isolated]), and
// has no terminal ([reel.Frame.Cells] is nil), so effects after it, such as
// [Keys], leave it alone.
func Intro(d time.Duration, lines ...Line) reel.Effect {
	return intro{d: d, lines: lines}
}

type intro struct {
	d     time.Duration
	lines []Line
}

func (e intro) Start(ctx *reel.Context) (reel.Stage, error) {
	lines := e.lines
	if len(lines) == 0 && ctx.Title != "" {
		lines = []Line{{Text: ctx.Title, Style: screen.Style{Attrs: screen.AttrBold}}}
	}
	if e.d <= 0 || len(lines) == 0 {
		return nil, nil //nolint:nilnil // A nil stage leaves the recording alone.
	}
	cards, err := NewCardRenderer(ctx)
	if err != nil {
		return nil, err
	}
	return func(f reel.Frame, emit func(reel.Frame)) {
		if f.First {
			emit(reel.Frame{Image: cards.Card(f.Cells, lines...), Delay: e.d, Isolated: true})
		}
		emit(f)
	}, nil
}
