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
// recording. With lines, the card shows them. Without, it shows the
// *[Result] in [reel.Context.Meta], which a [Harness] export sets: PASS or
// FAIL, the test name, and the first line of as many failures as fit. Without
// lines or a *Result, or with a d of zero or less, it shows nothing. Playback
// speed does not scale d.
//
// For a failed test, the last frame before the card plays for at least 3s, so
// the failing screen can be read. List [effects.Pause] before Outro to badge
// that frame.
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

// failureHold is the minimum duration of a failed test's last frame.
const failureHold = 3 * time.Second

var (
	passStyle = vt.Style{Fg: vt.Indexed(2), Attrs: vt.AttrBold}
	failStyle = vt.Style{Fg: vt.Indexed(1), Attrs: vt.AttrBold}
	faint     = vt.Style{Attrs: vt.AttrFaint}
)

// resultLines are the default outro card lines for a cols by rows grid.
func resultLines(r *Result, cols, rows int) []effects.Line {
	if !r.Failed {
		return []effects.Line{{Text: "PASS", Style: passStyle}, {Text: r.Name, Style: faint}}
	}
	lines := []effects.Line{{Text: "FAIL", Style: failStyle}, {Text: r.Name, Style: faint}}
	return append(lines, failureLines(r.Failures, cols, rows)...)
}

// failureLines lists failures for the FAIL card on a cols by rows grid. Only
// the first line of each is shown; the rest is usually a screen dump the
// recording already shows. Lines are padded to one width so the block stays
// left-aligned when each line is centered.
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
			need++ // Room for "+N more".
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
