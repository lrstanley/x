// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"fmt"
	"image/color"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// terminalProbe is a minimal model with a stable empty view so
// [Harness.WaitSettleView] completes while a full Bubble Tea program runs.
type terminalProbe struct{}

func (terminalProbe) Init() tea.Cmd { return nil }

func (terminalProbe) Update(uv.Event) (tea.Model, tea.Cmd) {
	return terminalProbe{}, nil
}

func (terminalProbe) View() tea.View {
	return tea.NewView("")
}

func newTerminalHarness(tb testing.TB, w, h int) *Harness {
	tb.Helper()
	hr := NewHarness(tb, terminalProbe{}, WithInitialTermSize(w, h))
	hr.WaitSettleView()
	return hr
}

func colorEqual(a, b color.Color) bool {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

// waitMessagesContainWindowSize polls until a matching [tea.WindowSizeMsg] or
// in-band [uv.WindowSizeEvent] inside [uv.MultiEvent] appears (in-band resize
// uses CSI 48 ; … t per [ansi.InBandResize]).
func waitMessagesContainWindowSize(tb testing.TB, h *Harness, wantW, wantH int) {
	tb.Helper()
	cfg := collectOptions()
	deadline := time.Now().Add(cfg.timeout)

	for time.Now().Before(deadline) {
		for _, msg := range h.Messages() {
			switch msg := msg.(type) {
			case tea.WindowSizeMsg:
				if msg.Width == wantW && msg.Height == wantH {
					return
				}
			}
		}
		select {
		case <-tb.Context().Done():
			tb.Fatalf("context canceled: %v", tb.Context().Err())
		case <-time.After(cfg.checkInterval):
		}
	}
	tb.Fatalf("timed out waiting for terminal size %#v in observed messages:\n%s",
		tea.WindowSizeMsg{Width: wantW, Height: wantH},
		observedMessageTypes(h.Messages()),
	)
}

func TestHarness_terminalInitialWindowSizeMsg(t *testing.T) {
	wantW, wantH := 11, 7
	h := newTerminalHarness(t, wantW, wantH)

	WaitMessageWhere(t, h, func(m tea.WindowSizeMsg) bool {
		return m.Width == wantW && m.Height == wantH
	})
}

func TestHarness_terminalDimensionsResizeInBandMsgs(t *testing.T) {
	const wantW, wantH = 11, 7
	h := newTerminalHarness(t, wantW, wantH)
	WaitMessageWhere(t, h, func(m tea.WindowSizeMsg) bool {
		return m.Width == wantW && m.Height == wantH
	})

	const nextW, nextH = 30, 12
	h.terminal.mu.Lock()
	_, _ = h.terminal.vt.WriteString(ansi.SetModeInBandResize)
	h.terminal.mu.Unlock()
	h.Resize(nextW, nextH)
	waitMessagesContainWindowSize(t, h, nextW, nextH)

	if h.TerminalWidth() != nextW || h.TerminalHeight() != nextH {
		t.Fatalf("TerminalWidth/Height = %dx%d, want %dx%d", h.TerminalWidth(), h.TerminalHeight(), nextW, nextH)
	}
	b := h.Bounds()
	if b.Dx() != nextW || b.Dy() != nextH {
		t.Fatalf("Bounds() = (%d,%d), want (%d,%d)", b.Dx(), b.Dy(), nextW, nextH)
	}
}

func TestHarness_terminalColors(t *testing.T) {
	h := newTerminalHarness(t, 5, 3)

	fg := color.NRGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff}
	bg := color.NRGBA{R: 0xaa, G: 0xbb, B: 0xcc, A: 0xff}
	cr := color.NRGBA{R: 0xee, G: 0xdd, B: 0xcc, A: 0xff}

	got := h.
		SetForegroundColor(fg).
		SetBackgroundColor(bg).
		SetCursorColor(cr)
	if got != h {
		t.Fatal("expected color setters to return the same Harness for chaining")
	}

	if !colorEqual(h.ForegroundColor(), fg) {
		t.Errorf("ForegroundColor = %v, want %#v", h.ForegroundColor(), fg)
	}
	if !colorEqual(h.BackgroundColor(), bg) {
		t.Errorf("BackgroundColor = %v, want %#v", h.BackgroundColor(), bg)
	}
	if !colorEqual(h.CursorColor(), cr) {
		t.Errorf("CursorColor = %v, want %#v", h.CursorColor(), cr)
	}

	dfg := color.NRGBA{R: 0x10, G: 0x20, B: 0x40, A: 0xff}
	dbg := color.NRGBA{R: 0x41, G: 0x42, B: 0x43, A: 0xff}
	dcr := color.NRGBA{R: 0xfe, G: 0xdc, B: 0xba, A: 0xff}

	h.SetDefaultForegroundColor(dfg)
	h.SetDefaultBackgroundColor(dbg)
	h.SetDefaultCursorColor(dcr)

	h.SetForegroundColor(nil)
	h.SetBackgroundColor(nil)
	h.SetCursorColor(nil)

	if !colorEqual(h.ForegroundColor(), dfg) || !colorEqual(h.BackgroundColor(), dbg) || !colorEqual(h.CursorColor(), dcr) {
		t.Fatalf("nil colors should revert to defaults: fg=%v bg=%v cur=%v", h.ForegroundColor(), h.BackgroundColor(), h.CursorColor())
	}
}

func TestHarness_terminalCursorPosition(t *testing.T) {
	h := newTerminalHarness(t, 8, 4)
	pt := h.CursorPosition()
	if pt.X != 0 || pt.Y != 0 {
		t.Fatalf("initial cursor (%d,%d), want (0,0)", pt.X, pt.Y)
	}
}

func TestHarness_terminalScrollbackCopyAndCount(t *testing.T) {
	h := newTerminalHarness(t, 5, 4)
	h.terminal.mu.Lock()
	_, _ = h.terminal.vt.WriteString("start\n")
	for i := range 18 {
		_, _ = fmt.Fprintf(h.terminal.vt, "row-%d\n", i)
	}
	h.terminal.mu.Unlock()

	h.terminal.mu.RLock()
	vtLen := h.terminal.vt.ScrollbackLen()
	h.terminal.mu.RUnlock()
	if h.ScrollbackCount() != vtLen {
		t.Fatalf("ScrollbackCount vs vt len: %d vs %d", h.ScrollbackCount(), vtLen)
	}
	if h.ScrollbackCount() == 0 {
		t.Fatal("expected non-zero scrollback after many newlines")
	}

	snap := h.Scrollback()
	if len(snap) != h.ScrollbackCount() {
		t.Fatalf("len(Scrollback()) = %d, ScrollbackCount = %d", len(snap), h.ScrollbackCount())
	}

	h.ClearScrollback()
	if h.ScrollbackCount() != 0 {
		t.Fatalf("after ClearScrollback, count = %d, want 0", h.ScrollbackCount())
	}
	if len(snap) == 0 {
		t.Fatal("snapshot should have had lines to verify copy retention")
	}
	if len(snap[0]) == 0 {
		t.Fatal("expected first snapshot line to have cells")
	}

	snap[0][0] = uv.Cell{}
	if h.ScrollbackCount() != 0 {
		t.Fatalf("after clearing scrollback, mutating snapshot line should leave count at 0, got %d", h.ScrollbackCount())
	}
}

func TestHarness_terminalSetScrollbackSize(t *testing.T) {
	h := newTerminalHarness(t, 4, 3)
	h.SetScrollbackSize(3)
	h.terminal.mu.Lock()
	for i := range 20 {
		_, _ = fmt.Fprintf(h.terminal.vt, "x%d\n", i)
	}
	h.terminal.mu.Unlock()
	if got := h.ScrollbackCount(); got > 3 {
		t.Fatalf("ScrollbackCount = %d, max was 3", got)
	}
}

func TestHarness_terminalAltScreen(t *testing.T) {
	h := newTerminalHarness(t, 6, 3)
	if h.IsAltScreen() {
		t.Fatal("expected main screen initially")
	}
	h.terminal.mu.Lock()
	_, _ = h.terminal.vt.WriteString("\x1b[?1049h")
	h.terminal.mu.Unlock()
	if !h.IsAltScreen() {
		t.Fatal("expected alt screen after CSI ?1049h")
	}
	h.terminal.mu.Lock()
	_, _ = h.terminal.vt.WriteString("\x1b[?1049l")
	h.terminal.mu.Unlock()
	if h.IsAltScreen() {
		t.Fatal("expected main screen after leaving alt screen")
	}
}

func TestHarness_terminalFocusBlurMsgs(t *testing.T) {
	h := newTerminalHarness(t, 4, 2)
	h.terminal.mu.Lock()
	_, _ = h.terminal.vt.WriteString(ansi.SetModeFocusEvent)
	h.terminal.mu.Unlock()

	h.Focus()
	WaitMessage[tea.FocusMsg](t, h)

	h.Blur()
	WaitMessage[tea.BlurMsg](t, h)
}

func TestHarness_terminalPasteMsg(t *testing.T) {
	h := newTerminalHarness(t, 4, 2)
	h.terminal.mu.Lock()
	_, _ = h.terminal.vt.WriteString(ansi.SetModeBracketedPaste)
	h.terminal.mu.Unlock()

	const pasted = "hello"
	h.Paste(pasted)

	got := WaitMessageWhere(t, h, func(m tea.PasteMsg) bool {
		return m.Content == pasted
	})
	if got.Content != pasted {
		t.Fatalf("PasteMsg = %#v, want Content %q", got, pasted)
	}
}

func TestHarness_terminalChainedAPI(t *testing.T) {
	h := newTerminalHarness(t, 6, 2)
	got := h.Focus().Blur().Resize(5, 4)
	if got != h {
		t.Fatal("expected chained terminal methods to return the same harness")
	}
	_ = h.TerminalView()
}
