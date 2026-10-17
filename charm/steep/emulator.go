// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"image"
	"image/color"
	"io"
	"slices"
	"sync"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/lrstanley/x/charm/steep/internal/vtpipe"
	"github.com/lrstanley/x/charm/steep/snapshot"
)

var (
	_ io.Reader = (*emulator)(nil) // [tea.WithInput]
	_ io.Writer = (*emulator)(nil) // [tea.WithOutput]
)

type emulator struct {
	mu             sync.RWMutex // serializes vt I/O with pumpSink and Harness vt helpers
	vt             *vt.Emulator
	pipe           *vtpipe.Pipe // program stdin/stdout bridge to vt (see [vtpipe.Pipe])
	inputCloseOnce sync.Once    // closes the vt input pipe at most once (unblocks [tea.Program] shutdown)
	shutdownOnce   sync.Once    // runs Close teardown once

	// Purely state tracking fields.
	trackMu     sync.RWMutex // Tracked mirror fields; separate from mu so vt callbacks cannot deadlock callers holding mu around vt writes
	focused     bool         // Last known focus state for TerminalFocus/TerminalBlur.
	title       string
	altScreen   bool
	modes       ansi.Modes
	cursorPos   image.Point
	cursorColor color.Color
	bgColor     color.Color
	fgColor     color.Color
	lastBellAt  time.Time // Zero until first [vt.Callbacks.Bell].
	// statuses is oldest-update first. At most [programStatusMaxRecords].
	statuses []ansi.ProgramStatus
}

// vtSink serializes writes from the program output pump with other harness vt I/O.
type vtSink struct{ e *emulator }

func (w vtSink) Write(p []byte) (int, error) {
	w.e.mu.Lock()
	defer w.e.mu.Unlock()
	return w.e.vt.Write(p)
}

func newEmulator(width, height int) *emulator {
	emu := &emulator{
		vt:      vt.NewEmulator(width, height),
		focused: true,

		// Mirror defaults from: https://github.com/charmbracelet/x/blob/main/vt/mode.go
		modes: ansi.Modes{
			ansi.ModeCursorKeys: ansi.ModeReset,
			ansi.ModeOrigin:     ansi.ModeReset,
			ansi.ModeAutoWrap:   ansi.ModeSet,
			ansi.ModeMouseX10:   ansi.ModeReset,
			// LNM off is VT-correct, but TTY-backed apps usually see LF as CRLF due to
			// termios ONLCR; Lip Gloss/Bubble Tea emit '\n' only. Enable LNM so LF also
			// resets column and Render matches real-terminal layout.
			ansi.ModeLineFeedNewLine:     ansi.ModeSet,
			ansi.ModeTextCursorEnable:    ansi.ModeSet,
			ansi.ModeNumericKeypad:       ansi.ModeReset,
			ansi.ModeLeftRightMargin:     ansi.ModeReset,
			ansi.ModeMouseNormal:         ansi.ModeReset,
			ansi.ModeMouseHighlight:      ansi.ModeReset,
			ansi.ModeMouseButtonEvent:    ansi.ModeReset,
			ansi.ModeMouseAnyEvent:       ansi.ModeReset,
			ansi.ModeFocusEvent:          ansi.ModeReset,
			ansi.ModeMouseExtSgr:         ansi.ModeReset,
			ansi.ModeAltScreen:           ansi.ModeReset,
			ansi.ModeSaveCursor:          ansi.ModeReset,
			ansi.ModeAltScreenSaveCursor: ansi.ModeReset,
			ansi.ModeBracketedPaste:      ansi.ModeReset,
		},
	}

	emu.vt.SetCallbacks(vt.Callbacks{
		Bell: func() {
			emu.trackMu.Lock()
			emu.lastBellAt = time.Now()
			emu.trackMu.Unlock()
		},
		Title: func(title string) {
			emu.trackMu.Lock()
			emu.title = title
			emu.trackMu.Unlock()
		},
		AltScreen: func(altScreen bool) {
			emu.trackMu.Lock()
			emu.altScreen = altScreen
			emu.trackMu.Unlock()
		},
		EnableMode: func(mode ansi.Mode) {
			emu.trackMu.Lock()
			emu.modes.Set(mode)
			emu.trackMu.Unlock()
		},
		DisableMode: func(mode ansi.Mode) {
			emu.trackMu.Lock()
			emu.modes.Reset(mode)
			emu.trackMu.Unlock()
		},
		CursorPosition: func(_, next uv.Position) {
			emu.trackMu.Lock()
			emu.cursorPos = next
			emu.trackMu.Unlock()
		},
		CursorColor: func(color color.Color) {
			emu.trackMu.Lock()
			emu.cursorColor = color
			emu.trackMu.Unlock()
		},
		BackgroundColor: func(color color.Color) {
			emu.trackMu.Lock()
			emu.bgColor = color
			emu.trackMu.Unlock()
		},
		ForegroundColor: func(color color.Color) {
			emu.trackMu.Lock()
			emu.fgColor = color
			emu.trackMu.Unlock()
		},
	})
	emu.vt.RegisterOscHandler(7501, emu.handleProgramStatus)
	emu.vt.RegisterOscHandler(133, emu.handleShellPrompt)
	// Return false so the emulator's own RIS handler still runs. Last
	// registered wins only when the handler reports the sequence consumed.
	emu.vt.RegisterEscHandler('c', func() bool {
		emu.trackMu.Lock()
		emu.statuses = nil
		emu.trackMu.Unlock()
		return false
	})

	emu.pipe = vtpipe.New(
		vtSink{e: emu},
		emu.vt,
		vtpipe.WithAfterOutgoingClosed(func() { emu.closeInput() }),
	)

	emu.mu.Lock()
	emu.vt.Resize(width, height)
	_, _ = emu.vt.WriteString(ansi.SetModeLineFeedNewLine)
	emu.vt.Focus()
	emu.mu.Unlock()

	return emu
}

// snapshot produces a snapshot of the current terminal state.
func (e *emulator) snapshot(tb testing.TB, opts ...snapshot.Option) *snapshot.ScreenSnapshot {
	tb.Helper()
	e.mu.RLock()
	defer e.mu.RUnlock()
	e.trackMu.RLock()
	defer e.trackMu.RUnlock()

	visible, style, blink := e.cursorLocked()
	snap := &snapshot.ScreenSnapshot{
		Title:         e.title,
		Rows:          e.vt.Height(),
		Cols:          e.vt.Width(),
		AltScreen:     e.altScreen,
		Focused:       e.focused,
		ProgramStatus: snapshotProgramStatuses(e.programStatusesLocked()),
		Cursor: snapshot.Cursor{
			Position: snapshot.Position{
				X: e.cursorPos.X,
				Y: e.cursorPos.Y,
			},
			Visible: visible,
			Blink:   blink,
			Style:   style,
			Color:   snapshot.Color{Color: e.cursorColor},
		},
		BgColor: snapshot.Color{Color: e.bgColor},
		FgColor: snapshot.Color{Color: e.fgColor},
	}
	snap.WithScreenBuffer(tb, snapshot.AsScreenBuffer(tb, e.vt.Render(), opts...))
	return snap
}

func (e *emulator) Close() {
	e.shutdownOnce.Do(func() {
		_ = e.pipe.Close()
	})
}

func (e *emulator) Read(p []byte) (n int, err error) {
	return e.pipe.Read(p)
}

func (e *emulator) Write(p []byte) (n int, err error) {
	return e.pipe.Write(p)
}

// closeInput closes the vt input pipe so [tea.Program] read loops can drain during shutdown.
func (e *emulator) closeInput() {
	e.inputCloseOnce.Do(func() {
		switch input := e.vt.InputPipe().(type) {
		case interface{ CloseWithError(error) error }:
			_ = input.CloseWithError(io.EOF)
		case io.Closer:
			_ = input.Close()
		}
	})
}

// View returns the current VT screen buffer ([vt.Emulator.Render]). [Harness] wait,
// assert, and snapshot helpers sample this rendered output.
//
// Empty cells are spaces, so each line is the terminal width and a blank row is
// a line of spaces. [Harness.Width] and [Harness.Height] match that grid. Size
// the terminal with [WithWindowSize] or [Harness.Resize] when the text under
// test should fill the window.
func (h *Harness) View() string {
	h.tb.Helper()
	h.emulator.mu.RLock()
	defer h.emulator.mu.RUnlock()
	return h.emulator.vt.Render()
}

// Focus sends the terminal a focus event if focus events mode is enabled. This is
// the opposite of [Harness.Blur].
func (h *Harness) Focus() *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.emulator.mu.Lock()
	h.emulator.vt.Focus()
	h.emulator.mu.Unlock()
	h.emulator.trackMu.Lock()
	h.emulator.focused = true
	h.emulator.trackMu.Unlock()
	return h
}

// Blur sends the terminal a blur event if focus events mode is enabled. This is
// the opposite of [Harness.Focus].
func (h *Harness) Blur() *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.emulator.mu.Lock()
	h.emulator.vt.Blur()
	h.emulator.mu.Unlock()
	h.emulator.trackMu.Lock()
	h.emulator.focused = false
	h.emulator.trackMu.Unlock()
	return h
}

// IsFocused reports the last known terminal focus state.
func (h *Harness) IsFocused() bool {
	h.emulator.trackMu.RLock()
	defer h.emulator.trackMu.RUnlock()
	return h.emulator.focused
}

// Title returns the last known window title.
func (h *Harness) Title() string {
	h.emulator.trackMu.RLock()
	defer h.emulator.trackMu.RUnlock()
	return h.emulator.title
}

// LastBellAt returns the wall clock time of the most recent bell ([ansi.BEL])
// processed by the VT emulator. It is the zero [time.Time] until the first bell.
func (h *Harness) LastBellAt() time.Time {
	h.emulator.trackMu.RLock()
	defer h.emulator.trackMu.RUnlock()
	return h.emulator.lastBellAt
}

// Bounds returns the terminals current screen bounds.
func (h *Harness) Bounds() uv.Rectangle {
	h.emulator.mu.RLock()
	defer h.emulator.mu.RUnlock()
	return h.emulator.vt.Bounds()
}

// IsAltScreen returns true if the terminal is in alt screen mode.
func (h *Harness) IsAltScreen() bool {
	h.emulator.trackMu.RLock()
	defer h.emulator.trackMu.RUnlock()
	return h.emulator.altScreen
}

// ModeSetting returns the tracked ANSI mode setting. An absent mode yields
// [ansi.ModeNotRecognized].
func (h *Harness) ModeSetting(mode ansi.Mode) ansi.ModeSetting {
	h.emulator.trackMu.RLock()
	defer h.emulator.trackMu.RUnlock()
	return h.emulator.modes.Get(mode)
}

// Width returns the terminal width in cells. This matches [Harness.View], which
// pads each line out to this width.
func (h *Harness) Width() int {
	h.emulator.mu.RLock()
	defer h.emulator.mu.RUnlock()
	return h.emulator.vt.Width()
}

// Height returns the terminals current height in cells (matches [Harness.View]
// row count).
func (h *Harness) Height() int {
	h.emulator.mu.RLock()
	defer h.emulator.mu.RUnlock()
	return h.emulator.vt.Height()
}

// Dimensions returns the terminals width and height in cells.
func (h *Harness) Dimensions() (width, height int) {
	h.emulator.mu.RLock()
	defer h.emulator.mu.RUnlock()
	return h.emulator.vt.Width(), h.emulator.vt.Height()
}

// Resize resizes the terminal to the given width and height. This should result
// in a [tea.WindowSizeMsg] being sent to the [tea.Program].
func (h *Harness) Resize(width, height int) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.emulator.mu.Lock()
	h.emulator.vt.Resize(width, height)
	h.emulator.mu.Unlock()
	return h
}

// Paste paste text into the terminal. If bracketed paste mode is enabled, the
// text is bracketed with the appropriate escape sequences.
func (h *Harness) Paste(text string) *Harness {
	h.tb.Helper()
	defer h.hook(HookInput, InputMeta{Kind: "paste", Text: text})()
	h.emulator.mu.Lock()
	h.emulator.vt.Paste(text)
	h.emulator.mu.Unlock()
	return h
}

// FgColor returns the terminal emulator's foreground color.
func (h *Harness) FgColor() color.Color {
	h.emulator.trackMu.RLock()
	defer h.emulator.trackMu.RUnlock()
	return h.emulator.fgColor
}

// BgColor returns the terminal emulator's background color.
func (h *Harness) BgColor() color.Color {
	h.emulator.trackMu.RLock()
	defer h.emulator.trackMu.RUnlock()
	return h.emulator.bgColor
}

// CursorColor returns the terminal emulator's cursor color.
func (h *Harness) CursorColor() color.Color {
	h.emulator.trackMu.RLock()
	defer h.emulator.trackMu.RUnlock()
	return h.emulator.cursorColor
}

// SetFgColor sets the terminal emulator foreground color.
func (h *Harness) SetFgColor(c color.Color) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.emulator.mu.Lock()
	h.emulator.vt.SetForegroundColor(c)
	h.emulator.mu.Unlock()
	return h
}

// SetBgColor sets the terminal emulator background color.
func (h *Harness) SetBgColor(c color.Color) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.emulator.mu.Lock()
	h.emulator.vt.SetBackgroundColor(c)
	h.emulator.mu.Unlock()
	return h
}

// SetCursorColor sets the terminal emulator cursor color.
func (h *Harness) SetCursorColor(c color.Color) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.emulator.mu.Lock()
	h.emulator.vt.SetCursorColor(c)
	h.emulator.mu.Unlock()
	return h
}

// SetDefaultFgColor sets default foreground color used when none is specified.
func (h *Harness) SetDefaultFgColor(c color.Color) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.emulator.mu.Lock()
	h.emulator.vt.SetDefaultForegroundColor(c)
	h.emulator.mu.Unlock()
	return h
}

// SetDefaultBgColor sets default background color used when none is specified.
func (h *Harness) SetDefaultBgColor(c color.Color) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.emulator.mu.Lock()
	h.emulator.vt.SetDefaultBackgroundColor(c)
	h.emulator.mu.Unlock()
	return h
}

// SetDefaultCursorColor sets default cursor color used when none is specified.
func (h *Harness) SetDefaultCursorColor(c color.Color) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.emulator.mu.Lock()
	h.emulator.vt.SetDefaultCursorColor(c)
	h.emulator.mu.Unlock()
	return h
}

// CursorPosition returns the terminal emulator cursor position.
func (h *Harness) CursorPosition() uv.Position {
	h.emulator.trackMu.RLock()
	defer h.emulator.trackMu.RUnlock()
	return h.emulator.cursorPos
}

// CursorVisible reports whether the cursor is shown. Visibility follows DECTCEM.
func (h *Harness) CursorVisible() bool {
	h.emulator.mu.RLock()
	defer h.emulator.mu.RUnlock()
	return !h.emulator.vt.CursorHidden()
}

// CursorBlink reports whether the cursor style is blinking.
func (h *Harness) CursorBlink() bool {
	h.emulator.mu.RLock()
	defer h.emulator.mu.RUnlock()
	_, blink := h.emulator.vt.CursorStyle()
	return blink
}

// CursorStyle returns the current VT cursor shape.
func (h *Harness) CursorStyle() vt.CursorStyle {
	h.emulator.mu.RLock()
	defer h.emulator.mu.RUnlock()
	style, _ := h.emulator.vt.CursorStyle()
	return style
}

// cursorLocked reads the live cursor. The caller must hold [emulator.mu].
func (e *emulator) cursorLocked() (visible bool, style vt.CursorStyle, blink bool) {
	style, blink = e.vt.CursorStyle()
	return !e.vt.CursorHidden(), style, blink
}

// Scrollback returns a copy of the terminals scrollback lines (oldest first).
// It is nil in alternate screen mode. An empty non-nil slice means the scrollback
// buffer exists but has no lines yet.
func (h *Harness) Scrollback() []uv.Line {
	h.emulator.mu.RLock()
	defer h.emulator.mu.RUnlock()
	sb := h.emulator.vt.Scrollback()
	if sb == nil {
		return nil
	}
	src := sb.Lines()
	out := make([]uv.Line, len(src))
	for i := range src {
		out[i] = slices.Clone(src[i])
	}
	return out
}

// ScrollbackCount returns the count of scrollback lines.
func (h *Harness) ScrollbackCount() int {
	h.emulator.mu.RLock()
	defer h.emulator.mu.RUnlock()
	return h.emulator.vt.ScrollbackLen()
}

// SetScrollbackSize sets maximum scrollback lines retained on the terminals
// screen buffer.
func (h *Harness) SetScrollbackSize(maxLines int) *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.emulator.mu.Lock()
	h.emulator.vt.SetScrollbackSize(maxLines)
	h.emulator.mu.Unlock()
	return h
}

// ClearScrollback clears all scrollback history on the screen buffer.
func (h *Harness) ClearScrollback() *Harness {
	h.tb.Helper()
	defer h.hook(HookTerminal, nil)()
	h.emulator.mu.Lock()
	h.emulator.vt.ClearScrollback()
	h.emulator.mu.Unlock()
	return h
}

// Type sends a sequence of key press messages to the terminal emulator itself.
// This is designed for providing regular text input, not more complex key
// combinations (ctrl-key, alt-key, etc).
//
// Note that as this sends through the emulator, if paired with [Harness.SendProgram],
// The order of events that the [tea.Program] receives may not be the same as
// order of invocation.
func (h *Harness) Type(s string) *Harness {
	h.tb.Helper()
	defer h.hook(HookInput, InputMeta{Kind: "type", Text: s})()
	h.typeKeys(s)
	return h
}

func (h *Harness) typeKeys(s string) {
	for _, r := range s {
		h.emulator.mu.Lock()
		h.emulator.vt.SendKey(uv.KeyPressEvent{Code: r, Text: string(r)})
		h.emulator.mu.Unlock()
	}
}

// Key sends a single key press message to the terminal emulator itself, e.g.
// "ctrl+a", "enter", "space", etc.
//
// Note that as this sends through the emulator, if paired with [Harness.SendProgram],
// The order of events that the [tea.Program] receives may not be the same as
// order of invocation.
func (h *Harness) Key(key string) *Harness {
	h.tb.Helper()
	defer h.hook(HookInput, InputMeta{Kind: "key", Key: key})()
	h.key(key)
	return h
}

func (h *Harness) key(key string) {
	ev := mapKeyToEvent(key)
	if ev.Code == 0 && ev.Text != "" {
		h.typeKeys(ev.Text)
		return
	}
	h.emulator.mu.Lock()
	h.emulator.vt.SendKey(ev)
	h.emulator.mu.Unlock()
}

// KeyUp sends an up-arrow key press to the terminal emulator.
func (h *Harness) KeyUp() *Harness {
	h.tb.Helper()
	return h.Key("up")
}

// KeyDown sends a down-arrow key press to the terminal emulator.
func (h *Harness) KeyDown() *Harness {
	h.tb.Helper()
	return h.Key("down")
}

// KeyLeft sends a left-arrow key press to the terminal emulator.
func (h *Harness) KeyLeft() *Harness {
	h.tb.Helper()
	return h.Key("left")
}

// KeyRight sends a right-arrow key press to the terminal emulator.
func (h *Harness) KeyRight() *Harness {
	h.tb.Helper()
	return h.Key("right")
}

// KeyEsc sends an escape key press to the terminal emulator.
func (h *Harness) KeyEsc() *Harness {
	h.tb.Helper()
	return h.Key("esc")
}

// KeyDelete sends a delete (forward-delete) key press to the terminal emulator.
func (h *Harness) KeyDelete() *Harness {
	h.tb.Helper()
	return h.Key("delete")
}

// KeyBackspace sends a backspace key press to the terminal emulator.
func (h *Harness) KeyBackspace() *Harness {
	h.tb.Helper()
	return h.Key("backspace")
}

// KeyCtrlC sends ctrl+c to the terminal emulator.
func (h *Harness) KeyCtrlC() *Harness {
	h.tb.Helper()
	return h.Key("ctrl+c")
}

// KeyCtrlD sends ctrl+d to the terminal emulator.
func (h *Harness) KeyCtrlD() *Harness {
	h.tb.Helper()
	return h.Key("ctrl+d")
}

// Mouse forwards a mouse event through the terminal emulator. The running
// [tea.Program] must have enabled a mouse tracking mode and (typically) SGR mouse
// encoding; otherwise events are discarded by the emulator, matching real terminal
// behavior.
func (h *Harness) Mouse(m uv.MouseEvent) *Harness {
	h.tb.Helper()
	defer h.hook(HookInput, InputMeta{Kind: "mouse"})()
	h.mouse(m)
	return h
}

func (h *Harness) mouse(m uv.MouseEvent) {
	h.emulator.mu.Lock()
	h.emulator.vt.SendMouse(m)
	h.emulator.mu.Unlock()
}

// MouseClick simulates a full press and release at (x, y).
func (h *Harness) MouseClick(button uv.MouseButton, x, y int) *Harness {
	h.tb.Helper()
	defer h.hook(HookInput, InputMeta{Kind: "mouse_click"})()
	h.mouseClick(button, x, y)
	return h
}

func (h *Harness) mouseClick(button uv.MouseButton, x, y int) {
	h.mouse(uv.MouseClickEvent{X: x, Y: y, Button: button})
	h.mouse(uv.MouseReleaseEvent{X: x, Y: y, Button: button})
}

// LeftClick is [Harness.MouseClick] with the left mouse button.
func (h *Harness) LeftClick(x, y int) *Harness {
	h.tb.Helper()
	defer h.hook(HookInput, InputMeta{Kind: "left_click"})()
	h.mouseClick(uv.MouseLeft, x, y)
	return h
}

// MiddleClick is [Harness.MouseClick] with the middle mouse button.
func (h *Harness) MiddleClick(x, y int) *Harness {
	h.tb.Helper()
	defer h.hook(HookInput, InputMeta{Kind: "middle_click"})()
	h.mouseClick(uv.MouseMiddle, x, y)
	return h
}

// RightClick is [Harness.MouseClick] with the right mouse button.
func (h *Harness) RightClick(x, y int) *Harness {
	h.tb.Helper()
	defer h.hook(HookInput, InputMeta{Kind: "right_click"})()
	h.mouseClick(uv.MouseRight, x, y)
	return h
}

// MouseDrag simulates a drag from (x1, y1) to (x2, y2).
func (h *Harness) MouseDrag(button uv.MouseButton, x1, y1, x2, y2 int) *Harness {
	h.tb.Helper()
	defer h.hook(HookInput, InputMeta{Kind: "mouse_drag"})()
	h.mouseDrag(button, x1, y1, x2, y2)
	return h
}

func (h *Harness) mouseDrag(button uv.MouseButton, x1, y1, x2, y2 int) {
	h.mouse(uv.MouseClickEvent{X: x1, Y: y1, Button: button})

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
		h.mouse(uv.MouseMotionEvent{X: x, Y: y, Button: button})
	}

	h.mouse(uv.MouseReleaseEvent{X: x2, Y: y2, Button: button})
}

// LeftDrag is [Harness.MouseDrag] with the left mouse button.
func (h *Harness) LeftDrag(x1, y1, x2, y2 int) *Harness {
	h.tb.Helper()
	defer h.hook(HookInput, InputMeta{Kind: "left_drag"})()
	h.mouseDrag(uv.MouseLeft, x1, y1, x2, y2)
	return h
}
