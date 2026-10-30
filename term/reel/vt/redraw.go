// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"bytes"
	"errors"
	"fmt"
	"image/color"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/reel/vt/internal/wasmvt"
)

// Redraw returns VT bytes that, written to a fresh terminal of the same size,
// draw what t shows, so a recording can start part way through. They hold:
//
//   - the active screen and its scrollback, with styles and hyperlinks
//   - the cursor's position, shape, and visibility, and its pen: style,
//     hyperlink, protection, and character sets
//   - the modes that differ from their defaults, such as the alternate
//     screen, bracketed paste, and mouse and keyboard reporting, the kitty
//     keyboard flags, and modifyOtherKeys
//   - the scrolling region and tab stops
//   - the window title and working directory
//   - the foreground, background, cursor, and indexed colors the program set
//   - the OSC 9;4 progress and OSC 7501 program status
//
// Kitty graphics placements on the active screen are sent again with their
// images, as of the current animation frame, cropped to the screen.
//
// Left out are the screen that is not active (the primary screen while the
// alternate screen is shown), the saved cursor, Kitty images with no
// placement on the active screen, notifications, and which rows were
// soft-wrapped, so a later resize reflows them as separate lines.
// In synchronized output, the bytes start a hold, but over the screen as it
// is rather than the screen the hold froze.
//
// The screen is written by libghostty-vt's VT formatter, which also restores
// the modes, scrolling region, tab stops, keyboard state, and cursor.
func (t *Terminal) Redraw() ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle {
		return nil, errClosed
	}
	screenVT, err := t.screenVTLocked()
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	// The cursor shape first: the modes the formatter restores include
	// the cursor's blinking, which DECSCUSR also sets.
	b.WriteString(cursorShape(t.cursorLocked()))
	b.Write(screenVT)
	t.redrawImagesLocked(&b)
	if t.title != "" {
		b.WriteString(ansi.SetWindowTitle(t.title))
	}
	for _, c := range []struct {
		current, def int32
		set          func(string) string
	}{
		{wasmvt.TerminalDataColorForeground, wasmvt.TerminalDataColorForegroundDefault, ansi.SetForegroundColor},
		{wasmvt.TerminalDataColorBackground, wasmvt.TerminalDataColorBackgroundDefault, ansi.SetBackgroundColor},
		{wasmvt.TerminalDataColorCursor, wasmvt.TerminalDataColorCursorDefault, ansi.SetCursorColor},
	} {
		// A color the program set differs from the default the host set.
		// A color neither set reads as the zero fallback both times.
		cur := t.readRGB(c.current, color.NRGBA{})
		if def := t.readRGB(c.def, color.NRGBA{}); cur != def {
			b.WriteString(c.set(fmt.Sprintf("#%02x%02x%02x", cur.R, cur.G, cur.B)))
		}
	}
	b.WriteString(paletteVT(t.paletteLocked()))
	b.WriteString(progressVT(t.progress))
	if t.statusReported && len(t.statuses) == 0 {
		b.WriteString(ansi.ClearProgramStatus)
	}
	for _, s := range t.statuses {
		b.WriteString(programStatusVT(s))
	}
	return b.Bytes(), nil
}

// screenVTLocked returns the formatter's output for the terminal, with the
// rows it trims added back. The formatter leaves out the blank rows after
// the last one with text, so written to a fresh terminal, its rows would
// scroll the screen and its scrollback short of where they were. Blank lines
// after the screen's own fill them in.
func (t *Terminal) screenVTLocked() ([]byte, error) {
	full, err := t.formatVTLocked(true)
	if err != nil {
		return nil, err
	}
	content, err := t.formatVTLocked(false)
	if err != nil {
		return nil, err
	}
	// The extras before the screen end with a cursor home, and the ones
	// after it start with the scrolling region and the cursor.
	home := bytes.Index(full, []byte("\x1b[H"))
	if home < 0 || !bytes.HasPrefix(full[home+3:], content) {
		return nil, errors.New("vt: redraw: unexpected formatter output")
	}
	// Every row the formatter writes but the last ends with CRLF, and the
	// first is at the top of the scrollback.
	written := bytes.Count(content, []byte("\r\n"))
	pad := max(t.scrollbackLenLocked()+t.rows-1-written, 0)
	end := home + 3 + len(content)
	out := make([]byte, 0, len(full)+2*pad)
	out = append(out, full[:end]...)
	out = append(out, bytes.Repeat([]byte("\r\n"), pad)...)
	return append(out, full[end:]...), nil
}

// formatVTLocked runs libghostty-vt's VT formatter over the terminal: the
// screen and its scrollback, with every extra but the palette when extras is
// set.
func (t *Terminal) formatVTLocked(extras bool) ([]byte, error) {
	optN := wasmvt.SizeFormatterTerminalOptions
	opt, err := t.mod.Alloc(optN)
	if err != nil {
		return nil, err
	}
	defer t.mod.Free(opt, optN)
	t.mod.Zero(opt, optN)
	t.mod.SetU32(opt, wasmvt.FormatterTerminalOptionsSizeOff, uint32(optN))
	t.mod.SetI32(opt, wasmvt.FormatterTerminalOptionsEmitOff, wasmvt.FormatterFormatVt)
	extra := opt + int32(wasmvt.FormatterTerminalOptionsExtraOff)
	t.mod.SetU32(extra, wasmvt.FormatterTerminalExtraSizeOff, uint32(wasmvt.SizeFormatterTerminalExtra))
	scr := extra + int32(wasmvt.FormatterTerminalExtraScreenOff)
	t.mod.SetU32(scr, wasmvt.FormatterScreenExtraSizeOff, uint32(wasmvt.SizeFormatterScreenExtra))
	var on byte
	if extras {
		on = 1
	}
	for _, off := range []int{
		wasmvt.FormatterTerminalExtraModesOff,
		wasmvt.FormatterTerminalExtraScrollingRegionOff,
		wasmvt.FormatterTerminalExtraTabstopsOff,
		wasmvt.FormatterTerminalExtraPwdOff,
		wasmvt.FormatterTerminalExtraKeyboardOff,
	} {
		t.mod.SetU8(extra, off, on)
	}
	for _, off := range []int{
		wasmvt.FormatterScreenExtraCursorOff,
		wasmvt.FormatterScreenExtraStyleOff,
		wasmvt.FormatterScreenExtraHyperlinkOff,
		wasmvt.FormatterScreenExtraProtectionOff,
		wasmvt.FormatterScreenExtraKittyKeyboardOff,
		wasmvt.FormatterScreenExtraCharsetsOff,
	} {
		t.mod.SetU8(scr, off, on)
	}

	out, err := t.mod.Alloc(4)
	if err != nil {
		return nil, err
	}
	defer t.mod.Free(out, 4)
	t.mod.Zero(out, 4)
	if rerr := wasmvt.Result("formatter new", t.mod.Xghostty_formatter_terminal_new(0, out, t.term, opt)); rerr != nil {
		return nil, rerr
	}
	f := t.mod.I32(out, 0)
	defer t.mod.Xghostty_formatter_free(f)

	// A null buffer reports the size needed.
	rc := t.mod.Xghostty_formatter_format_buf(f, 0, 0, out)
	if rc != 0 && rc != wasmvt.ResultOutOfSpace {
		return nil, wasmvt.Result("format", rc)
	}
	need := int(t.mod.U32(out, 0))
	if need == 0 {
		return nil, nil
	}
	buf, err := t.mod.Alloc(need)
	if err != nil {
		return nil, err
	}
	defer t.mod.Free(buf, need)
	if rerr := wasmvt.Result("format", t.mod.Xghostty_formatter_format_buf(f, buf, int32(need), out)); rerr != nil { //nolint:gosec // need fits the Wasm address space.
		return nil, rerr
	}
	return append([]byte(nil), t.mod.Slice(buf, min(int(t.mod.U32(out, 0)), need))...), nil
}

// cursorShape returns the DECSCUSR sequence for c's shape and blinking.
func cursorShape(c Cursor) string {
	var style int
	switch c.Shape {
	case CursorUnderline:
		style = 3
	case CursorBar:
		style = 5
	case CursorBlock, CursorBlockHollow:
		style = 1
	}
	if !c.Blink {
		style++
	}
	return ansi.SetCursorStyle(style)
}

// progressVT returns the OSC 9;4 report that sets p, or nothing when there
// is none.
func progressVT(p Progress) string {
	if !p.Reported || p.State == ProgressRemove {
		return ""
	}
	seq := fmt.Sprintf("\x1b]9;4;%d", p.State)
	if p.Percent >= 0 {
		seq += fmt.Sprintf(";%d", p.Percent)
	}
	return seq + "\x07"
}

// paletteVT returns the OSC 4 that sets p's colors, or nothing when it has
// none.
func paletteVT(p *Palette) string {
	if p == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\x1b]4")
	for i := range 256 {
		if c, ok := p.Color(uint8(i)); ok {
			fmt.Fprintf(&b, ";%d;#%02x%02x%02x", i, c.R, c.G, c.B)
		}
	}
	return b.String() + "\x07"
}

// programStatusVT returns the OSC 7501 report of s.
func programStatusVT(s ProgramStatus) string {
	return ansi.SetProgramStatus(ansi.ProgramStatus{
		State:       ansi.ProgramState(s.State.String()),
		ID:          s.ID,
		App:         s.App,
		Kind:        ansi.ProgramStatusKind(s.Kind.String()),
		Progress:    max(s.Progress, 0),
		HasProgress: s.Progress >= 0,
		Title:       s.Title,
		Message:     s.Message,
	})
}
