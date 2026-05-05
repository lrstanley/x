// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"bufio"
	"errors"
	"image/color"
	"io"
	"slices"
	"sync"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
)

const emuIOBufferSize = 4096

var (
	_ io.Reader = (*emulator)(nil) // [tea.WithInput]
	_ io.Writer = (*emulator)(nil) // [tea.WithOutput]
)

type emulator struct {
	mu             sync.RWMutex
	vt             *vt.Emulator
	inputCloseOnce sync.Once // closes the vt input pipe at most once (unblocks [tea.Program] shutdown)
	focused        bool      // last known focus state for TerminalFocus / TerminalBlur

	shutdownOnce sync.Once      // runs Close teardown once
	wg           sync.WaitGroup // pumpTeaToVT and pumpVTToTea
	outMu        sync.Mutex     // serializes Write and Flush on teaOut
	teaIn        *bufio.Reader  // [tea.Program] reads emulator->program bytes here
	teaOut       *bufio.Writer  // [tea.Program] render output; flushed to vt via toVTW
	toVTW        *io.PipeWriter // writer end read by pumpTeaToVT into vt.Write
	teaPipeR     *io.PipeReader // reader end closed on shutdown to unblock pumpVTToTea if the pipe fills
}

func newEmulator(width, height int) *emulator {
	toVTR, toVTW := io.Pipe()
	fromVTR, fromVTW := io.Pipe()

	emu := &emulator{
		vt:       vt.NewEmulator(width, height),
		focused:  true,
		teaIn:    bufio.NewReaderSize(fromVTR, emuIOBufferSize),
		teaOut:   bufio.NewWriterSize(toVTW, emuIOBufferSize),
		toVTW:    toVTW,
		teaPipeR: fromVTR,
	}

	emu.wg.Add(2)
	go emu.pumpTeaToVT(toVTR)
	go emu.pumpVTToTea(fromVTW)

	emu.mu.Lock()
	emu.vt.Resize(width, height)
	emu.vt.Focus()
	emu.mu.Unlock()

	return emu
}

// pumpTeaToVT copies [teaOutput.Read] to the vt [io.WriteCloser]. Workaround due to
// the blocking nature of [teaOutput.Read] and [vt.Write] calls.
func (e *emulator) pumpTeaToVT(teaOutput *io.PipeReader) {
	defer e.wg.Done()
	buf := make([]byte, emuIOBufferSize)
	for {
		n, err := teaOutput.Read(buf)
		if n > 0 {
			e.mu.Lock()
			_, werr := e.vt.Write(buf[:n])
			e.mu.Unlock()
			if werr != nil {
				return
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) {
				_ = err
			}
			return
		}
	}
}

// pumpVTToTea copies [vt.Read] to the dst [io.WriteCloser]. Workaround due to
// the blocking nature of [vt.Read] and [vt.Write] calls.
func (e *emulator) pumpVTToTea(dst io.WriteCloser) {
	defer e.wg.Done()
	defer dst.Close()

	buf := make([]byte, emuIOBufferSize)
	_, err := io.CopyBuffer(dst, e.vt, buf)
	if err != nil && !errors.Is(err, io.ErrClosedPipe) {
		_ = err
	}
}

// Close stops the I/O pumps, closes the vt input pipe, and waits for pump
// goroutines to exit. It is safe to call more than once.
func (e *emulator) Close() error {
	e.shutdownOnce.Do(func() {
		e.outMu.Lock()
		_ = e.teaOut.Flush()
		e.outMu.Unlock()
		_ = e.toVTW.Close()

		e.closeInput()

		// If the [tea.Program] is not reading terminal input, the vt->tea pipe
		// can fill and block pumpVTToTea on Write; closing the reader end
		// unblocks [tea.Program] shutdown.
		if e.teaPipeR != nil {
			_ = e.teaPipeR.Close()
		}

		e.wg.Wait()
	})
	return nil
}

func (e *emulator) Read(p []byte) (n int, err error) {
	return e.teaIn.Read(p)
}

func (e *emulator) Write(p []byte) (n int, err error) {
	e.outMu.Lock()
	defer e.outMu.Unlock()

	n, err = e.teaOut.Write(p)
	if err != nil {
		return n, err
	}
	err = e.teaOut.Flush()
	return n, err
}

// closeInput closes the vt input pipe. Workaround to ensure appropriate draining
// of [vt.Read] calls by [tea.Program].
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
// assert, and snapshot helpers sample this rendered output. Match the terminal
// dimensions ([WithWindowSize], [Harness.Resize]) to the layout under test so output
// has no unused rows or columns beyond what the renderer emits for that window size.
func (h *Harness) View() string {
	h.tb.Helper()
	h.emulator.mu.RLock()
	defer h.emulator.mu.RUnlock()
	return h.emulator.vt.Render()
}

// Focus sends the terminal a focus event if focus events mode is enabled. This is
// the opposite of [Harness.Blur].
func (h *Harness) Focus() *Harness {
	h.emulator.mu.Lock()
	h.emulator.vt.Focus()
	h.emulator.focused = true
	h.emulator.mu.Unlock()
	return h
}

// Blur sends the terminal a blur event if focus events mode is enabled. This is
// the opposite of [Harness.Focus].
func (h *Harness) Blur() *Harness {
	h.emulator.mu.Lock()
	h.emulator.vt.Blur()
	h.emulator.focused = false
	h.emulator.mu.Unlock()
	return h
}

// Bounds returns the terminals current screen bounds.
func (h *Harness) Bounds() uv.Rectangle {
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

// Width returns the terminals current width in cells (matches [Harness.View]
// output width).
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
	h.emulator.mu.Lock()
	h.emulator.vt.Resize(width, height)
	h.emulator.mu.Unlock()
	return h
}

// Paste paste text into the terminal. If bracketed paste mode is enabled, the
// text is bracketed with the appropriate escape sequences.
func (h *Harness) Paste(text string) *Harness {
	h.emulator.mu.Lock()
	h.emulator.vt.Paste(text)
	h.emulator.mu.Unlock()
	return h
}

// FgColor returns the terminal emulator's foreground color.
func (h *Harness) FgColor() color.Color {
	h.emulator.mu.RLock()
	defer h.emulator.mu.RUnlock()
	return h.emulator.vt.ForegroundColor()
}

// BgColor returns the terminal emulator's background color.
func (h *Harness) BgColor() color.Color {
	h.emulator.mu.RLock()
	defer h.emulator.mu.RUnlock()
	return h.emulator.vt.BackgroundColor()
}

// CursorColor returns the terminal emulator's cursor color.
func (h *Harness) CursorColor() color.Color {
	h.emulator.mu.RLock()
	defer h.emulator.mu.RUnlock()
	return h.emulator.vt.CursorColor()
}

// SetFgColor sets the terminal emulator foreground color.
func (h *Harness) SetFgColor(c color.Color) *Harness {
	h.emulator.mu.Lock()
	h.emulator.vt.SetForegroundColor(c)
	h.emulator.mu.Unlock()
	return h
}

// SetBgColor sets the terminal emulator background color.
func (h *Harness) SetBgColor(c color.Color) *Harness {
	h.emulator.mu.Lock()
	h.emulator.vt.SetBackgroundColor(c)
	h.emulator.mu.Unlock()
	return h
}

// SetCursorColor sets the terminal emulator cursor color.
func (h *Harness) SetCursorColor(c color.Color) *Harness {
	h.emulator.mu.Lock()
	h.emulator.vt.SetCursorColor(c)
	h.emulator.mu.Unlock()
	return h
}

// SetDefaultFgColor sets default foreground color used when none is specified.
func (h *Harness) SetDefaultFgColor(c color.Color) *Harness {
	h.emulator.mu.Lock()
	h.emulator.vt.SetDefaultForegroundColor(c)
	h.emulator.mu.Unlock()
	return h
}

// SetDefaultBgColor sets default background color used when none is specified.
func (h *Harness) SetDefaultBgColor(c color.Color) *Harness {
	h.emulator.mu.Lock()
	h.emulator.vt.SetDefaultBackgroundColor(c)
	h.emulator.mu.Unlock()
	return h
}

// SetDefaultCursorColor sets default cursor color used when none is specified.
func (h *Harness) SetDefaultCursorColor(c color.Color) *Harness {
	h.emulator.mu.Lock()
	h.emulator.vt.SetDefaultCursorColor(c)
	h.emulator.mu.Unlock()
	return h
}

// CursorPosition returns the terminal emulator cursor position.
func (h *Harness) CursorPosition() uv.Position {
	h.emulator.mu.RLock()
	defer h.emulator.mu.RUnlock()
	return h.emulator.vt.CursorPosition()
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
	h.emulator.mu.Lock()
	h.emulator.vt.SetScrollbackSize(maxLines)
	h.emulator.mu.Unlock()
	return h
}

// ClearScrollback clears all scrollback history on the screen buffer.
func (h *Harness) ClearScrollback() *Harness {
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
	for _, r := range s {
		h.emulator.mu.Lock()
		h.emulator.vt.SendKey(uv.KeyPressEvent{Code: r, Text: string(r)})
		h.emulator.mu.Unlock()
	}
	return h
}

// Key sends a single key press message to the terminal emulator itself, e.g.
// "ctrl+a", "enter", "space", etc.
//
// Note that as this sends through the emulator, if paired with [Harness.SendProgram],
// The order of events that the [tea.Program] receives may not be the same as
// order of invocation.
func (h *Harness) Key(key string) *Harness {
	h.tb.Helper()
	ev := mapKeyToEvent(key)
	// mapKeyToEvent maps unknown multi-character keys to Text-only events.
	// x/vt SendKey encodes only Code in its default branch, so split into
	// per-rune presses (same transport as [Harness.Type]).
	if ev.Code == 0 && ev.Text != "" {
		for _, r := range ev.Text {
			h.emulator.mu.Lock()
			h.emulator.vt.SendKey(uv.KeyPressEvent{Code: r, Text: string(r)})
			h.emulator.mu.Unlock()
		}
		return h
	}
	h.emulator.mu.Lock()
	h.emulator.vt.SendKey(ev)
	h.emulator.mu.Unlock()
	return h
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
	h.emulator.mu.Lock()
	h.emulator.vt.SendMouse(m)
	h.emulator.mu.Unlock()
	return h
}

// MouseClick simulates a full press and release at (x, y).
func (h *Harness) MouseClick(button uv.MouseButton, x, y int) *Harness {
	h.tb.Helper()
	h.Mouse(uv.MouseClickEvent{X: x, Y: y, Button: button})
	h.Mouse(uv.MouseReleaseEvent{X: x, Y: y, Button: button})
	return h
}

// LeftClick is [Harness.MouseClick] with the left mouse button.
func (h *Harness) LeftClick(x, y int) *Harness {
	h.tb.Helper()
	return h.MouseClick(uv.MouseLeft, x, y)
}

// MiddleClick is [Harness.MouseClick] with the middle mouse button.
func (h *Harness) MiddleClick(x, y int) *Harness {
	h.tb.Helper()
	return h.MouseClick(uv.MouseMiddle, x, y)
}

// RightClick is [Harness.MouseClick] with the right mouse button.
func (h *Harness) RightClick(x, y int) *Harness {
	h.tb.Helper()
	return h.MouseClick(uv.MouseRight, x, y)
}

// MouseDrag simulates a drag from (x1, y1) to (x2, y2).
func (h *Harness) MouseDrag(button uv.MouseButton, x1, y1, x2, y2 int) *Harness {
	h.tb.Helper()

	h.Mouse(uv.MouseClickEvent{X: x1, Y: y1, Button: button})

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
		h.Mouse(uv.MouseMotionEvent{X: x, Y: y, Button: button})
	}

	h.Mouse(uv.MouseReleaseEvent{X: x2, Y: y2, Button: button})
	return h
}

// LeftDrag is [Harness.MouseDrag] with the left mouse button.
func (h *Harness) LeftDrag(x1, y1, x2, y2 int) *Harness {
	h.tb.Helper()
	return h.MouseDrag(uv.MouseLeft, x1, y1, x2, y2)
}
