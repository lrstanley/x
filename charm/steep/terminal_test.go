// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"fmt"
	"image/color"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// terminalProbe is a minimal model with a stable empty view.
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
	return NewHarness(tb, terminalProbe{}, WithWindowSize(w, h))
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
		for msg := range h.MessageHistory() {
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
		newTypeObserver[uv.Event]().observe(slices.Collect(h.MessageHistory())...),
	)
}

func TestHarness_terminalInitialWindowSizeMsg(t *testing.T) {
	wantW, wantH := 11, 7
	h := newTerminalHarness(t, wantW, wantH)

	WaitMessageWhere(t, h, func(msg uv.Event) bool {
		m, ok := msg.(tea.WindowSizeMsg)
		return ok && m.Width == wantW && m.Height == wantH
	})
}

func TestHarness_terminalDimensionsResizeInBandMsgs(t *testing.T) {
	const wantW, wantH = 11, 7
	h := newTerminalHarness(t, wantW, wantH)
	WaitMessageWhere(t, h, func(msg uv.Event) bool {
		m, ok := msg.(tea.WindowSizeMsg)
		return ok && m.Width == wantW && m.Height == wantH
	})

	const nextW, nextH = 30, 12
	h.emulator.mu.Lock()
	_, _ = h.emulator.vt.WriteString(ansi.SetModeInBandResize)
	h.emulator.mu.Unlock()
	h.TerminalResize(nextW, nextH)
	waitMessagesContainWindowSize(t, h, nextW, nextH)

	if h.TerminalWidth() != nextW || h.TerminalHeight() != nextH {
		t.Fatalf("TerminalWidth/Height = %dx%d, want %dx%d", h.TerminalWidth(), h.TerminalHeight(), nextW, nextH)
	}
	tw, th := h.TerminalDimensions()
	if tw != nextW || th != nextH {
		t.Fatalf("TerminalDimensions = %dx%d, want %dx%d", tw, th, nextW, nextH)
	}
	b := h.TerminalBounds()
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
		SetTerminalFgColor(fg).
		SetTerminalBgColor(bg).
		SetTerminalCursorColor(cr)
	if got != h {
		t.Fatal("expected color setters to return the same Harness for chaining")
	}

	if !colorEqual(h.TerminalFgColor(), fg) {
		t.Errorf("TerminalFgColor = %v, want %#v", h.TerminalFgColor(), fg)
	}
	if !colorEqual(h.TerminalBgColor(), bg) {
		t.Errorf("TerminalBgColor = %v, want %#v", h.TerminalBgColor(), bg)
	}
	if !colorEqual(h.TerminalCursorColor(), cr) {
		t.Errorf("TerminalCursorColor = %v, want %#v", h.TerminalCursorColor(), cr)
	}

	dfg := color.NRGBA{R: 0x10, G: 0x20, B: 0x40, A: 0xff}
	dbg := color.NRGBA{R: 0x41, G: 0x42, B: 0x43, A: 0xff}
	dcr := color.NRGBA{R: 0xfe, G: 0xdc, B: 0xba, A: 0xff}

	h.SetDefaultTerminalFgColor(dfg)
	h.SetDefaultTerminalBgColor(dbg)
	h.SetDefaultTerminalCursorColor(dcr)

	h.SetTerminalFgColor(nil)
	h.SetTerminalBgColor(nil)
	h.SetTerminalCursorColor(nil)

	if !colorEqual(h.TerminalFgColor(), dfg) || !colorEqual(h.TerminalBgColor(), dbg) || !colorEqual(h.TerminalCursorColor(), dcr) {
		t.Fatalf("nil colors should revert to defaults: fg=%v bg=%v cur=%v", h.TerminalFgColor(), h.TerminalBgColor(), h.TerminalCursorColor())
	}
}

func TestHarness_terminalCursorPosition(t *testing.T) {
	h := newTerminalHarness(t, 8, 4)
	pt := h.TerminalCursorPosition()
	if pt.X != 0 || pt.Y != 0 {
		t.Fatalf("initial cursor (%d,%d), want (0,0)", pt.X, pt.Y)
	}
}

func TestHarness_terminalScrollbackCopyAndCount(t *testing.T) {
	h := newTerminalHarness(t, 5, 4)
	h.emulator.mu.Lock()
	_, _ = h.emulator.vt.WriteString("start\n")
	for i := range 18 {
		_, _ = fmt.Fprintf(h.emulator.vt, "row-%d\n", i)
	}
	h.emulator.mu.Unlock()

	h.emulator.mu.RLock()
	vtLen := h.emulator.vt.ScrollbackLen()
	h.emulator.mu.RUnlock()
	if h.TerminalScrollbackCount() != vtLen {
		t.Fatalf("TerminalScrollbackCount vs vt len: %d vs %d", h.TerminalScrollbackCount(), vtLen)
	}
	if h.TerminalScrollbackCount() == 0 {
		t.Fatal("expected non-zero scrollback after many newlines")
	}

	snap := h.TerminalScrollback()
	if len(snap) != h.TerminalScrollbackCount() {
		t.Fatalf("len(TerminalScrollback()) = %d, TerminalScrollbackCount = %d", len(snap), h.TerminalScrollbackCount())
	}

	h.ClearTerminalScrollback()
	if h.TerminalScrollbackCount() != 0 {
		t.Fatalf("after ClearTerminalScrollback, count = %d, want 0", h.TerminalScrollbackCount())
	}
	if len(snap) == 0 {
		t.Fatal("snapshot should have had lines to verify copy retention")
	}
	if len(snap[0]) == 0 {
		t.Fatal("expected first snapshot line to have cells")
	}

	snap[0][0] = uv.Cell{}
	if h.TerminalScrollbackCount() != 0 {
		t.Fatalf("after clearing scrollback, mutating snapshot line should leave count at 0, got %d", h.TerminalScrollbackCount())
	}
}

func TestHarness_terminalSetScrollbackSize(t *testing.T) {
	h := newTerminalHarness(t, 4, 3)
	h.SetTerminalScrollbackSize(3)
	h.emulator.mu.Lock()
	for i := range 20 {
		_, _ = fmt.Fprintf(h.emulator.vt, "x%d\n", i)
	}
	h.emulator.mu.Unlock()
	if got := h.TerminalScrollbackCount(); got > 3 {
		t.Fatalf("TerminalScrollbackCount = %d, max was 3", got)
	}
}

func TestHarness_terminalAltScreen(t *testing.T) {
	h := newTerminalHarness(t, 6, 3)
	if h.IsAltScreen() {
		t.Fatal("expected main screen initially")
	}
	h.emulator.mu.Lock()
	_, _ = h.emulator.vt.WriteString("\x1b[?1049h")
	h.emulator.mu.Unlock()
	if !h.IsAltScreen() {
		t.Fatal("expected alt screen after CSI ?1049h")
	}
	h.emulator.mu.Lock()
	_, _ = h.emulator.vt.WriteString("\x1b[?1049l")
	h.emulator.mu.Unlock()
	if h.IsAltScreen() {
		t.Fatal("expected main screen after leaving alt screen")
	}
}

func TestHarness_terminalFocusBlurMsgs(t *testing.T) {
	h := newTerminalHarness(t, 4, 2)
	h.emulator.mu.Lock()
	_, _ = h.emulator.vt.WriteString(ansi.SetModeFocusEvent)
	h.emulator.mu.Unlock()

	h.TerminalFocus()
	WaitMessage[tea.FocusMsg](t, h)

	h.TerminalBlur()
	WaitMessage[tea.BlurMsg](t, h)
}

func TestHarness_terminalPasteMsg(t *testing.T) {
	h := newTerminalHarness(t, 4, 2)
	h.emulator.mu.Lock()
	_, _ = h.emulator.vt.WriteString(ansi.SetModeBracketedPaste)
	h.emulator.mu.Unlock()

	const pasted = "hello"
	h.TerminalPaste(pasted)

	got := WaitMessageWhere(t, h, func(msg uv.Event) bool {
		m, ok := msg.(tea.PasteMsg)
		return ok && m.Content == pasted
	})
	pm, ok := got.(tea.PasteMsg)
	if !ok || pm.Content != pasted {
		t.Fatalf("PasteMsg = %#v, want Content %q", got, pasted)
	}
}

func TestHarness_terminalChainedAPI(t *testing.T) {
	h := newTerminalHarness(t, 6, 2)
	got := h.TerminalFocus().TerminalBlur().TerminalResize(5, 4)
	if got != h {
		t.Fatal("expected chained terminal methods to return the same harness")
	}
	_ = h.TerminalView()
}

func TestHarness_terminalAsViewableUsesRenderPath(t *testing.T) {
	h := newTerminalHarness(t, 8, 4)
	WaitMessageWhere(t, h, func(msg uv.Event) bool {
		m, ok := msg.(tea.WindowSizeMsg)
		return ok && m.Width == 8 && m.Height == 4
	})
	// Empty substring matches immediately; exercises [terminal.View] via [Viewable].
	WaitString(t, h.emulator.View, "")
	h.Quit()
	h.WaitFinished(WithTimeout(time.Second))
}

func TestHarness_TerminalType_returnsHarnessForChaining(t *testing.T) {
	h := NewHarness(t, rootTestModel{})
	if ptr := h.TerminalType(""); ptr != h {
		t.Fatalf("TerminalType should return the same harness, got %p want %p", ptr, h)
	}
}

func TestHarness_TerminalType_deliversPrintableRunesThroughEmulator(t *testing.T) {
	h := NewHarness(t, rootTestModel{})
	h.TerminalType("ab ").WaitString("text=ab ").RequireString("text=ab ")
}

func TestHarness_TerminalKey_returnsHarnessForChaining(t *testing.T) {
	h := NewHarness(t, rootTestModel{})
	if ptr := h.TerminalKey("z"); ptr != h {
		t.Fatalf("TerminalKey should return the same harness, got %p want %p", ptr, h)
	}
}

func TestHarness_TerminalKey_accumulatesPrintableText(t *testing.T) {
	h := NewHarness(t, rootTestModel{})
	h.TerminalKey("a").TerminalKey("b").TerminalKey("space").WaitString("text=ab ").RequireString("text=ab ")
}

func TestHarness_TerminalKey_mapsNamedKeysThroughEmulator(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		wantCode rune
		wantMod  tea.KeyMod
		wantText string
	}{
		{name: "single ASCII rune", key: "q", wantCode: 'q', wantMod: 0, wantText: "q"},
		{name: "space", key: "space", wantCode: ' ', wantMod: 0, wantText: " "},
		{name: "enter", key: "enter", wantCode: tea.KeyEnter},
		{name: "tab", key: "tab", wantCode: tea.KeyTab},
		{name: "escape alias esc", key: "esc", wantCode: tea.KeyEscape},
		{name: "shift+tab", key: "shift+tab", wantCode: tea.KeyTab, wantMod: tea.ModShift},
		{name: "ctrl lowercase letter", key: "ctrl+g", wantCode: 'g', wantMod: tea.ModCtrl},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHarness(t, rootTestModel{})
			h.TerminalKey(tt.key)
			got := WaitMessageWhere(t, h, func(msg uv.Event) bool {
				km, ok := msg.(tea.KeyPressMsg)
				if !ok {
					return false
				}
				k := km.Key()
				if k.Code != tt.wantCode {
					return false
				}
				if k.Mod != tt.wantMod {
					return false
				}
				if tt.wantText != "" && k.Text != tt.wantText {
					return false
				}
				return true
			})
			if _, ok := got.(tea.KeyPressMsg); !ok {
				t.Fatalf("got message type %T, want tea.KeyPressMsg", got)
			}
		})
	}
}

func enableEmulatorMouseReporting(tb testing.TB, h *Harness) {
	tb.Helper()
	h.emulator.mu.Lock()
	_, _ = h.emulator.vt.WriteString(ansi.SetModeMouseButtonEvent)
	_, _ = h.emulator.vt.WriteString(ansi.SetModeMouseExtSgr)
	h.emulator.mu.Unlock()
}

// mouseStep is a normalized mouse line from [tea] for ordered assertions.
type mouseStep struct {
	kind string // "click", "motion", or "release"
	tea.Mouse
}

func collectMouseTail(tb testing.TB, h *Harness, nBefore int) []mouseStep {
	tb.Helper()
	all := slices.Collect(h.MessageHistory())
	if nBefore > len(all) {
		tb.Fatalf("nBefore=%d longer than history len=%d", nBefore, len(all))
	}
	var out []mouseStep
	for _, msg := range all[nBefore:] {
		switch m := msg.(type) {
		case tea.MouseClickMsg:
			out = append(out, mouseStep{"click", m.Mouse()})
		case tea.MouseMotionMsg:
			out = append(out, mouseStep{"motion", m.Mouse()})
		case tea.MouseReleaseMsg:
			out = append(out, mouseStep{"release", m.Mouse()})
		}
	}
	return out
}

func TestHarness_TerminalMouse_deliversClick(t *testing.T) {
	h := newTerminalHarness(t, 14, 5)
	WaitMessageWhere(t, h, func(msg uv.Event) bool {
		_, ok := msg.(tea.WindowSizeMsg)
		return ok
	})

	enableEmulatorMouseReporting(t, h)
	n := len(slices.Collect(h.MessageHistory()))

	h.TerminalMouse(uv.MouseClickEvent{X: 4, Y: 2, Button: uv.MouseLeft})
	WaitSettleMessages(t, h)

	got := collectMouseTail(t, h, n)
	if len(got) != 1 || got[0].kind != "click" || got[0].X != 4 || got[0].Y != 2 || got[0].Button != tea.MouseLeft {
		t.Fatalf("unexpected mouse tail: %#v", got)
	}
}

func TestHarness_TerminalMouseClick_deliversPressAndRelease(t *testing.T) {
	h := newTerminalHarness(t, 12, 4)
	WaitMessageWhere(t, h, func(msg uv.Event) bool {
		_, ok := msg.(tea.WindowSizeMsg)
		return ok
	})
	enableEmulatorMouseReporting(t, h)
	n := len(slices.Collect(h.MessageHistory()))

	h.TerminalMouseClick(uv.MouseLeft, 3, 2)
	WaitSettleMessages(t, h)

	got := collectMouseTail(t, h, n)
	want := []mouseStep{
		{"click", tea.Mouse{X: 3, Y: 2, Button: tea.MouseLeft}},
		{"release", tea.Mouse{X: 3, Y: 2, Button: tea.MouseLeft}},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d mouse msgs, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].kind != want[i].kind || got[i].X != want[i].X || got[i].Y != want[i].Y || got[i].Button != want[i].Button {
			t.Fatalf("step %d: got %#v, want kind=%s Mouse=%+v", i, got[i], want[i].kind, want[i].Mouse)
		}
	}
}

func TestHarness_TerminalLeftMiddleRightClick_buttons(t *testing.T) {
	tests := []struct {
		name string
		fn   func(*Harness, int, int) *Harness
		want uv.MouseButton
	}{
		{name: "left", fn: (*Harness).TerminalLeftClick, want: uv.MouseLeft},
		{name: "middle", fn: (*Harness).TerminalMiddleClick, want: uv.MouseMiddle},
		{name: "right", fn: (*Harness).TerminalRightClick, want: uv.MouseRight},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTerminalHarness(t, 10, 5)
			WaitMessageWhere(t, h, func(msg uv.Event) bool {
				_, ok := msg.(tea.WindowSizeMsg)
				return ok
			})
			enableEmulatorMouseReporting(t, h)
			n := len(slices.Collect(h.MessageHistory()))

			tt.fn(h, 2, 1)
			WaitSettleMessages(t, h)

			got := collectMouseTail(t, h, n)
			if len(got) != 2 || got[0].Button != tt.want || got[1].Button != tt.want {
				t.Fatalf("got %#v, want click+release with button %v", got, tt.want)
			}
		})
	}
}

func TestHarness_TerminalMouseDrag_sequence(t *testing.T) {
	h := newTerminalHarness(t, 16, 6)
	WaitMessageWhere(t, h, func(msg uv.Event) bool {
		_, ok := msg.(tea.WindowSizeMsg)
		return ok
	})
	enableEmulatorMouseReporting(t, h)
	n := len(slices.Collect(h.MessageHistory()))

	// (1,1) -> (3,1): press, motion at (2,1), motion at (3,1), release.
	h.TerminalMouseDrag(uv.MouseLeft, 1, 1, 3, 1)
	WaitSettleMessages(t, h)

	got := collectMouseTail(t, h, n)
	want := []mouseStep{
		{"click", tea.Mouse{X: 1, Y: 1, Button: tea.MouseLeft}},
		{"motion", tea.Mouse{X: 2, Y: 1, Button: tea.MouseLeft}},
		{"motion", tea.Mouse{X: 3, Y: 1, Button: tea.MouseLeft}},
		{"release", tea.Mouse{X: 3, Y: 1, Button: tea.MouseLeft}},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d steps, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].kind != want[i].kind || got[i].X != want[i].X || got[i].Y != want[i].Y || got[i].Button != want[i].Button {
			t.Fatalf("step %d: got %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestHarness_TerminalLeftDrag_equivalentToMouseDragLeft(t *testing.T) {
	h := newTerminalHarness(t, 12, 4)
	WaitMessageWhere(t, h, func(msg uv.Event) bool {
		_, ok := msg.(tea.WindowSizeMsg)
		return ok
	})
	enableEmulatorMouseReporting(t, h)

	n0 := len(slices.Collect(h.MessageHistory()))
	h.TerminalMouseDrag(uv.MouseLeft, 0, 0, 1, 0)
	WaitSettleMessages(t, h)
	a := collectMouseTail(t, h, n0)

	n1 := len(slices.Collect(h.MessageHistory()))
	h.TerminalLeftDrag(0, 0, 1, 0)
	WaitSettleMessages(t, h)
	b := collectMouseTail(t, h, n1)

	if len(a) != len(b) {
		t.Fatalf("TerminalMouseDrag len=%d vs TerminalLeftDrag len=%d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("step %d: mouse drag %#v != left drag %#v", i, a[i], b[i])
		}
	}
}

func TestHarness_TerminalMouseDrag_sameCellIsClickAndRelease(t *testing.T) {
	h := newTerminalHarness(t, 8, 4)
	WaitMessageWhere(t, h, func(msg uv.Event) bool {
		_, ok := msg.(tea.WindowSizeMsg)
		return ok
	})
	enableEmulatorMouseReporting(t, h)
	n := len(slices.Collect(h.MessageHistory()))

	h.TerminalMouseDrag(uv.MouseRight, 2, 2, 2, 2)
	WaitSettleMessages(t, h)

	got := collectMouseTail(t, h, n)
	if len(got) != 2 || got[0].kind != "click" || got[1].kind != "release" {
		t.Fatalf("want click+release only, got %#v", got)
	}
	if got[0].Button != tea.MouseRight || got[1].Button != tea.MouseRight {
		t.Fatalf("buttons: got (%v, %v), want right", got[0].Button, got[1].Button)
	}
}

func TestHarness_TerminalMouse_ignoredWithoutMouseModes(t *testing.T) {
	h := newTerminalHarness(t, 8, 4)
	WaitMessageWhere(t, h, func(msg uv.Event) bool {
		_, ok := msg.(tea.WindowSizeMsg)
		return ok
	})
	n := len(slices.Collect(h.MessageHistory()))
	h.TerminalLeftClick(2, 2)
	WaitSettleMessages(t, h)
	for _, msg := range slices.Collect(h.MessageHistory())[n:] {
		switch msg.(type) {
		case tea.MouseClickMsg, tea.MouseMotionMsg, tea.MouseReleaseMsg, tea.MouseWheelMsg:
			t.Fatalf("unexpected mouse message without DEC mouse modes: %T", msg)
		default:
		}
	}
}

func TestHarness_TerminalMouse_helpersReturnHarnessForChaining(t *testing.T) {
	h := newTerminalHarness(t, 10, 4)
	WaitMessageWhere(t, h, func(msg uv.Event) bool {
		_, ok := msg.(tea.WindowSizeMsg)
		return ok
	})
	enableEmulatorMouseReporting(t, h)
	if p := h.
		TerminalMouse(uv.MouseClickEvent{X: 0, Y: 0, Button: uv.MouseLeft}).
		TerminalMouseClick(uv.MouseMiddle, 1, 1).
		TerminalLeftClick(2, 2).
		TerminalMiddleClick(3, 3).
		TerminalRightClick(0, 1).
		TerminalMouseDrag(uv.MouseRight, 1, 0, 2, 0).
		TerminalLeftDrag(0, 2, 1, 2); p != h {
		t.Fatalf("chain returned %p, want same harness %p", p, h)
	}
}
