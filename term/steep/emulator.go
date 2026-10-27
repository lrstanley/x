// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"image"
	"image/color"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/reel/vt"
	"github.com/lrstanley/x/term/reel/vt/screen"
	"github.com/lrstanley/x/term/steep/snapshot"
)

var (
	_ io.Reader = (*emulator)(nil)
	_ io.Writer = (*emulator)(nil)
)

type emulator struct {
	term         *vt.Terminal
	shutdownOnce sync.Once

	focused bool
}

func (e *emulator) writeString(s string) error {
	_, err := e.term.Write([]byte(s))
	return err
}

// termRuntime is the emulator pool for steep. A size below 1 follows
// GOMAXPROCS, so parallel tests stay within one Wasm instance per processor.
var termRuntime = vt.NewRuntime(0)

func newEmulator(tb testing.TB, width, height int) *emulator {
	tb.Helper()
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	term, err := termRuntime.Open(tb.Context(), width, height)
	if err != nil {
		tb.Fatal(err)
	}
	return &emulator{term: term, focused: true}
}

// snapshot produces a snapshot of the current terminal state.
func (e *emulator) snapshot(tb testing.TB, _ ...snapshot.Option) *snapshot.ScreenSnapshot {
	tb.Helper()
	grid := e.term.Screen()
	cur := e.term.Cursor()
	fg := e.term.Foreground()
	bg := e.term.Background()
	cursorColor, hasCursorColor := e.term.CursorColor()
	cols, rows := e.term.Size()
	cursorSnap := snapshot.Cursor{
		Position: snapshot.Position{X: cur.X, Y: cur.Y},
		Visible:  cur.Visible,
		Blink:    cur.Blink,
		Style:    cur.Shape,
	}
	if hasCursorColor {
		cursorSnap.Color = snapshot.Color{Color: cursorColor}
	}
	snap := &snapshot.ScreenSnapshot{
		Title:         e.term.Title(),
		Rows:          rows,
		Cols:          cols,
		AltScreen:     e.term.AltScreen(),
		Focused:       e.focused,
		ProgramStatus: snapshotProgramStatuses(e.programStatuses()),
		Cursor:        cursorSnap,
		BgColor:       snapshot.Color{Color: bg},
		FgColor:       snapshot.Color{Color: fg},
	}
	snap.WithScreenBuffer(tb, grid)
	return snap
}

// closeInput unblocks program reads with EOF. The grid stays available
// until [emulator.Close].
func (e *emulator) closeInput() { e.term.CloseReader() }

func (e *emulator) Close() {
	e.shutdownOnce.Do(func() {
		e.closeInput()
		e.term.SetWriteNotify(nil)
		_ = e.term.Close()
	})
}

// setWriteNotify registers fn to run after each program write to the
// terminal. See [vt.Terminal.SetWriteNotify].
func (e *emulator) setWriteNotify(fn func()) {
	e.term.SetWriteNotify(fn)
}

func (e *emulator) Read(p []byte) (int, error)  { return e.term.Read(p) }
func (e *emulator) Write(p []byte) (int, error) { return e.term.Write(p) }

// View returns the current VT screen text. Harness wait, assert, and
// snapshot helpers sample this rendered output.
//
// Empty cells are spaces, so each line is the terminal width and a blank
// row is a line of spaces. [Harness.Width] and [Harness.Height] match
// that grid. Size the terminal with [WithWindowSize] or [Harness.Resize]
// when the text under test should fill the window.
func (h *Harness) View() string {
	h.tb.Helper()
	return h.emulator.term.Text()
}

// Focus sends the terminal a focus event if focus events mode is enabled. This is
// the opposite of [Harness.Blur].
func (h *Harness) Focus() *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	_ = h.emulator.term.Focus(true)
	h.emulator.focused = true
	return h
}

// Blur sends the terminal a blur event if focus events mode is enabled. This is
// the opposite of [Harness.Focus].
func (h *Harness) Blur() *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	_ = h.emulator.term.Focus(false)
	h.emulator.focused = false
	return h
}

// IsFocused reports the last known terminal focus state.
func (h *Harness) IsFocused() bool { return h.emulator.focused }

// Title returns the last known window title.
func (h *Harness) Title() string { return h.emulator.term.Title() }

// LastBellAt returns the wall clock time of the most recent bell ([ansi.BEL])
// processed by the VT emulator. It is the zero [time.Time] until the first bell.
func (h *Harness) LastBellAt() time.Time { return h.emulator.term.LastBellAt() }

// Bounds returns the terminal's current screen bounds.
func (h *Harness) Bounds() image.Rectangle {
	w, height := h.emulator.term.Size()
	return image.Rect(0, 0, w, height)
}

// IsAltScreen returns true if the terminal is in alt screen mode.
func (h *Harness) IsAltScreen() bool { return h.emulator.term.AltScreen() }

// ModeSetting returns the tracked ANSI mode setting. An absent mode yields
// [ansi.ModeNotRecognized].
func (h *Harness) ModeSetting(mode ansi.Mode) ansi.ModeSetting {
	_, ansiMode := mode.(ansi.ANSIMode)
	on, ok := h.emulator.term.Mode(vt.Mode{Number: mode.Mode(), ANSI: ansiMode})
	if !ok {
		return ansi.ModeNotRecognized
	}
	if on {
		return ansi.ModeSet
	}
	return ansi.ModeReset
}

// Width returns the terminal width in cells. This matches [Harness.View], which
// pads each line out to this width.
func (h *Harness) Width() int {
	w, _ := h.emulator.term.Size()
	return w
}

// Height returns the terminal's current height in cells (matches [Harness.View]
// row count).
func (h *Harness) Height() int {
	_, height := h.emulator.term.Size()
	return height
}

// Dimensions returns the terminal's width and height in cells.
func (h *Harness) Dimensions() (width, height int) { return h.emulator.term.Size() }

// Resize resizes the terminal to the given width and height. This should result
// in a [tea.WindowSizeMsg] being sent to the [tea.Program].
func (h *Harness) Resize(width, height int) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	_ = h.emulator.term.Resize(width, height)
	return h
}

// Paste pastes text into the terminal. If bracketed paste mode is enabled, the
// text is bracketed with the appropriate escape sequences.
func (h *Harness) Paste(text string) *Harness {
	h.tb.Helper()
	defer h.hook(HookInput, InputMeta{Kind: "paste", Text: text})()
	_ = h.emulator.term.Paste(text)
	return h
}

// FgColor returns the terminal emulator's foreground color.
func (h *Harness) FgColor() color.Color { return h.emulator.term.Foreground() }

// BgColor returns the terminal emulator's background color.
func (h *Harness) BgColor() color.Color { return h.emulator.term.Background() }

// CursorColor returns the terminal emulator's cursor color.
func (h *Harness) CursorColor() color.Color {
	c, _ := h.emulator.term.CursorColor()
	return c
}

// SetFgColor sets the terminal emulator foreground color.
func (h *Harness) SetFgColor(c color.Color) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	_ = h.emulator.term.SetForeground(c)
	return h
}

// SetBgColor sets the terminal emulator background color.
func (h *Harness) SetBgColor(c color.Color) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	_ = h.emulator.term.SetBackground(c)
	return h
}

// SetCursorColor sets the terminal emulator cursor color.
func (h *Harness) SetCursorColor(c color.Color) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	_ = h.emulator.term.SetCursorColor(c)
	return h
}

// SetDefaultFgColor sets the default foreground color used when none is specified.
func (h *Harness) SetDefaultFgColor(c color.Color) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	_ = h.emulator.term.SetDefaultForeground(c)
	return h
}

// SetDefaultBgColor sets the default background color used when none is specified.
func (h *Harness) SetDefaultBgColor(c color.Color) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	_ = h.emulator.term.SetDefaultBackground(c)
	return h
}

// SetDefaultCursorColor sets the default cursor color used when none is specified.
func (h *Harness) SetDefaultCursorColor(c color.Color) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	_ = h.emulator.term.SetDefaultCursorColor(c)
	return h
}

// CursorPosition returns the terminal emulator cursor position.
func (h *Harness) CursorPosition() image.Point {
	cur := h.emulator.term.Cursor()
	return image.Pt(cur.X, cur.Y)
}

// CursorVisible reports whether the cursor is shown. Visibility follows DECTCEM.
func (h *Harness) CursorVisible() bool { return h.emulator.term.Cursor().Visible }

// CursorBlink reports whether the cursor style is blinking.
func (h *Harness) CursorBlink() bool { return h.emulator.term.Cursor().Blink }

// CursorStyle returns the current VT cursor shape.
func (h *Harness) CursorStyle() screen.CursorShape {
	return h.emulator.term.Cursor().Shape
}

// Scrollback returns a copy of the terminal's scrollback rows (oldest first).
// It is nil in alternate screen mode.
func (h *Harness) Scrollback() [][]screen.Cell { return h.emulator.term.Scrollback() }

// ScrollbackCount returns the count of scrollback lines.
func (h *Harness) ScrollbackCount() int { return h.emulator.term.ScrollbackLen() }

// SetScrollbackSize sets the maximum scrollback lines retained on the terminal's
// screen buffer.
func (h *Harness) SetScrollbackSize(maxLines int) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	_ = h.emulator.term.SetScrollbackMaxLines(maxLines)
	return h
}

// ClearScrollback clears all scrollback history on the screen buffer.
func (h *Harness) ClearScrollback() *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	_ = h.emulator.term.ClearScrollback()
	return h
}

// Type sends a sequence of key presses to the terminal emulator itself.
// This is for regular text input, not modifier combinations such as
// ctrl-key or alt-key.
//
// Because this goes through the emulator, pairing it with [Harness.SendProgram]
// can deliver events to the [tea.Program] in a different order than the
// calls were made.
func (h *Harness) Type(s string) *Harness {
	h.tb.Helper()
	defer h.hook(HookInput, InputMeta{Kind: "type", Text: s})()
	h.typeKeys(s)
	return h
}

func (h *Harness) typeKeys(s string) {
	for _, r := range s {
		_ = h.emulator.term.SendKey(vt.Key{Code: letterCode(r), Text: string(r)})
	}
}

// Key sends a single key press to the terminal emulator itself, for example
// "ctrl+a", "enter", or "space".
//
// Because this goes through the emulator, pairing it with [Harness.SendProgram]
// can deliver events to the [tea.Program] in a different order than the
// calls were made.
func (h *Harness) Key(key string) *Harness {
	h.tb.Helper()
	defer h.hook(HookInput, InputMeta{Kind: "key", Key: key})()
	h.key(key)
	return h
}

func (h *Harness) key(key string) {
	ev := mapKey(key)
	if ev.Code == "" && ev.Text != "" {
		h.typeKeys(ev.Text)
		return
	}
	_ = h.emulator.term.SendKey(ev)
}

// KeyUp sends an up-arrow key press to the terminal emulator.
func (h *Harness) KeyUp() *Harness { return h.Key("up") }

// KeyDown sends a down-arrow key press to the terminal emulator.
func (h *Harness) KeyDown() *Harness { return h.Key("down") }

// KeyLeft sends a left-arrow key press to the terminal emulator.
func (h *Harness) KeyLeft() *Harness { return h.Key("left") }

// KeyRight sends a right-arrow key press to the terminal emulator.
func (h *Harness) KeyRight() *Harness { return h.Key("right") }

// KeyEsc sends an escape key press to the terminal emulator.
func (h *Harness) KeyEsc() *Harness { return h.Key("esc") }

// KeyDelete sends a delete (forward-delete) key press to the terminal emulator.
func (h *Harness) KeyDelete() *Harness { return h.Key("delete") }

// KeyBackspace sends a backspace key press to the terminal emulator.
func (h *Harness) KeyBackspace() *Harness { return h.Key("backspace") }

// KeyCtrlC sends ctrl+c to the terminal emulator.
func (h *Harness) KeyCtrlC() *Harness { return h.Key("ctrl+c") }

// KeyCtrlD sends ctrl+d to the terminal emulator.
func (h *Harness) KeyCtrlD() *Harness { return h.Key("ctrl+d") }

// Mouse forwards a mouse event through the terminal emulator. The running
// [tea.Program] must have enabled a mouse tracking mode and, typically, SGR
// mouse encoding. Otherwise the emulator discards the event, matching a
// real terminal.
func (h *Harness) Mouse(action vt.MouseAction, button vt.MouseButton, x, y int) *Harness {
	h.tb.Helper()
	defer h.hook(HookInput, InputMeta{Kind: "mouse"})()
	_ = h.emulator.term.SendMouse(action, button, x, y)
	return h
}

// MouseClick simulates a full press and release at (x, y).
func (h *Harness) MouseClick(button vt.MouseButton, x, y int) *Harness {
	h.tb.Helper()
	defer h.hook(HookInput, InputMeta{Kind: "mouse_click"})()
	h.mouseClick(button, x, y)
	return h
}

func (h *Harness) mouseClick(button vt.MouseButton, x, y int) {
	_ = h.emulator.term.SendMouse(vt.MousePress, button, x, y)
	_ = h.emulator.term.SendMouse(vt.MouseRelease, button, x, y)
}

// LeftClick is [Harness.MouseClick] with the left mouse button.
func (h *Harness) LeftClick(x, y int) *Harness {
	h.tb.Helper()
	defer h.hook(HookInput, InputMeta{Kind: "left_click"})()
	h.mouseClick(vt.MouseLeft, x, y)
	return h
}

// MiddleClick is [Harness.MouseClick] with the middle mouse button.
func (h *Harness) MiddleClick(x, y int) *Harness {
	h.tb.Helper()
	defer h.hook(HookInput, InputMeta{Kind: "middle_click"})()
	h.mouseClick(vt.MouseMiddle, x, y)
	return h
}

// RightClick is [Harness.MouseClick] with the right mouse button.
func (h *Harness) RightClick(x, y int) *Harness {
	h.tb.Helper()
	defer h.hook(HookInput, InputMeta{Kind: "right_click"})()
	h.mouseClick(vt.MouseRight, x, y)
	return h
}

// MouseDrag simulates a drag from (x1, y1) to (x2, y2).
func (h *Harness) MouseDrag(button vt.MouseButton, x1, y1, x2, y2 int) *Harness {
	h.tb.Helper()
	defer h.hook(HookInput, InputMeta{Kind: "mouse_drag"})()
	h.mouseDrag(button, x1, y1, x2, y2)
	return h
}

func (h *Harness) mouseDrag(button vt.MouseButton, x1, y1, x2, y2 int) {
	_ = h.emulator.term.SendMouse(vt.MousePress, button, x1, y1)
	dx, dy := 0, 0
	switch {
	case x2 > x1:
		dx = 1
	case x2 < x1:
		dx = -1
	}
	switch {
	case y2 > y1:
		dy = 1
	case y2 < y1:
		dy = -1
	}
	x, y := x1, y1
	for x != x2 || y != y2 {
		if x != x2 {
			x += dx
		}
		if y != y2 {
			y += dy
		}
		_ = h.emulator.term.SendMouse(vt.MouseMotion, button, x, y)
	}
	_ = h.emulator.term.SendMouse(vt.MouseRelease, button, x2, y2)
}

// LeftDrag is [Harness.MouseDrag] with the left mouse button.
func (h *Harness) LeftDrag(x1, y1, x2, y2 int) *Harness {
	h.tb.Helper()
	defer h.hook(HookInput, InputMeta{Kind: "left_drag"})()
	h.mouseDrag(vt.MouseLeft, x1, y1, x2, y2)
	return h
}

// Progress returns the latest OSC 9;4 progress report.
func (h *Harness) Progress() screen.Progress { return h.emulator.term.Progress() }

// Notifications returns desktop notifications from OSC 9 and OSC 777.
func (h *Harness) Notifications() []vt.Notification { return h.emulator.term.Notifications() }

// Clipboard returns the last text the program wrote to the clipboard.
func (h *Harness) Clipboard() string { return h.emulator.term.Clipboard() }

// SetClipboard sets the host clipboard returned for OSC 52 reads.
func (h *Harness) SetClipboard(text string) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.emulator.term.SetClipboard(text)
	return h
}

// WorkingDirectory returns the last OSC 7 working directory.
func (h *Harness) WorkingDirectory() string { return h.emulator.term.WorkingDirectory() }

// Prompt returns the latest OSC 133 semantic prompt step.
func (h *Harness) Prompt() vt.Prompt { return h.emulator.term.Prompt() }

// ResizeRequest returns the last CSI 8 t window-resize request.
// ok is false until the program asks. The harness is not resized.
func (h *Harness) ResizeRequest() (cols, rows int, ok bool) {
	return h.emulator.term.ResizeRequest()
}

const programStatusMaxRecords = 256

// ProgramStatuses returns the current OSC 7501 records, sorted by id. App is
// copied from the nearest ancestor that set one. Each report replaces its
// record; at most 256 records are kept. The returned slice is a copy.
//
// Working and blocked records are removed when a shell prompt starts (OSC 133
// A). A full reset (RIS) removes every record.
//
// With [render.WithWindowChrome] in [WithRenderer], [Harness.Image] and GIF
// exports draw the records in the title bar and as an outline around the
// window (see [render.WithProgramStatus]). Once any record has been reported,
// OSC 9;4 progress is no longer drawn, until a full reset, as the OSC 7501
// spec asks.
func (h *Harness) ProgramStatuses() []ansi.ProgramStatus {
	return h.emulator.programStatuses()
}

func (e *emulator) programStatuses() []ansi.ProgramStatus {
	in := e.term.ProgramStatuses()
	if len(in) == 0 {
		return nil
	}
	out := make([]ansi.ProgramStatus, len(in))
	for i, s := range in {
		out[i] = ansi.ProgramStatus{
			ID:      s.ID,
			State:   ansi.ProgramState(s.State.String()),
			Kind:    ansi.ProgramStatusKind(s.Kind.String()),
			App:     s.App,
			Title:   s.Title,
			Message: s.Message,
		}
		if s.Progress >= 0 {
			out[i].Progress = s.Progress
			out[i].HasProgress = true
		}
	}
	return out
}

func snapshotProgramStatuses(in []ansi.ProgramStatus) []snapshot.ProgramStatus {
	if len(in) == 0 {
		return nil
	}
	out := make([]snapshot.ProgramStatus, len(in))
	for i, s := range in {
		out[i] = snapshot.ProgramStatus{
			ID:      s.ID,
			State:   string(s.State),
			Kind:    string(s.Kind),
			App:     s.App,
			Title:   s.Title,
			Message: s.Message,
		}
		if s.HasProgress {
			progress := s.Progress
			out[i].Progress = &progress
		}
	}
	return out
}
