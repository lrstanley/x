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
	_ io.Reader = (*terminal)(nil) // [tea.WithInput]
	_ io.Writer = (*terminal)(nil) // [tea.WithOutput]
	_ Viewable  = (*terminal)(nil) // [Viewable]
)

type terminal struct {
	mu sync.RWMutex
	vt *vt.Emulator
}

func (t *terminal) Read(p []byte) (n int, err error) {
	return t.vt.Read(p)
}

func (t *terminal) Write(p []byte) (n int, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.vt.Write(p)
}

func (t *terminal) View() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.vt.Render()
}

// Focus sends the terminal a focus event if focus events mode is enabled.
// This is the opposite of [Harness.Blur].
func (h *Harness) Focus() *Harness {
	h.terminal.mu.Lock()
	h.terminal.vt.Focus()
	h.terminal.mu.Unlock()
	return h
}

// Blur sends the terminal a blur event if focus events mode is enabled.
// This is the opposite of [Harness.Focus].
func (h *Harness) Blur() *Harness {
	h.terminal.mu.Lock()
	h.terminal.vt.Blur()
	h.terminal.mu.Unlock()
	return h
}

// TerminalView renders the terminal's current screen as a string.
func (h *Harness) TerminalView() string {
	h.terminal.mu.RLock()
	defer h.terminal.mu.RUnlock()
	return h.terminal.vt.Render()
}

// Bounds returns the terminal's current screen bounds.
func (h *Harness) Bounds() uv.Rectangle {
	h.terminal.mu.RLock()
	defer h.terminal.mu.RUnlock()
	return h.terminal.vt.Bounds()
}

// IsAltScreen returns true if the terminal is in alt screen mode.
func (h *Harness) IsAltScreen() bool {
	h.terminal.mu.RLock()
	defer h.terminal.mu.RUnlock()
	return h.terminal.vt.IsAltScreen()
}

// TerminalWidth returns the terminal's current width.
func (h *Harness) TerminalWidth() int {
	h.terminal.mu.RLock()
	defer h.terminal.mu.RUnlock()
	return h.terminal.vt.Width()
}

// TerminalHeight returns the terminal's current height.
func (h *Harness) TerminalHeight() int {
	h.terminal.mu.RLock()
	defer h.terminal.mu.RUnlock()
	return h.terminal.vt.Height()
}

// TerminalDimensions returns the terminal's current dimensions.
func (h *Harness) TerminalDimensions() (width, height int) {
	h.terminal.mu.RLock()
	defer h.terminal.mu.RUnlock()
	return h.terminal.vt.Width(), h.terminal.vt.Height()
}

// Resize resizes the terminal to the given width and height. This should result
// in a [tea.WindowSizeMsg] being sent to the program.
func (h *Harness) Resize(width, height int) *Harness {
	h.terminal.mu.Lock()
	h.terminal.vt.Resize(width, height)
	h.terminal.mu.Unlock()
	return h
}

// Paste text into the terminal. If bracketed paste mode is enabled,
// the text is bracketed with the appropriate escape sequences.
func (h *Harness) Paste(text string) *Harness {
	h.terminal.mu.Lock()
	h.terminal.vt.Paste(text)
	h.terminal.mu.Unlock()
	return h
}

// ForegroundColor returns the terminal emulator's foreground color.
func (h *Harness) ForegroundColor() color.Color {
	h.terminal.mu.RLock()
	defer h.terminal.mu.RUnlock()
	return h.terminal.vt.ForegroundColor()
}

// BackgroundColor returns the terminal emulator's background color.
func (h *Harness) BackgroundColor() color.Color {
	h.terminal.mu.RLock()
	defer h.terminal.mu.RUnlock()
	return h.terminal.vt.BackgroundColor()
}

// CursorColor returns the terminal emulator's cursor color.
func (h *Harness) CursorColor() color.Color {
	h.terminal.mu.RLock()
	defer h.terminal.mu.RUnlock()
	return h.terminal.vt.CursorColor()
}

// SetForegroundColor sets the terminal emulator foreground color.
func (h *Harness) SetForegroundColor(c color.Color) *Harness {
	h.terminal.mu.Lock()
	h.terminal.vt.SetForegroundColor(c)
	h.terminal.mu.Unlock()
	return h
}

// SetBackgroundColor sets the terminal emulator background color.
func (h *Harness) SetBackgroundColor(c color.Color) *Harness {
	h.terminal.mu.Lock()
	h.terminal.vt.SetBackgroundColor(c)
	h.terminal.mu.Unlock()
	return h
}

// SetCursorColor sets the terminal emulator cursor color.
func (h *Harness) SetCursorColor(c color.Color) *Harness {
	h.terminal.mu.Lock()
	h.terminal.vt.SetCursorColor(c)
	h.terminal.mu.Unlock()
	return h
}

// SetDefaultForegroundColor sets default foreground color used when none is
// specified.
func (h *Harness) SetDefaultForegroundColor(c color.Color) *Harness {
	h.terminal.mu.Lock()
	h.terminal.vt.SetDefaultForegroundColor(c)
	h.terminal.mu.Unlock()
	return h
}

// SetDefaultBackgroundColor sets default background color used when none is
// specified.
func (h *Harness) SetDefaultBackgroundColor(c color.Color) *Harness {
	h.terminal.mu.Lock()
	h.terminal.vt.SetDefaultBackgroundColor(c)
	h.terminal.mu.Unlock()
	return h
}

// SetDefaultCursorColor sets default cursor color used when none is specified.
func (h *Harness) SetDefaultCursorColor(c color.Color) *Harness {
	h.terminal.mu.Lock()
	h.terminal.vt.SetDefaultCursorColor(c)
	h.terminal.mu.Unlock()
	return h
}

// CursorPosition returns the terminal emulator cursor position.
func (h *Harness) CursorPosition() uv.Position {
	h.terminal.mu.RLock()
	defer h.terminal.mu.RUnlock()
	return h.terminal.vt.CursorPosition()
}

// Scrollback returns a copy of the terminal's scrollback lines (oldest first).
// It is nil in alternate screen mode. An empty non-nil slice means the scrollback
// buffer exists but has no lines yet.
func (h *Harness) Scrollback() []uv.Line {
	h.terminal.mu.RLock()
	defer h.terminal.mu.RUnlock()
	sb := h.terminal.vt.Scrollback()
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
	h.terminal.mu.RLock()
	defer h.terminal.mu.RUnlock()
	return h.terminal.vt.ScrollbackLen()
}

// SetScrollbackSize sets maximum scrollback lines retained on the terminal's
// screen buffer.
func (h *Harness) SetScrollbackSize(maxLines int) *Harness {
	h.terminal.mu.Lock()
	h.terminal.vt.SetScrollbackSize(maxLines)
	h.terminal.mu.Unlock()
	return h
}

// ClearScrollback clears all scrollback history on the screen buffer.
func (h *Harness) ClearScrollback() *Harness {
	h.terminal.mu.Lock()
	h.terminal.vt.ClearScrollback()
	h.terminal.mu.Unlock()
	return h
}
