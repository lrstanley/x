// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"image"
	"image/color"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/vt"
)

// View returns the screen's text, which the harness's waits, assertions,
// and snapshots compare. Empty cells are spaces, so every line is
// [Harness.Width] wide and there are [Harness.Height] lines.
func (h *Harness) View() string {
	h.tb.Helper()
	return h.session.Terminal().Text()
}

// Focus sends the program a focus event if it enabled focus reporting.
func (h *Harness) Focus() *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.check(h.session.Focus(true))
	return h
}

// Blur sends the program a blur event if it enabled focus reporting.
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

// Bell returns the latest bell ([ansi.BEL]). Its Count is zero before the
// first.
func (h *Harness) Bell() vt.Bell { return h.session.Terminal().Bell() }

// Bounds returns the terminal's current screen bounds.
func (h *Harness) Bounds() image.Rectangle {
	w, height := h.session.Terminal().Size()
	return image.Rect(0, 0, w, height)
}

// IsAltScreen returns true if the terminal is in alt screen mode.
func (h *Harness) IsAltScreen() bool { return h.session.Terminal().AltScreen() }

// ModeSetting returns the setting of mode, or [ansi.ModeNotRecognized] for a
// mode the terminal does not track.
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

// Width returns the terminal width in cells.
func (h *Harness) Width() int {
	w, _ := h.session.Terminal().Size()
	return w
}

// Height returns the terminal height in cells.
func (h *Harness) Height() int {
	_, height := h.session.Terminal().Size()
	return height
}

// Dimensions returns the terminal's width and height in cells.
func (h *Harness) Dimensions() (width, height int) { return h.session.Terminal().Size() }

// Resize resizes the terminal, which sends the program a
// [tea.WindowSizeMsg].
func (h *Harness) Resize(width, height int) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.check(h.session.Resize(width, height))
	return h
}

// Paste pastes text into the terminal, bracketed if the program enabled
// bracketed paste.
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

// SetDefaultFgColor sets the foreground color used when the program sets none.
func (h *Harness) SetDefaultFgColor(c color.Color) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.check(h.session.SetDefaultForeground(c))
	return h
}

// SetDefaultBgColor sets the background color used when the program sets none.
func (h *Harness) SetDefaultBgColor(c color.Color) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.check(h.session.SetDefaultBackground(c))
	return h
}

// SetDefaultCursorColor sets the cursor color used when the program sets none.
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

// CursorVisible reports whether the cursor is shown (DECTCEM).
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

// ScrollUp scrolls the terminal's viewport n rows toward older scrollback, so
// [Harness.View], snapshots, and a window-chrome scrollbar show the older
// rows. The program is not told. The viewport stays put as the program
// writes more, until [Harness.ScrollDown] or [Harness.ScrollToBottom], or
// input from [Harness.Type], [Harness.Key], or [Harness.Paste] (see
// [WithScrollOnInput]).
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

// SetScrollbackSize sets the most scrollback lines the terminal keeps (see
// [reel.Session.SetScrollbackSize]).
func (h *Harness) SetScrollbackSize(maxLines int) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.check(h.session.SetScrollbackSize(maxLines))
	return h
}

// ClearScrollback clears the terminal's scrollback (see
// [reel.Session.ClearScrollback]).
func (h *Harness) ClearScrollback() *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.check(h.session.ClearScrollback())
	return h
}

// Type sends s to the terminal as key presses (see [reel.Session.Type]).
// Use [Harness.Key] for modifier combinations such as ctrl+a.
//
// Input goes through the terminal, so mixing it with [Harness.SendProgram]
// can deliver events to the program in a different order than the calls.
// Pass [reel.Delay] to pause after each key press of this call.
func (h *Harness) Type(s string, opts ...reel.InputOption) *Harness {
	h.tb.Helper()
	meta := InputMeta{Kind: InputKindType, Text: s}
	defer h.hook(HookInput, meta)()
	h.annotate(&inputNote{InputMeta: meta})
	h.check(h.session.Type(s, opts...))
	return h
}

// Key sends a single key press to the terminal, such as "ctrl+a", "enter",
// "shift+tab", or "space" (see [reel.Session.Key]).
// An unknown name fails the test. Like [Harness.Type], it can be reordered
// against [Harness.SendProgram]. Pass [reel.Delay] to pause after the key
// press, or [reel.Repeat] to press it more than once.
func (h *Harness) Key(key string, opts ...reel.InputOption) *Harness {
	h.tb.Helper()
	meta := InputMeta{Kind: InputKindKey, Key: key}
	defer h.hook(HookInput, meta)()
	h.annotate(&inputNote{InputMeta: meta})
	h.check(h.session.Key(key, opts...))
	return h
}

// KeyUp sends an up-arrow key press to the terminal emulator.
func (h *Harness) KeyUp(opts ...reel.InputOption) *Harness { return h.Key("up", opts...) }

// KeyDown sends a down-arrow key press to the terminal emulator.
func (h *Harness) KeyDown(opts ...reel.InputOption) *Harness { return h.Key("down", opts...) }

// KeyLeft sends a left-arrow key press to the terminal emulator.
func (h *Harness) KeyLeft(opts ...reel.InputOption) *Harness { return h.Key("left", opts...) }

// KeyRight sends a right-arrow key press to the terminal emulator.
func (h *Harness) KeyRight(opts ...reel.InputOption) *Harness { return h.Key("right", opts...) }

// KeyEsc sends an escape key press to the terminal emulator.
func (h *Harness) KeyEsc(opts ...reel.InputOption) *Harness { return h.Key("esc", opts...) }

// KeyDelete sends a delete (forward-delete) key press to the terminal emulator.
func (h *Harness) KeyDelete(opts ...reel.InputOption) *Harness { return h.Key("delete", opts...) }

// KeyBackspace sends a backspace key press to the terminal emulator.
func (h *Harness) KeyBackspace(opts ...reel.InputOption) *Harness {
	return h.Key("backspace", opts...)
}

// KeyCtrlC sends ctrl+c to the terminal emulator.
func (h *Harness) KeyCtrlC(opts ...reel.InputOption) *Harness { return h.Key("ctrl+c", opts...) }

// KeyCtrlD sends ctrl+d to the terminal emulator.
func (h *Harness) KeyCtrlD(opts ...reel.InputOption) *Harness { return h.Key("ctrl+d", opts...) }

// Mouse sends a mouse event through the terminal. As with a real terminal,
// it is dropped unless the program enabled mouse tracking.
func (h *Harness) Mouse(action vt.MouseAction, button vt.MouseButton, x, y int) *Harness {
	h.tb.Helper()
	meta := InputMeta{Kind: InputKindMouse, Mouse: mouseName(action), Button: buttonName(button)}
	defer h.hook(HookInput, meta)()
	h.annotate(&inputNote{InputMeta: meta, At: &cell{Row: y, Col: x}})
	h.check(h.session.Mouse(action, button, x, y))
	return h
}

// MouseClick sends a press and release at (x, y) (see
// [reel.Session.Click]). Pass [reel.Repeat] for a double click.
func (h *Harness) MouseClick(button vt.MouseButton, x, y int, opts ...reel.InputOption) *Harness {
	h.tb.Helper()
	meta := InputMeta{Kind: InputKindClick, Button: buttonName(button)}
	defer h.hook(HookInput, meta)()
	h.annotate(&inputNote{InputMeta: meta, At: &cell{Row: y, Col: x}})
	h.check(h.session.Click(button, x, y, opts...))
	return h
}

// LeftClick is [Harness.MouseClick] with the left mouse button.
func (h *Harness) LeftClick(x, y int, opts ...reel.InputOption) *Harness {
	return h.MouseClick(vt.MouseLeft, x, y, opts...)
}

// MiddleClick is [Harness.MouseClick] with the middle mouse button.
func (h *Harness) MiddleClick(x, y int, opts ...reel.InputOption) *Harness {
	return h.MouseClick(vt.MouseMiddle, x, y, opts...)
}

// RightClick is [Harness.MouseClick] with the right mouse button.
func (h *Harness) RightClick(x, y int, opts ...reel.InputOption) *Harness {
	return h.MouseClick(vt.MouseRight, x, y, opts...)
}

// MouseDrag sends a press at (x1, y1), motion through each cell of the
// straight line to (x2, y2), and a release there (see
// [reel.Session.Drag]). Pass [reel.Delay] to pause after each step.
func (h *Harness) MouseDrag(button vt.MouseButton, x1, y1, x2, y2 int, opts ...reel.InputOption) *Harness {
	h.tb.Helper()
	meta := InputMeta{Kind: InputKindDrag, Button: buttonName(button)}
	defer h.hook(HookInput, meta)()
	h.annotate(&inputNote{InputMeta: meta, At: &cell{Row: y1, Col: x1}, To: &cell{Row: y2, Col: x2}})
	h.check(h.session.Drag(button, image.Pt(x1, y1), image.Pt(x2, y2), opts...))
	return h
}

// LeftDrag is [Harness.MouseDrag] with the left mouse button.
func (h *Harness) LeftDrag(x1, y1, x2, y2 int, opts ...reel.InputOption) *Harness {
	return h.MouseDrag(vt.MouseLeft, x1, y1, x2, y2, opts...)
}

// Progress returns the latest OSC 9;4 progress report.
func (h *Harness) Progress() vt.Progress { return h.session.Terminal().Progress() }

// Notifications returns desktop notifications from OSC 9 and OSC 777.
func (h *Harness) Notifications() []vt.Notification { return h.session.Terminal().Notifications() }

// Clipboard returns the program's latest clipboard write.
func (h *Harness) Clipboard() vt.ClipboardWrite { return h.session.Terminal().Clipboard() }

// SetClipboard sets the host clipboard returned for OSC 52 reads.
func (h *Harness) SetClipboard(text string) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.annotate(&terminalNote{Action: "clipboard"})
	h.session.Terminal().SetClipboard(text)
	return h
}

// WorkingDirectory returns the last OSC 7 working directory.
func (h *Harness) WorkingDirectory() string { return h.session.Terminal().WorkingDirectory() }

// Prompt returns the latest OSC 133 semantic prompt step.
func (h *Harness) Prompt() vt.Prompt { return h.session.Terminal().Prompt() }

// ResizeRequest returns the last CSI 8 t window-resize request, with ok
// false until the program asks. The terminal is not resized.
func (h *Harness) ResizeRequest() (cols, rows int, ok bool) {
	return h.session.Terminal().ResizeRequest()
}

// ProgramStatuses returns the current OSC 7501 records (see
// [vt.Terminal.ProgramStatuses]). At most 256 are kept.
func (h *Harness) ProgramStatuses() []vt.ProgramStatus {
	return h.session.Terminal().ProgramStatuses()
}
