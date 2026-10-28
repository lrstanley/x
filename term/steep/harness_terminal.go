// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"image"
	"image/color"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/vt"
)

// View returns the current VT screen text. Harness wait, assert, and
// snapshot helpers sample this rendered output.
//
// Empty cells are spaces, so each line is the terminal width and a blank
// row is a line of spaces. [Harness.Width] and [Harness.Height] match
// that grid. Size the terminal with [WithWindowSize] or [Harness.Resize]
// when the text under test should fill the window.
func (h *Harness) View() string {
	h.tb.Helper()
	return h.session.Terminal().Text()
}

// Focus sends the terminal a focus event if focus events mode is enabled. This is
// the opposite of [Harness.Blur].
func (h *Harness) Focus() *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.check(h.session.Focus(true))
	return h
}

// Blur sends the terminal a blur event if focus events mode is enabled. This is
// the opposite of [Harness.Focus].
func (h *Harness) Blur() *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.check(h.session.Focus(false))
	return h
}

// IsFocused reports the last known terminal focus state.
func (h *Harness) IsFocused() bool { return h.session.Focused() }

// Title returns the last known window title.
func (h *Harness) Title() string { return h.session.Terminal().Title() }

// LastBellAt returns the wall clock time of the most recent bell ([ansi.BEL])
// processed by the VT emulator. It is the zero [time.Time] until the first bell.
func (h *Harness) LastBellAt() time.Time { return h.session.Terminal().LastBellAt() }

// Bounds returns the terminal's current screen bounds.
func (h *Harness) Bounds() image.Rectangle {
	w, height := h.session.Terminal().Size()
	return image.Rect(0, 0, w, height)
}

// IsAltScreen returns true if the terminal is in alt screen mode.
func (h *Harness) IsAltScreen() bool { return h.session.Terminal().AltScreen() }

// ModeSetting returns the tracked ANSI mode setting. An absent mode yields
// [ansi.ModeNotRecognized].
func (h *Harness) ModeSetting(mode ansi.Mode) ansi.ModeSetting {
	_, ansiMode := mode.(ansi.ANSIMode)
	on, ok := h.session.Terminal().Mode(vt.Mode{Number: mode.Mode(), ANSI: ansiMode})
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
	w, _ := h.session.Terminal().Size()
	return w
}

// Height returns the terminal's current height in cells (matches [Harness.View]
// row count).
func (h *Harness) Height() int {
	_, height := h.session.Terminal().Size()
	return height
}

// Dimensions returns the terminal's width and height in cells.
func (h *Harness) Dimensions() (width, height int) { return h.session.Terminal().Size() }

// Resize resizes the terminal to the given width and height. This should result
// in a [tea.WindowSizeMsg] being sent to the [tea.Program].
func (h *Harness) Resize(width, height int) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.check(h.session.Resize(width, height))
	return h
}

// Paste pastes text into the terminal. If bracketed paste mode is enabled, the
// text is bracketed with the appropriate escape sequences.
func (h *Harness) Paste(text string) *Harness {
	h.tb.Helper()
	meta := InputMeta{Kind: InputKindPaste, Text: text}
	defer h.hook(HookInput, meta)()
	h.annotate(&inputNote{InputMeta: meta})
	h.check(h.session.Paste(text))
	return h
}

// FgColor returns the terminal emulator's foreground color.
func (h *Harness) FgColor() color.Color { return h.session.Terminal().Foreground() }

// BgColor returns the terminal emulator's background color.
func (h *Harness) BgColor() color.Color { return h.session.Terminal().Background() }

// CursorColor returns the terminal emulator's cursor color.
func (h *Harness) CursorColor() color.Color {
	c, _ := h.session.Terminal().CursorColor()
	return c
}

// SetFgColor sets the terminal emulator foreground color.
func (h *Harness) SetFgColor(c color.Color) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.check(h.session.SetForeground(c))
	return h
}

// SetBgColor sets the terminal emulator background color.
func (h *Harness) SetBgColor(c color.Color) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.check(h.session.SetBackground(c))
	return h
}

// SetCursorColor sets the terminal emulator cursor color.
func (h *Harness) SetCursorColor(c color.Color) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.check(h.session.SetCursorColor(c))
	return h
}

// SetDefaultFgColor sets the default foreground color used when none is specified.
func (h *Harness) SetDefaultFgColor(c color.Color) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.check(h.session.SetDefaultForeground(c))
	return h
}

// SetDefaultBgColor sets the default background color used when none is specified.
func (h *Harness) SetDefaultBgColor(c color.Color) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.check(h.session.SetDefaultBackground(c))
	return h
}

// SetDefaultCursorColor sets the default cursor color used when none is specified.
func (h *Harness) SetDefaultCursorColor(c color.Color) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.check(h.session.SetDefaultCursorColor(c))
	return h
}

// CursorPosition returns the terminal emulator cursor position, or (-1, -1)
// while the viewport is scrolled off it (see [Harness.ScrollUp]).
func (h *Harness) CursorPosition() image.Point {
	cur := h.session.Terminal().Cursor()
	return image.Pt(cur.X, cur.Y)
}

// CursorVisible reports whether the cursor is shown. Visibility follows DECTCEM.
func (h *Harness) CursorVisible() bool { return h.session.Terminal().Cursor().Visible }

// CursorBlink reports whether the cursor style is blinking.
func (h *Harness) CursorBlink() bool { return h.session.Terminal().Cursor().Blink }

// CursorStyle returns the current VT cursor shape.
func (h *Harness) CursorStyle() vt.CursorShape {
	return h.session.Terminal().Cursor().Shape
}

// Scrollback returns a copy of the terminal's scrollback rows (oldest first).
// It is nil in alternate screen mode.
func (h *Harness) Scrollback() [][]vt.Cell { return h.session.Terminal().Scrollback() }

// ScrollbackCount returns the count of scrollback lines.
func (h *Harness) ScrollbackCount() int { return h.session.Terminal().ScrollbackLen() }

// ScrollbackOffset returns how many rows the terminal's viewport is scrolled up
// from the bottom, so zero while it shows the live screen.
func (h *Harness) ScrollbackOffset() int { return h.session.Terminal().ScrollbackOffset() }

// ScrollUp scrolls the terminal's viewport n rows toward older scrollback, the
// way a terminal's scrollbar does, so [Harness.View] and snapshots show the
// older rows, and a scrollbar drawn with window chrome moves. The program
// is not told, and the viewport stays where it is as the program writes more,
// until [Harness.ScrollDown] or [Harness.ScrollToBottom] returns to the bottom,
// or until [Harness.Type], [Harness.Key], or [Harness.Paste] send it input
// (see [WithScrollOnInput]).
func (h *Harness) ScrollUp(n int) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.check(h.session.ScrollUp(n))
	return h
}

// ScrollDown scrolls the terminal's viewport n rows toward the live screen.
// See [Harness.ScrollUp].
func (h *Harness) ScrollDown(n int) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.check(h.session.ScrollDown(n))
	return h
}

// ScrollToTop scrolls the terminal's viewport to the oldest scrollback row.
// See [Harness.ScrollUp].
func (h *Harness) ScrollToTop() *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.check(h.session.ScrollToTop())
	return h
}

// ScrollToBottom scrolls the terminal's viewport back to the live screen. See
// [Harness.ScrollUp].
func (h *Harness) ScrollToBottom() *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.check(h.session.ScrollToBottom())
	return h
}

// SetScrollbackSize sets the maximum scrollback lines retained on the terminal's
// screen buffer.
func (h *Harness) SetScrollbackSize(maxLines int) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.annotate(&terminalNote{Action: "scrollback_size", Lines: &maxLines})
	h.check(h.session.Terminal().SetScrollbackMaxLines(maxLines))
	return h
}

// ClearScrollback clears all scrollback history on the screen buffer.
func (h *Harness) ClearScrollback() *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.annotate(&terminalNote{Action: "clear_scrollback"})
	h.check(h.session.Terminal().ClearScrollback())
	return h
}

// Type sends a sequence of key presses to the terminal emulator itself.
// This is for regular text input, not modifier combinations such as
// ctrl-key or alt-key.
//
// Because this goes through the emulator, pairing it with [Harness.SendProgram]
// can deliver events to the [tea.Program] in a different order than the
// calls were made.
//
// Pass [WithTypingDelay] to pause after each key press of this call.
func (h *Harness) Type(s string, opts ...Option) *Harness {
	h.tb.Helper()
	meta := InputMeta{Kind: InputKindType, Text: s}
	defer h.hook(HookInput, meta)()
	h.annotate(&inputNote{InputMeta: meta})
	h.check(h.session.Type(s, h.keyDelay(opts)))
	return h
}

// Key sends a single key press to the terminal emulator itself, for example
// "ctrl+a", "enter", "shift+tab", or "space". See
// [github.com/lrstanley/x/term/reel.Session.Key] for the names it accepts; an
// unknown name fails the test.
//
// Because this goes through the emulator, pairing it with [Harness.SendProgram]
// can deliver events to the [tea.Program] in a different order than the
// calls were made.
//
// Pass [WithTypingDelay] to pause after the key press of this call.
func (h *Harness) Key(key string, opts ...Option) *Harness {
	h.tb.Helper()
	meta := InputMeta{Kind: InputKindKey, Key: key}
	defer h.hook(HookInput, meta)()
	h.annotate(&inputNote{InputMeta: meta})
	h.check(h.session.Key(key, h.keyDelay(opts)))
	return h
}

// keyDelay returns the harness's [WithTypingDelay], overridden by any in
// opts.
func (h *Harness) keyDelay(opts []Option) reel.KeyOption {
	h.tb.Helper()
	return reel.KeyDelay(collectOptions(h.tb, h.mergedOpts(opts...)...).typingDelay)
}

// KeyUp sends an up-arrow key press to the terminal emulator.
func (h *Harness) KeyUp(opts ...Option) *Harness { return h.Key("up", opts...) }

// KeyDown sends a down-arrow key press to the terminal emulator.
func (h *Harness) KeyDown(opts ...Option) *Harness { return h.Key("down", opts...) }

// KeyLeft sends a left-arrow key press to the terminal emulator.
func (h *Harness) KeyLeft(opts ...Option) *Harness { return h.Key("left", opts...) }

// KeyRight sends a right-arrow key press to the terminal emulator.
func (h *Harness) KeyRight(opts ...Option) *Harness { return h.Key("right", opts...) }

// KeyEsc sends an escape key press to the terminal emulator.
func (h *Harness) KeyEsc(opts ...Option) *Harness { return h.Key("esc", opts...) }

// KeyDelete sends a delete (forward-delete) key press to the terminal emulator.
func (h *Harness) KeyDelete(opts ...Option) *Harness { return h.Key("delete", opts...) }

// KeyBackspace sends a backspace key press to the terminal emulator.
func (h *Harness) KeyBackspace(opts ...Option) *Harness { return h.Key("backspace", opts...) }

// KeyCtrlC sends ctrl+c to the terminal emulator.
func (h *Harness) KeyCtrlC(opts ...Option) *Harness { return h.Key("ctrl+c", opts...) }

// KeyCtrlD sends ctrl+d to the terminal emulator.
func (h *Harness) KeyCtrlD(opts ...Option) *Harness { return h.Key("ctrl+d", opts...) }

// Mouse forwards a mouse event through the terminal emulator. The running
// [tea.Program] must have enabled a mouse tracking mode and, typically, SGR
// mouse encoding. Otherwise the emulator discards the event, matching a
// real terminal.
func (h *Harness) Mouse(action vt.MouseAction, button vt.MouseButton, x, y int) *Harness {
	h.tb.Helper()
	meta := InputMeta{Kind: InputKindMouse, Mouse: mouseName(action), Button: buttonName(button)}
	defer h.hook(HookInput, meta)()
	h.annotate(&inputNote{InputMeta: meta, At: &cell{Row: y, Col: x}})
	h.check(h.session.Mouse(action, button, x, y))
	return h
}

// MouseClick simulates a full press and release at (x, y).
func (h *Harness) MouseClick(button vt.MouseButton, x, y int) *Harness {
	h.tb.Helper()
	meta := InputMeta{Kind: InputKindClick, Button: buttonName(button)}
	defer h.hook(HookInput, meta)()
	h.annotate(&inputNote{InputMeta: meta, At: &cell{Row: y, Col: x}})
	h.check(h.session.Mouse(vt.MousePress, button, x, y))
	h.check(h.session.Mouse(vt.MouseRelease, button, x, y))
	return h
}

// LeftClick is [Harness.MouseClick] with the left mouse button.
func (h *Harness) LeftClick(x, y int) *Harness { return h.MouseClick(vt.MouseLeft, x, y) }

// MiddleClick is [Harness.MouseClick] with the middle mouse button.
func (h *Harness) MiddleClick(x, y int) *Harness { return h.MouseClick(vt.MouseMiddle, x, y) }

// RightClick is [Harness.MouseClick] with the right mouse button.
func (h *Harness) RightClick(x, y int) *Harness { return h.MouseClick(vt.MouseRight, x, y) }

// MouseDrag simulates a drag from (x1, y1) to (x2, y2).
func (h *Harness) MouseDrag(button vt.MouseButton, x1, y1, x2, y2 int) *Harness {
	h.tb.Helper()
	meta := InputMeta{Kind: InputKindDrag, Button: buttonName(button)}
	defer h.hook(HookInput, meta)()
	h.annotate(&inputNote{InputMeta: meta, At: &cell{Row: y1, Col: x1}, To: &cell{Row: y2, Col: x2}})
	h.check(h.session.Mouse(vt.MousePress, button, x1, y1))
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
		h.check(h.session.Mouse(vt.MouseMotion, button, x, y))
	}
	h.check(h.session.Mouse(vt.MouseRelease, button, x2, y2))
	return h
}

// LeftDrag is [Harness.MouseDrag] with the left mouse button.
func (h *Harness) LeftDrag(x1, y1, x2, y2 int) *Harness {
	return h.MouseDrag(vt.MouseLeft, x1, y1, x2, y2)
}

// Progress returns the latest OSC 9;4 progress report.
func (h *Harness) Progress() vt.Progress { return h.session.Terminal().Progress() }

// Notifications returns desktop notifications from OSC 9 and OSC 777.
func (h *Harness) Notifications() []vt.Notification { return h.session.Terminal().Notifications() }

// Clipboard returns the last text the program wrote to the clipboard.
func (h *Harness) Clipboard() string { return h.session.Terminal().Clipboard() }

// SetClipboard sets the host clipboard returned for OSC 52 reads.
func (h *Harness) SetClipboard(text string) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.annotate(&terminalNote{Action: "clipboard", Text: text})
	h.session.Terminal().SetClipboard(text)
	return h
}

// WorkingDirectory returns the last OSC 7 working directory.
func (h *Harness) WorkingDirectory() string { return h.session.Terminal().WorkingDirectory() }

// Prompt returns the latest OSC 133 semantic prompt step.
func (h *Harness) Prompt() vt.Prompt { return h.session.Terminal().Prompt() }

// ResizeRequest returns the last CSI 8 t window-resize request.
// ok is false until the program asks. The harness is not resized.
func (h *Harness) ResizeRequest() (cols, rows int, ok bool) {
	return h.session.Terminal().ResizeRequest()
}

const programStatusMaxRecords = 256

// ProgramStatuses returns the current OSC 7501 records, sorted by id. App is
// copied from the nearest ancestor that set one. Each report replaces its
// record; at most 256 records are kept. The returned slice is a copy.
//
// Working and blocked records are removed when a shell prompt starts (OSC 133
// A). A full reset (RIS) removes every record.
//
// With [render.WithWindowChrome] in the profile's Render options (see
// [WithRecording]), [Harness.Image] and recordings draw the records in the
// title bar and as an outline around the window (see
// [render.WithProgramStatus]). Once any record has been reported,
// OSC 9;4 progress is no longer drawn, until a full reset, as the OSC 7501
// spec asks.
func (h *Harness) ProgramStatuses() []ansi.ProgramStatus {
	return programStatuses(h.session.Terminal())
}

func programStatuses(term *vt.Terminal) []ansi.ProgramStatus {
	in := term.ProgramStatuses()
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
