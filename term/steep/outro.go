// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/effects"
	"github.com/lrstanley/x/term/reel/vt"
	"github.com/lrstanley/x/term/steep/internal/xansi"
	"github.com/rivo/uniseg"
)

// Outro returns an effect that shows a card for d after the last frame of a
// recording (see [effects.CardRenderer.Card]). With lines, the card shows
// them. Without, it shows the test's result from the *[Result] in
// [reel.Context.Meta], which an export from a [Harness] sets: PASS or FAIL
// and the test name, and for a failure, the summary line of each failure the
// harness reported, as many as fit, with a count of the rest. A recording
// whose Meta is not a *Result, such as one exported without a harness, gets
// no card without lines. A d of zero or less is no card either. The speed
// does not scale d.
//
// For a failed test, Outro also makes the last frame before the card play
// for at least 3s, so a viewer has time to read the screen the test failed
// on. List [effects.Pause] before Outro, so that frame is its badged end
// pause.
func Outro(d time.Duration, lines ...effects.Line) reel.Effect {
	return outro{d: d, lines: lines}
}

type outro struct {
	d     time.Duration
	lines []effects.Line
}

func (e outro) Start(ctx *reel.Context) (reel.Stage, error) {
	result, _ := ctx.Meta.(*Result)
	if e.d <= 0 || (len(e.lines) == 0 && result == nil) {
		return nil, nil //nolint:nilnil // A nil stage leaves the recording alone.
	}
	cards, err := effects.NewCardRenderer(ctx)
	if err != nil {
		return nil, err
	}
	failed := result != nil && result.Failed
	return func(f reel.Frame, emit func(reel.Frame)) {
		if !f.Last {
			emit(f)
			return
		}
		if failed && f.Cells != nil {
			f.Delay = max(f.Delay, failureHold)
		}
		emit(f)
		lines := e.lines
		if len(lines) == 0 {
			cols, rows := cards.Grid(f.Cells)
			lines = resultLines(result, cols, rows)
		}
		emit(reel.Frame{Image: cards.Card(f.Cells, lines...), Delay: e.d, Isolated: true})
	}, nil
}

// failureHold is the least a failed test's last terminal frame plays, before
// the FAIL card.
const failureHold = 3 * time.Second

var (
	passStyle = vt.Style{Fg: vt.Indexed(2), Attrs: vt.AttrBold}
	failStyle = vt.Style{Fg: vt.Indexed(1), Attrs: vt.AttrBold}
	faint     = vt.Style{Attrs: vt.AttrFaint}
)

// resultLines are the default outro card for a card grid of cols by rows:
// PASS or FAIL, the test name, and for a failure, what failed.
func resultLines(r *Result, cols, rows int) []effects.Line {
	if !r.Failed {
		return []effects.Line{{Text: "PASS", Style: passStyle}, {Text: r.Name, Style: faint}}
	}
	lines := []effects.Line{{Text: "FAIL", Style: failStyle}, {Text: r.Name, Style: faint}}
	return append(lines, failureLines(r.Failures, cols, rows)...)
}

// failureLines lists failures for the FAIL card, below its two title lines,
// on a card grid of cols by rows. Each failure shows only its first line, its
// summary; the rest is usually a dump of the screen the recording already
// shows. Summaries are wrapped to the grid's width, cut to the rows left with
// a count of the rest, and padded to one width so the block is left-aligned
// when each line is centered.
func failureLines(failures []string, cols, rows int) []effects.Line {
	// Leave rows for the two title lines, a spacer, and a margin above and below.
	room := rows - 5
	width := max(cols-6, 10)

	var msg []string
	shown := 0
	for _, failure := range failures {
		summary, _, _ := strings.Cut(strings.TrimSpace(xansi.StripANSI(failure)), "\n")
		wrapped := strings.Split(ansi.Wrap(strings.ReplaceAll(summary, "\t", " "), width, ""), "\n")
		need := len(msg) + len(wrapped)
		if shown < len(failures)-1 {
			need++ // Room to say how many more there are, if the next one does not fit.
		}
		if need > room {
			break
		}
		for i, line := range wrapped {
			prefix := "  "
			if i == 0 {
				prefix = "• "
			}
			msg = append(msg, prefix+strings.TrimRight(line, " "))
		}
		shown++
	}
	if shown == 0 {
		return nil
	}

	var block int
	for _, line := range msg {
		block = max(block, uniseg.StringWidth(line))
	}
	lines := []effects.Line{{}}
	for _, line := range msg {
		lines = append(lines, effects.Line{Text: line + strings.Repeat(" ", block-uniseg.StringWidth(line))})
	}
	if more := len(failures) - shown; more > 0 {
		lines = append(lines, effects.Line{Text: fmt.Sprintf("+%d more", more), Style: faint})
	}
	return lines
}
