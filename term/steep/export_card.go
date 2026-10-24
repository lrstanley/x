// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"fmt"
	"image"
	"image/draw"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/steep/internal/xansi"
	"github.com/lrstanley/x/term/still"
	"github.com/lrstanley/x/term/vt/screen"
	"github.com/rivo/uniseg"
)

// CardLine is one line of text on a title card.
type CardLine struct {
	Text  string
	Style screen.Style
}

// Card renders a title card: lines centered on an otherwise blank terminal,
// drawn with the harness renderer at the same pixel size and window chrome as
// the frame being mutated, so it can sit beside that frame in a recording.
// Cards use the renderer's default colors (see [WithRenderer]), not ones the
// program set, so every card in a recording looks the same. On terminals
// larger than 80x24, the text is drawn larger than the terminal font, so it
// stays readable when the recording is scaled down to fit a page. Text that
// does not fit is cut off. The image is owned by the caller.
func (ec ExportContext) Card(lines ...CardLine) image.Image {
	state := ec.cardState()
	ec.h.imageMu.Lock()
	card := copyDrawnLocked(ec.h, screen.NewBuffer(ec.src.cols, ec.src.rows), state, time.Time{})
	ec.h.imageMu.Unlock()

	if text := ec.cardText(state, lines); text != nil {
		b, tb := card.Bounds(), text.Bounds()
		at := b.Min.Add(image.Pt((b.Dx()-tb.Dx())/2, (b.Dy()-tb.Dy())/2))
		draw.Draw(card, tb.Sub(tb.Min).Add(at), text, tb.Min, draw.Src)
	}
	return card
}

// cardState is the renderer state for cards: the frame's window state with
// default colors, no cursor, no progress or program status, and nothing to
// scroll.
func (ec ExportContext) cardState() still.EmulatorState {
	state := viewportState(ec.h, still.EmulatorState{}, ec.src.state)
	state.FgColor, state.BgColor = nil, nil
	state.CursorVisible = false
	state.ScrollbackCount = 0
	state.Progress = still.Progress{}
	state.ProgramStatuses = nil
	return state
}

// cardScale is how much larger card text is than the terminal font. It grows
// at three quarters the rate the terminal grows past 80x24, so text on a large
// terminal stays readable without filling the card.
func (ec ExportContext) cardScale() float64 {
	grown := min(float64(ec.src.cols)/DefaultTermWidth, float64(ec.src.rows)/DefaultTermHeight)
	return max(1, 1+(grown-1)*3/4)
}

// cardGrid is the number of columns and rows of card text that fit on the
// card at [ExportContext.cardScale].
func (ec ExportContext) cardGrid() (cols, rows int) {
	scale := ec.cardScale()
	return int(float64(ec.src.cols) / scale), int(float64(ec.src.rows) / scale)
}

// cardText renders lines, each centered, as a block of text without window
// chrome, at [ExportContext.cardScale] and cut to [ExportContext.cardGrid].
// It returns nil when there is nothing to draw.
func (ec ExportContext) cardText(state still.EmulatorState, lines []CardLine) image.Image {
	cols, rows := ec.cardGrid()
	var width int
	for _, line := range lines {
		width = max(width, uniseg.StringWidth(line.Text))
	}
	width, height := min(width, cols), min(len(lines), rows)
	if width == 0 || height == 0 {
		return nil
	}
	buf := screen.NewBuffer(width, height)
	for i, line := range lines[:height] {
		writeCentered(buf, i, line)
	}

	return ec.cards.draw(ec.h.tb, ec.h.imageRenderer.Metrics().Scale*ec.cardScale(), state, buf)
}

// cardRenderer draws card text with the harness renderer's options, without
// window chrome, at a scale that follows the card scale. A recording owns
// one; it is built on first use, rebuilt when the size changes, and closed
// when the recording finishes.
type cardRenderer struct {
	opts  []still.Option
	r     *still.Renderer
	scale float64
	// failed is set when building the renderer fails, so the error is logged
	// once and later cards are drawn without text.
	failed bool
}

// draw renders scr at scale with state and returns an owned image, or nil when
// the renderer cannot be built.
func (c *cardRenderer) draw(tb testing.TB, scale float64, state still.EmulatorState, scr screen.Screen) image.Image {
	if c.failed {
		return nil
	}
	if c.r == nil || c.scale != scale {
		r, err := still.New(append(slices.Clip(c.opts),
			still.WithScale(scale),
			still.WithPadding(0),
			still.WithMargin(0, nil),
			still.WithBorderRadius(0),
			still.WithScrollbar(false),
			still.WithWindowChrome(false),
		)...)
		if err != nil {
			c.failed = true
			tb.Logf("export: drawing cards without text: %v", err)
			return nil
		}
		if cerr := c.close(); cerr != nil {
			tb.Logf("export: closing card renderer: %v", cerr)
		}
		c.r, c.scale = r, scale
	}
	c.r.UpdateEmulatorState(&state)
	return cloneNRGBA(c.r.Draw(scr))
}

func (c *cardRenderer) close() error {
	if c.r == nil {
		return nil
	}
	r := c.r
	c.r = nil
	return r.Close()
}

// Failures returns the failure messages the harness has reported so far,
// oldest first: failed assertions, waits, and snapshots, and harness errors
// such as timeouts. Failures a test reports itself, through its own
// [testing.TB], are not included.
func (ec ExportContext) Failures() []string {
	return ec.h.failures.messages()
}

// failureLines lists failures for the default FAIL card, below its title
// lines. Each failure shows only its first line, its summary; the rest is
// usually a dump of the screen the recording already shows. Summaries are
// wrapped to the terminal width, cut to the rows left with a count of the
// rest, and padded to one width so the block is left-aligned when each line
// is centered.
func (ec ExportContext) failureLines() []CardLine {
	failures := ec.Failures()
	// Leave rows for the two title lines, a spacer, and a margin above and below.
	cols, rows := ec.cardGrid()
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
	lines := []CardLine{{}}
	for _, line := range msg {
		lines = append(lines, CardLine{Text: line + strings.Repeat(" ", block-uniseg.StringWidth(line))})
	}
	if more := len(failures) - shown; more > 0 {
		lines = append(lines, CardLine{Text: fmt.Sprintf("+%d more", more), Style: screen.Style{Attrs: screen.AttrFaint}})
	}
	return lines
}

// titleLines is head above the test name, for the default cards.
func (ec ExportContext) titleLines(head CardLine) []CardLine {
	return []CardLine{head, {Text: ec.h.tb.Name(), Style: screen.Style{Attrs: screen.AttrFaint}}}
}

// writeCentered writes line into row y of buf, centered by display width.
func writeCentered(buf *screen.Buffer, y int, line CardLine) {
	x := max(0, (buf.Width()-uniseg.StringWidth(line.Text))/2)
	g := uniseg.NewGraphemes(line.Text)
	for g.Next() {
		w := g.Width()
		if w == 0 {
			continue
		}
		if x+w > buf.Width() {
			return
		}
		buf.SetCell(x, y, &screen.Cell{Content: g.Str(), Style: line.Style, Width: w})
		x += w
	}
}

// introCard is the mutator behind [WithExportIntro]: lines as a card, or
// without lines the animated logo, or "steep" and the test name as text when
// the terminal is too small for the logo.
func introCard(d time.Duration, lines ...CardLine) ExportMutator {
	return func(ec ExportContext, f ExportFrame, emit func(ExportFrame)) {
		switch {
		case !ec.First:
		case len(lines) > 0:
			emit(ExportFrame{Image: ec.Card(lines...), Delay: d, Isolated: true})
		case !ec.steepLogo(d, emit):
			title := ec.titleLines(CardLine{Text: "steep", Style: screen.Style{Attrs: screen.AttrBold}})
			emit(ExportFrame{Image: ec.Card(title...), Delay: d, Isolated: true})
		}
		emit(f)
	}
}

// outroCard is the mutator behind [WithExportOutro]: lines as a card, or
// without lines PASS or FAIL with the test name and failures.
func outroCard(d time.Duration, lines ...CardLine) ExportMutator {
	return func(ec ExportContext, f ExportFrame, emit func(ExportFrame)) {
		emit(f)
		if !ec.Last {
			return
		}
		card := lines
		if len(card) == 0 {
			card = ec.resultLines()
		}
		emit(ExportFrame{Image: ec.Card(card...), Delay: d, Isolated: true})
	}
}

// resultLines are the default outro card: PASS or FAIL, the test name, and
// for a failure, what failed.
func (ec ExportContext) resultLines() []CardLine {
	if !ec.Failed {
		return ec.titleLines(CardLine{Text: "PASS", Style: screen.Style{Fg: screen.Indexed(2), Attrs: screen.AttrBold}})
	}
	lines := ec.titleLines(CardLine{Text: "FAIL", Style: screen.Style{Fg: screen.Indexed(1), Attrs: screen.AttrBold}})
	return append(lines, ec.failureLines()...)
}
