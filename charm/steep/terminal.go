// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"image/color"
	"io"
	"slices"
	"sync"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
)

var (
	_ io.Reader = (*emulator)(nil) // [tea.WithInput]
	_ io.Writer = (*emulator)(nil) // [tea.WithOutput]
	_ Viewable  = (*emulator)(nil) // [Viewable]
)

type emulator struct {
	mu             sync.RWMutex
	vt             *vt.Emulator
	inputCloseOnce sync.Once
	focused        bool
}

func newEmulator(width, height int) *emulator {
	emu := &emulator{
		vt:      vt.NewEmulator(width, height),
		focused: true,
	}

	go func() {
		emu.mu.Lock()
		emu.vt.Resize(width, height)
		emu.vt.Focus()
		emu.mu.Unlock()
	}()

	return emu
}

func (e *emulator) Read(p []byte) (n int, err error) {
	return e.vt.Read(p)
}

func (e *emulator) Write(p []byte) (n int, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.vt.Write(p)
}

func (e *emulator) View() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.vt.Render()
}

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

// TerminalFocus sends the terminal a focus event if focus events mode is enabled.
// This is the opposite of [Harness.Blur].
func (h *Harness) TerminalFocus() *Harness {
	h.emulator.mu.Lock()
	h.emulator.vt.Focus()
	h.emulator.focused = true
	h.emulator.mu.Unlock()
	return h
}

// TerminalBlur sends the terminal a blur event if focus events mode is enabled.
// This is the opposite of [Harness.Focus].
func (h *Harness) TerminalBlur() *Harness {
	h.emulator.mu.Lock()
	h.emulator.vt.Blur()
	h.emulator.focused = false
	h.emulator.mu.Unlock()
	return h
}

// TerminalView renders the terminals current screen as a string.
func (h *Harness) TerminalView() string {
	h.emulator.mu.RLock()
	defer h.emulator.mu.RUnlock()
	return h.emulator.vt.Render()
}

// TerminalBounds returns the terminals current screen bounds.
func (h *Harness) TerminalBounds() uv.Rectangle {
	h.emulator.mu.RLock()
	defer h.emulator.mu.RUnlock()
	return h.emulator.vt.Bounds()
}

// IsAltScreen returns true if the terminal is in alt screen mode.
func (h *Harness) IsAltScreen() bool {
	h.emulator.mu.RLock()
	defer h.emulator.mu.RUnlock()
	return h.emulator.vt.IsAltScreen()
}

// TerminalWidth returns the terminals current width.
func (h *Harness) TerminalWidth() int {
	h.emulator.mu.RLock()
	defer h.emulator.mu.RUnlock()
	return h.emulator.vt.Width()
}

// TerminalHeight returns the terminals current height.
func (h *Harness) TerminalHeight() int {
	h.emulator.mu.RLock()
	defer h.emulator.mu.RUnlock()
	return h.emulator.vt.Height()
}

// TerminalDimensions returns the terminals current dimensions.
func (h *Harness) TerminalDimensions() (width, height int) {
	h.emulator.mu.RLock()
	defer h.emulator.mu.RUnlock()
	return h.emulator.vt.Width(), h.emulator.vt.Height()
}

// TerminalResize resizes the terminal to the given width and height. This should
// result in a [tea.WindowSizeMsg] being sent to the program.
func (h *Harness) TerminalResize(width, height int) *Harness {
	h.emulator.mu.Lock()
	h.emulator.vt.Resize(width, height)
	h.emulator.mu.Unlock()
	return h
}

// TerminalPaste paste text into the terminal. If bracketed paste mode is enabled,
// the text is bracketed with the appropriate escape sequences.
func (h *Harness) TerminalPaste(text string) *Harness {
	h.emulator.mu.Lock()
	h.emulator.vt.Paste(text)
	h.emulator.mu.Unlock()
	return h
}

// TerminalFgColor returns the terminal emulator's foreground color.
func (h *Harness) TerminalFgColor() color.Color {
	h.emulator.mu.RLock()
	defer h.emulator.mu.RUnlock()
	return h.emulator.vt.ForegroundColor()
}

// TerminalBgColor returns the terminal emulator's background color.
func (h *Harness) TerminalBgColor() color.Color {
	h.emulator.mu.RLock()
	defer h.emulator.mu.RUnlock()
	return h.emulator.vt.BackgroundColor()
}

// TerminalCursorColor returns the terminal emulator's cursor color.
func (h *Harness) TerminalCursorColor() color.Color {
	h.emulator.mu.RLock()
	defer h.emulator.mu.RUnlock()
	return h.emulator.vt.CursorColor()
}

// SetTerminalFgColor sets the terminal emulator foreground color.
func (h *Harness) SetTerminalFgColor(c color.Color) *Harness {
	h.emulator.mu.Lock()
	h.emulator.vt.SetForegroundColor(c)
	h.emulator.mu.Unlock()
	return h
}

// SetTerminalBgColor sets the terminal emulator background color.
func (h *Harness) SetTerminalBgColor(c color.Color) *Harness {
	h.emulator.mu.Lock()
	h.emulator.vt.SetBackgroundColor(c)
	h.emulator.mu.Unlock()
	return h
}

// SetTerminalCursorColor sets the terminal emulator cursor color.
func (h *Harness) SetTerminalCursorColor(c color.Color) *Harness {
	h.emulator.mu.Lock()
	h.emulator.vt.SetCursorColor(c)
	h.emulator.mu.Unlock()
	return h
}

// SetDefaultTerminalFgColor sets default foreground color used when none is
// specified.
func (h *Harness) SetDefaultTerminalFgColor(c color.Color) *Harness {
	h.emulator.mu.Lock()
	h.emulator.vt.SetDefaultForegroundColor(c)
	h.emulator.mu.Unlock()
	return h
}

// SetDefaultTerminalBgColor sets default background color used when none is
// specified.
func (h *Harness) SetDefaultTerminalBgColor(c color.Color) *Harness {
	h.emulator.mu.Lock()
	h.emulator.vt.SetDefaultBackgroundColor(c)
	h.emulator.mu.Unlock()
	return h
}

// SetDefaultTerminalCursorColor sets default cursor color used when none is specified.
func (h *Harness) SetDefaultTerminalCursorColor(c color.Color) *Harness {
	h.emulator.mu.Lock()
	h.emulator.vt.SetDefaultCursorColor(c)
	h.emulator.mu.Unlock()
	return h
}

// TerminalCursorPosition returns the terminal emulator cursor position.
func (h *Harness) TerminalCursorPosition() uv.Position {
	h.emulator.mu.RLock()
	defer h.emulator.mu.RUnlock()
	return h.emulator.vt.CursorPosition()
}

// TerminalScrollback returns a copy of the terminals scrollback lines (oldest first).
// It is nil in alternate screen mode. An empty non-nil slice means the scrollback
// buffer exists but has no lines yet.
func (h *Harness) TerminalScrollback() []uv.Line {
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

// TerminalScrollbackCount returns the count of scrollback lines.
func (h *Harness) TerminalScrollbackCount() int {
	h.emulator.mu.RLock()
	defer h.emulator.mu.RUnlock()
	return h.emulator.vt.ScrollbackLen()
}

// SetTerminalScrollbackSize sets maximum scrollback lines retained on the terminals
// screen buffer.
func (h *Harness) SetTerminalScrollbackSize(maxLines int) *Harness {
	h.emulator.mu.Lock()
	h.emulator.vt.SetScrollbackSize(maxLines)
	h.emulator.mu.Unlock()
	return h
}

// ClearTerminalScrollback clears all scrollback history on the screen buffer.
func (h *Harness) ClearTerminalScrollback() *Harness {
	h.emulator.mu.Lock()
	h.emulator.vt.ClearScrollback()
	h.emulator.mu.Unlock()
	return h
}

// TerminalType sends a sequence of key press messages to the terminal emulator
// itself. This is designed for providing regular text input, not more complex
// key combinations (ctrl-key, alt-key, etc).
//
// Note that as this sends through the emulator, if paired with [Harness.Send],
// The order of events that the [tea.Program] receives may not be the same as
// order of invocation.
func (h *Harness) TerminalType(s string) *Harness {
	h.tb.Helper()
	for _, r := range s {
		h.emulator.mu.Lock()
		h.emulator.vt.SendKey(vt.KeyPressEvent{Code: r, Text: string(r)})
		h.emulator.mu.Unlock()
	}
	return h
}

// TerminalKey sends a single key press message to the terminal emulator itself,
// e.g. "ctrl+a", "enter", "space", etc.
//
// Note that as this sends through the emulator, if paired with [Harness.Send],
// The order of events that the [tea.Program] receives may not be the same as
// order of invocation.
func (h *Harness) TerminalKey(key string) *Harness {
	h.emulator.mu.Lock()
	h.emulator.vt.SendKey(mapKeyToEvent(key))
	h.emulator.mu.Unlock()
	return h
}
