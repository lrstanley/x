// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"image/color"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/vt"
)

// PlayCmd replays an asciicast file in this terminal.
type PlayCmd struct {
	WindowFlags `embed:""`

	Cast      string        `arg:"" placeholder:"CAST" help:"Asciicast file (v2 or v3), or - for standard input."`
	Speed     float64       `name:"speed" default:"1" help:"Playback speed: 2 plays twice as fast."`
	IdleLimit time.Duration `name:"idle-limit" placeholder:"DUR" help:"Cut pauses longer than this down to it."`
}

// Run plays the file at its times. On a terminal, the recording plays in
// reel's terminal emulator, whose screen is drawn on the alternate screen,
// so nothing the recording does reaches this terminal, which is left as it
// was; q or ctrl+c stop it. Otherwise, its output is written as is.
func (c *PlayCmd) Run(ctx context.Context) (err error) {
	if c.Speed <= 0 {
		return fmt.Errorf("--speed must be above 0, got %v", c.Speed)
	}
	tl, err := readCast(c.Cast)
	if err != nil {
		return err
	}
	if tl, err = c.window(tl); err != nil {
		return err
	}
	if !term.IsTerminal(os.Stdout.Fd()) {
		write := func(_ int, ev reel.Event) error {
			out, ok := ev.(reel.Output)
			if !ok {
				return nil
			}
			_, writeErr := os.Stdout.Write(out.Data)
			return writeErr
		}
		return replay(ctx, tl, c.Speed, c.IdleLimit, write, func() error { return nil })
	}

	p, err := reel.NewPlayer(ctx, tl)
	if err != nil {
		return err
	}
	defer p.Close()
	ctx, stopInput, err := swallowInput(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, stopInput()) }()
	v := newView(bufio.NewWriterSize(os.Stdout, 64<<10), p, tl.Header())
	v.size = func() (int, int, error) { return term.GetSize(os.Stdout.Fd()) }
	// The alternate screen keeps this terminal's screen and cursor to
	// return to.
	_, err = os.Stdout.WriteString("\x1b[?1049h\x1b[H\x1b[2J")
	if err == nil {
		err = replay(ctx, tl, c.Speed, c.IdleLimit, v.seek, v.draw)
	}
	_, resetErr := os.Stdout.WriteString("\x1b[0m\x1b]8;;\x1b\\\x1b[?25h\x1b[?1049l")
	if errors.Is(err, context.Canceled) {
		err = nil // Stopped with q or ctrl+c.
	}
	return errors.Join(err, resetErr)
}

// replay applies the events of tl's window at their times, scaled by
// speed, with pauses cut to idle when it is above zero, and calls flush
// before each pause and at the end. apply is given each event and its
// index. The events before the window are applied at once, to reach its
// start.
func replay(ctx context.Context, tl *reel.Timeline, speed float64, idle time.Duration, apply func(i int, ev reel.Event) error, flush func() error) error {
	from, _ := tl.Window()
	start := time.Now()
	var last, skipped time.Duration
	i := 0
	for ev := range tl.Events() {
		at := max(ev.At()-from, 0)
		if gap := at - last; idle > 0 && gap > idle {
			skipped += gap - idle
		}
		last = at
		if wait := time.Duration(float64(at-skipped)/speed) - time.Since(start); wait > 0 {
			if err := flush(); err != nil {
				return err
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}
		if err := apply(i, ev); err != nil {
			return err
		}
		i++
	}
	return flush()
}

// view draws a player's screen on this terminal, from its top left,
// cropped to this terminal's size: only the rows of this terminal that
// changed since the last draw, in one synchronized update, then the cursor
// where the recording's is, shown only when it is.
type view struct {
	w *bufio.Writer
	p *reel.Player
	// size returns this terminal's size, in columns and lines.
	size func() (cols, lines int, err error)
	// fg and bg are the recording's starting default colors, which it
	// leaves to this terminal unless its header set them (themed).
	fg, bg color.NRGBA
	themed struct{ fg, bg bool }
	// palette is the recording's 16 ANSI colors, as its header has them.
	palette [16]color.Color
	// drawn is what was last drawn: the frame's generation, this
	// terminal's rows and size, and the colors; sgrs caches the styles'
	// SGR sequences for those colors.
	drawn struct {
		generation  uint64
		rows        []string
		cols, lines int
		colors      colors
	}
	sgrs map[vt.Style]string
}

// colors are the recording's colors that a cell's style leaves to the
// terminal: its default foreground and background, nil when it leaves them
// to this terminal too, the 16 ANSI colors of its header, and the indexed
// colors its program set.
type colors struct {
	fg, bg  color.Color
	palette *[16]color.Color
	program *vt.Palette
}

// newView returns a view of p, which plays a recording of header h.
func newView(w *bufio.Writer, p *reel.Player, h reel.Header) *view {
	v := &view{w: w, p: p, palette: h.Palette}
	v.themed.fg, v.themed.bg = h.Theme.Fg != nil, h.Theme.Bg != nil
	st := p.Frame().State
	v.fg, v.bg = st.Foreground, st.Background
	return v
}

// seek plays the events up to and including the one at i.
func (v *view) seek(i int, _ reel.Event) error { return v.p.Seek(i + 1) }

// colors returns the colors to draw st with: its default colors where the
// recording changed them, by its theme events or its program's OSC 10 and
// 11, or its header set them, and the palettes.
func (v *view) colors(st vt.State) colors {
	c := colors{palette: &v.palette, program: st.Palette}
	if st.Foreground != v.fg || v.themed.fg {
		c.fg = st.Foreground
	}
	if st.Background != v.bg || v.themed.bg {
		c.bg = st.Background
	}
	return c
}

// sgr returns the SGR sequence of s, with v's last drawn colors.
func (v *view) sgr(s vt.Style) string {
	seq, ok := v.sgrs[s]
	if !ok {
		seq = sgr(s, v.drawn.colors)
		v.sgrs[s] = seq
	}
	return seq
}

func (v *view) draw() error {
	cols, lines, err := v.size()
	if err != nil || cols < 1 || lines < 1 {
		cols, lines = v.drawn.cols, v.drawn.lines
	}
	f := v.p.Frame()
	pal := v.colors(f.State)
	d := &v.drawn
	full := cols != d.cols || lines != d.lines || pal != d.colors
	if full {
		d.rows, d.cols, d.lines, d.colors = make([]string, lines), cols, lines, pal
		v.sgrs = map[vt.Style]string{}
		for y := range d.rows {
			d.rows[y] = "\x00" // Not a row's text, so every row is drawn.
		}
	}
	_, _ = v.w.WriteString("\x1b[?2026h\x1b[?25l")
	if full || f.State.Generation != d.generation {
		d.generation = f.State.Generation
		blank := v.sgr(vt.Style{})
		for y := range lines {
			row := ""
			if y < f.Screen.Height() {
				row = renderRow(f.Screen, y, cols, v.sgr)
			}
			if row == d.rows[y] {
				continue
			}
			d.rows[y] = row
			// The row is cleared first: after a row as wide as this terminal,
			// the cursor is on its last cell, which clearing after would erase.
			fmt.Fprintf(v.w, "\x1b[%d;1H%s\x1b[2K%s\x1b]8;;\x1b\\", y+1, blank, row)
		}
	}
	if cur := f.State.Cursor; cur.Visible && cur.X >= 0 && cur.X < cols && cur.Y >= 0 && cur.Y < lines {
		fmt.Fprintf(v.w, "\x1b[%d;%dH\x1b[?25h", cur.Y+1, cur.X+1)
	}
	_, _ = v.w.WriteString("\x1b[?2026l")
	return v.w.Flush()
}

// renderRow returns row y of buf as text with the SGR sequences sgr gives
// for its styles, and OSC 8 links, up to cols columns, without a wide
// character cut in two.
func renderRow(buf *vt.Buffer, y, cols int, sgr func(vt.Style) string) string {
	var b strings.Builder
	style := vt.Style{Attrs: ^vt.Attr(0)} // No cell's, so the first sets its own.
	var link vt.Link
	for x := range min(buf.Width(), cols) {
		c := buf.CellAt(x, y)
		if c == nil {
			c = &vt.EmptyCell
		}
		if c.Width == 0 {
			continue // The tail of a wide character.
		}
		if c.Style != style {
			style = c.Style
			b.WriteString(sgr(style))
		}
		if c.Link != link {
			link = c.Link
			b.WriteString(ansi.SetHyperlink(link.URL, link.Params))
		}
		switch {
		case x+c.Width > cols, c.Content == "":
			b.WriteByte(' ')
		default:
			b.WriteString(c.Content)
		}
	}
	return b.String()
}

// sgr returns the SGR sequence that sets s, from the default style, with
// the colors s leaves to the terminal from pal.
func sgr(s vt.Style, pal colors) string {
	st := ansi.NewStyle().Reset()
	attrs := []struct {
		attr vt.Attr
		set  func(ansi.Style) ansi.Style
	}{
		{vt.AttrBold, ansi.Style.Bold},
		{vt.AttrFaint, ansi.Style.Faint},
		{vt.AttrItalic, func(s ansi.Style) ansi.Style { return s.Italic(true) }},
		{vt.AttrBlink, func(s ansi.Style) ansi.Style { return s.Blink(true) }},
		{vt.AttrRapidBlink, func(s ansi.Style) ansi.Style { return s.RapidBlink(true) }},
		{vt.AttrReverse, func(s ansi.Style) ansi.Style { return s.Reverse(true) }},
		{vt.AttrConceal, func(s ansi.Style) ansi.Style { return s.Conceal(true) }},
		{vt.AttrStrikethrough, func(s ansi.Style) ansi.Style { return s.Strikethrough(true) }},
	}
	for _, a := range attrs {
		if s.Attrs&a.attr != 0 {
			st = a.set(st)
		}
	}
	if s.Underline != vt.UnderlineNone {
		st = st.UnderlineStyle(ansi.Underline(s.Underline))
	}
	if fg := cmp.Or(s.Fg, pal.fg); fg != nil {
		st = st.ForegroundColor(pal.ansi(fg))
	}
	if bg := cmp.Or(s.Bg, pal.bg); bg != nil {
		st = st.BackgroundColor(pal.ansi(bg))
	}
	if s.UnderlineColor != nil {
		st = st.UnderlineColor(pal.ansi(s.UnderlineColor))
	}
	return st.String()
}

// ansi returns c as SGR encodes it. An indexed color is the color the
// recording's program set for it, or else one of the 16 ANSI colors as the
// recording's header has it, or else its index, so this terminal draws it
// from its own palette, as the recording's terminal did.
func (pal colors) ansi(c color.Color) ansi.Color {
	i, ok := c.(vt.Indexed)
	if !ok {
		return c
	}
	if pal.program != nil {
		if set, isSet := pal.program.Color(uint8(i)); isSet {
			return set
		}
	}
	if i < 16 && pal.palette != nil && pal.palette[i] != nil {
		return pal.palette[i]
	}
	return ansi.IndexedColor(i)
}

// swallowInput puts standard input, when it is a terminal whose reads can
// be interrupted, in raw mode while a recording plays, so that what is
// typed is not echoed over it, and discards what it reads; q or ctrl+c
// cancel the returned context. stop restores the terminal.
func swallowInput(ctx context.Context) (context.Context, func() error, error) {
	in := os.Stdin.Fd()
	if !canInterrupt || !term.IsTerminal(in) {
		return ctx, func() error { return nil }, nil
	}
	state, err := term.MakeRaw(in)
	if err != nil {
		return nil, nil, err
	}
	r, stopRead, err := interruptible()
	if err != nil {
		return nil, nil, errors.Join(err, term.Restore(in, state))
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 1024)
		for {
			n, readErr := r.Read(buf)
			if readErr != nil {
				return
			}
			for _, st := range vt.DecodeInput(buf[:n]) {
				if st.Kind == vt.StrokeText && strings.Contains(st.Text, "q") ||
					st.Kind == vt.StrokeKey && st.Key.String() == "ctrl+c" {
					cancel()
				}
			}
		}
	}()
	return ctx, func() error {
		stopRead()
		<-done
		cancel()
		return term.Restore(in, state)
	}, nil
}
