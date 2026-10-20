// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"fmt"
	"image"
	"strings"

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
// program set, so every card in a recording looks the same. Text beyond the
// terminal's width or height is cut off. The image is owned by the caller.
func (ec ExportContext) Card(lines ...CardLine) image.Image {
	buf := screen.NewBuffer(ec.src.cols, ec.src.rows)
	top := max(0, (ec.src.rows-len(lines))/2)
	for i, line := range lines {
		writeCentered(buf, top+i, line)
	}
	state := viewportState(ec.h, still.EmulatorState{}, ec.src.state)
	state.FgColor, state.BgColor = nil, nil
	// A blank screen has no cursor and nothing to scroll.
	state.CursorVisible = false
	state.ScrollbackCount = 0

	ec.h.imageMu.Lock()
	defer ec.h.imageMu.Unlock()
	return copyDrawnLocked(ec.h, buf, state)
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
	room := ec.src.rows - 5
	width := max(ec.src.cols-6, 10)

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
