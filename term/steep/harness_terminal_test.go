// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"fmt"
	"image/color"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/reel/vt"
	"github.com/lrstanley/x/term/reel/vt/screen"
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
	cfg := collectOptions(tb)
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
	t.Parallel()
	wantW, wantH := 11, 7
	h := newTerminalHarness(t, wantW, wantH)

	h.WaitMessageWhere(func(m tea.WindowSizeMsg) bool {
		return m.Width == wantW && m.Height == wantH
	})
}

func TestHarness_terminalDimensionsResizeInBandMsgs(t *testing.T) {
	t.Parallel()
	const wantW, wantH = 11, 7
	h := newTerminalHarness(t, wantW, wantH)
	h.WaitMessageWhere(func(m tea.WindowSizeMsg) bool {
		return m.Width == wantW && m.Height == wantH
	})

	const nextW, nextH = 30, 12
	_ = writeString(h, ansi.SetModeInBandResize)
	h.Resize(nextW, nextH)
	waitMessagesContainWindowSize(t, h, nextW, nextH)

	if h.Width() != nextW || h.Height() != nextH {
		t.Fatalf("TerminalWidth/Height = %dx%d, want %dx%d", h.Width(), h.Height(), nextW, nextH)
	}
	tw, th := h.Dimensions()
	if tw != nextW || th != nextH {
		t.Fatalf("TerminalDimensions = %dx%d, want %dx%d", tw, th, nextW, nextH)
	}
	b := h.Bounds()
	if b.Dx() != nextW || b.Dy() != nextH {
		t.Fatalf("Bounds() = (%d,%d), want (%d,%d)", b.Dx(), b.Dy(), nextW, nextH)
	}
}

func TestHarness_terminalColors(t *testing.T) {
	t.Parallel()
	h := newTerminalHarness(t, 5, 3)

	fg := color.NRGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff}
	bg := color.NRGBA{R: 0xaa, G: 0xbb, B: 0xcc, A: 0xff}
	cr := color.NRGBA{R: 0xee, G: 0xdd, B: 0xcc, A: 0xff}

	got := h.
		SetFgColor(fg).
		SetBgColor(bg).
		SetCursorColor(cr)
	if got != h {
		t.Fatal("expected color setters to return the same Harness for chaining")
	}

	if !colorEqual(h.FgColor(), fg) {
		t.Errorf("TerminalFgColor = %v, want %#v", h.FgColor(), fg)
	}
	if !colorEqual(h.BgColor(), bg) {
		t.Errorf("TerminalBgColor = %v, want %#v", h.BgColor(), bg)
	}
	if !colorEqual(h.CursorColor(), cr) {
		t.Errorf("TerminalCursorColor = %v, want %#v", h.CursorColor(), cr)
	}

	dfg := color.NRGBA{R: 0x10, G: 0x20, B: 0x40, A: 0xff}
	dbg := color.NRGBA{R: 0x41, G: 0x42, B: 0x43, A: 0xff}
	dcr := color.NRGBA{R: 0xfe, G: 0xdc, B: 0xba, A: 0xff}

	h.SetDefaultFgColor(dfg)
	h.SetDefaultBgColor(dbg)
	h.SetDefaultCursorColor(dcr)

	h.SetFgColor(nil)
	h.SetBgColor(nil)
	h.SetCursorColor(nil)

	if !colorEqual(h.FgColor(), dfg) || !colorEqual(h.BgColor(), dbg) || !colorEqual(h.CursorColor(), dcr) {
		t.Fatalf("nil colors should revert to defaults: fg=%v bg=%v cur=%v", h.FgColor(), h.BgColor(), h.CursorColor())
	}
}

func TestHarness_terminalCursorPosition(t *testing.T) {
	t.Parallel()
	h := newTerminalHarness(t, 8, 4)
	pt := h.CursorPosition()
	if pt.X != 0 || pt.Y != 0 {
		t.Fatalf("initial cursor (%d,%d), want (0,0)", pt.X, pt.Y)
	}
}

func TestHarness_terminalScrollbackCopyAndCount(t *testing.T) {
	t.Parallel()
	h := newTerminalHarness(t, 5, 4)
	var rows strings.Builder
	rows.WriteString("start\n")
	for i := range 18 {
		fmt.Fprintf(&rows, "row-%d\n", i)
	}
	_ = writeString(h, rows.String())
	vtLen := h.session.Terminal().ScrollbackLen()
	if h.ScrollbackCount() != vtLen {
		t.Fatalf("TerminalScrollbackCount vs vt len: %d vs %d", h.ScrollbackCount(), vtLen)
	}
	if h.ScrollbackCount() == 0 {
		t.Fatal("expected non-zero scrollback after many newlines")
	}

	snap := h.Scrollback()
	if len(snap) != h.ScrollbackCount() {
		t.Fatalf("len(TerminalScrollback()) = %d, TerminalScrollbackCount = %d", len(snap), h.ScrollbackCount())
	}

	h.ClearScrollback()
	if h.ScrollbackCount() != 0 {
		t.Fatalf("after ClearTerminalScrollback, count = %d, want 0", h.ScrollbackCount())
	}
	if len(snap) == 0 {
		t.Fatal("snapshot should have had lines to verify copy retention")
	}
	if len(snap[0]) == 0 {
		t.Fatal("expected first snapshot line to have cells")
	}

	snap[0][0] = screen.Cell{}
	if h.ScrollbackCount() != 0 {
		t.Fatalf("after clearing scrollback, mutating snapshot line should leave count at 0, got %d", h.ScrollbackCount())
	}
}

func TestHarness_terminalSetScrollbackSize(t *testing.T) {
	t.Parallel()
	h := newTerminalHarness(t, 4, 3)
	h.SetScrollbackSize(0)
	for i := range 20 {
		_, _ = fmt.Fprintf(h.session, "x%d\n", i)
	}
	if got := h.ScrollbackCount(); got != 0 {
		t.Fatalf("TerminalScrollbackCount = %d, want 0 with scrollback disabled", got)
	}
}

func TestHarness_terminalAltScreen(t *testing.T) {
	t.Parallel()
	h := newTerminalHarness(t, 6, 3)
	if h.IsAltScreen() {
		t.Fatal("expected main screen initially")
	}
	_ = writeString(h, "\x1b[?1049h")
	if !h.IsAltScreen() {
		t.Fatal("expected alt screen after CSI ?1049h")
	}
	_ = writeString(h, "\x1b[?1049l")
	if h.IsAltScreen() {
		t.Fatal("expected main screen after leaving alt screen")
	}
}

func TestHarness_terminalFocusBlurMsgs(t *testing.T) {
	t.Parallel()
	h := newTerminalHarness(t, 4, 2)
	_ = writeString(h, ansi.SetModeFocusEvent)

	h.Focus()
	h.WaitMessage[tea.FocusMsg]()

	h.Blur()
	h.WaitMessage[tea.BlurMsg]()
}

func TestHarness_terminalTrackedHarnessFocus(t *testing.T) {
	t.Parallel()
	h := newTerminalHarness(t, 4, 2)
	if !h.IsFocused() {
		t.Fatal("expected initially focused harness state")
	}
	h.Blur()
	if h.IsFocused() {
		t.Fatal("expected Blur() to record unfocused state")
	}
	h.Focus()
	if !h.IsFocused() {
		t.Fatal("expected Focus() to record focused state")
	}
}

func TestHarness_terminalTrackedCursorDefaults(t *testing.T) {
	t.Parallel()
	h := newTerminalHarness(t, 4, 2)
	if !h.CursorVisible() || h.CursorBlink() || h.CursorStyle() != screen.CursorBlock {
		t.Fatalf("defaults: visible=%v blink=%v style=%v", h.CursorVisible(), h.CursorBlink(), h.CursorStyle())
	}
	pt := h.CursorPosition()
	if pt.X != 0 || pt.Y != 0 {
		t.Fatalf("initial cursor %#v, want (0,0)", pt)
	}
}

func TestHarness_terminalTrackedTitle(t *testing.T) {
	t.Parallel()
	const want = "steep-tracked-title"
	h := newTerminalHarness(t, 4, 2)
	_ = writeString(h, ansi.SetWindowTitle(want))
	if got := h.Title(); got != want {
		t.Fatalf("Title() = %q, want %q", got, want)
	}
}

func TestHarness_terminalTrackedBell(t *testing.T) {
	t.Parallel()
	h := newTerminalHarness(t, 4, 2)
	if !h.LastBellAt().IsZero() {
		t.Fatalf("LastBellAt() = %v, want zero time before first bell", h.LastBellAt())
	}

	_ = writeString(h, string(rune(ansi.BEL)))

	first := h.LastBellAt()
	if first.IsZero() {
		t.Fatal("expected LastBellAt non-zero after BEL")
	}

	_ = writeString(h, string(rune(ansi.BEL)))

	second := h.LastBellAt()
	if second.Before(first) {
		t.Fatalf("LastBellAt regressed: %v before %v", second, first)
	}
}

func TestHarness_terminalTrackedModeDEC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		seq     string
		mode    ansi.Mode
		want    ansi.ModeSetting
		cleanup string
	}{
		{
			name:    "mouse button reporting on then off",
			seq:     ansi.SetModeMouseButtonEvent,
			mode:    ansi.ModeMouseButtonEvent,
			want:    ansi.ModeSet,
			cleanup: ansi.ResetModeMouseButtonEvent,
		},
		{
			name:    "focus event on then off",
			seq:     ansi.SetModeFocusEvent,
			mode:    ansi.ModeFocusEvent,
			want:    ansi.ModeSet,
			cleanup: ansi.ResetModeFocusEvent,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			local := newTerminalHarness(t, 4, 2)
			_ = writeString(local, tt.seq)
			if got := local.ModeSetting(tt.mode); got != tt.want {
				t.Fatalf("after enable: ModeSetting(%v)=%v want %v", tt.mode, got, tt.want)
			}
			_ = writeString(local, tt.cleanup)
			if got := local.ModeSetting(tt.mode); got != ansi.ModeReset {
				t.Fatalf("after reset: ModeSetting(%v)=%v want Reset", tt.mode, got)
			}
		})
	}
}

func TestHarness_terminalTrackedModeUnsetKey(t *testing.T) {
	t.Parallel()
	h := newTerminalHarness(t, 3, 2)
	unset := ansi.DECMode(0xbeef)
	if h.ModeSetting(unset) != ansi.ModeNotRecognized {
		t.Fatalf("unset mode setting = %v, want ModeNotRecognized", h.ModeSetting(unset))
	}
}

func TestHarness_terminalTrackedCursorHideShow(t *testing.T) {
	t.Parallel()
	h := newTerminalHarness(t, 6, 2)
	_ = writeString(h, ansi.HideCursor)
	if h.CursorVisible() {
		t.Fatal("expected hidden cursor after HideCursor sequence")
	}
	_ = writeString(h, ansi.ShowCursor)
	if !h.CursorVisible() {
		t.Fatal("expected visible cursor after ShowCursor sequence")
	}
}

func TestHarness_terminalPasteMsg(t *testing.T) {
	t.Parallel()
	h := newTerminalHarness(t, 4, 2)
	_ = writeString(h, ansi.SetModeBracketedPaste)

	const pasted = "hello"
	h.Paste(pasted)

	got := h.WaitMessageWhere(func(m tea.PasteMsg) bool {
		return m.Content == pasted
	})
	if got.Content != pasted {
		t.Fatalf("PasteMsg = %#v, want Content %q", got, pasted)
	}
}

func TestHarness_TerminalType_deliversPrintableRunesThroughEmulator(t *testing.T) {
	t.Parallel()
	h := NewHarness(t, rootTestModel{}, WithWindowSize(80, 3))
	h.Type("ab ").Wait("text=ab").Require("text=ab")
}

func TestHarness_TerminalKey_accumulatesPrintableText(t *testing.T) {
	t.Parallel()
	h := NewHarness(t, rootTestModel{}, WithWindowSize(80, 3))
	h.Key("a").Key("b").Key("space").Wait("text=ab").Require("text=ab")
}

func enableEmulatorMouseReporting(tb testing.TB, h *Harness) {
	tb.Helper()
	_ = writeString(h, ansi.SetModeMouseButtonEvent)
	_ = writeString(h, ansi.SetModeMouseExtSgr)
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
	t.Parallel()
	h := newTerminalHarness(t, 14, 5)
	h.WaitMessage[tea.WindowSizeMsg]()

	enableEmulatorMouseReporting(t, h)
	n := len(slices.Collect(h.MessageHistory()))

	h.Mouse(vt.MousePress, vt.MouseLeft, 4, 2)
	WaitSettleMessages(t, h)

	got := collectMouseTail(t, h, n)
	if len(got) != 1 || got[0].kind != "click" || got[0].X != 4 || got[0].Y != 2 || got[0].Button != tea.MouseLeft {
		t.Fatalf("unexpected mouse tail: %#v", got)
	}
}

func TestHarness_TerminalMouseClick_deliversPressAndRelease(t *testing.T) {
	t.Parallel()
	h := newTerminalHarness(t, 12, 4)
	h.WaitMessage[tea.WindowSizeMsg]()
	enableEmulatorMouseReporting(t, h)
	n := len(slices.Collect(h.MessageHistory()))

	h.MouseClick(vt.MouseLeft, 3, 2)
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
	t.Parallel()
	tests := []struct {
		name string
		fn   func(*Harness, int, int) *Harness
		want tea.MouseButton
	}{
		{name: "left", fn: (*Harness).LeftClick, want: tea.MouseLeft},
		{name: "middle", fn: (*Harness).MiddleClick, want: tea.MouseMiddle},
		{name: "right", fn: (*Harness).RightClick, want: tea.MouseRight},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newTerminalHarness(t, 10, 5)
			h.WaitMessage[tea.WindowSizeMsg]()
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
	t.Parallel()
	h := newTerminalHarness(t, 16, 6)
	h.WaitMessage[tea.WindowSizeMsg]()
	enableEmulatorMouseReporting(t, h)
	n := len(slices.Collect(h.MessageHistory()))

	// (1,1) -> (3,1): press, motion at (2,1), motion at (3,1), release.
	h.MouseDrag(vt.MouseLeft, 1, 1, 3, 1)
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
	t.Parallel()
	h := newTerminalHarness(t, 12, 4)
	h.WaitMessage[tea.WindowSizeMsg]()
	enableEmulatorMouseReporting(t, h)

	n0 := len(slices.Collect(h.MessageHistory()))
	h.MouseDrag(vt.MouseLeft, 0, 0, 1, 0)
	WaitSettleMessages(t, h)
	a := collectMouseTail(t, h, n0)

	n1 := len(slices.Collect(h.MessageHistory()))
	h.LeftDrag(0, 0, 1, 0)
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
	t.Parallel()
	h := newTerminalHarness(t, 8, 4)
	h.WaitMessage[tea.WindowSizeMsg]()
	enableEmulatorMouseReporting(t, h)
	n := len(slices.Collect(h.MessageHistory()))

	h.MouseDrag(vt.MouseRight, 2, 2, 2, 2)
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
	t.Parallel()
	h := newTerminalHarness(t, 8, 4)
	h.WaitMessage[tea.WindowSizeMsg]()
	n := len(slices.Collect(h.MessageHistory()))
	h.LeftClick(2, 2)
	WaitSettleMessages(t, h)
	for _, msg := range slices.Collect(h.MessageHistory())[n:] {
		switch msg.(type) {
		case tea.MouseClickMsg, tea.MouseMotionMsg, tea.MouseReleaseMsg, tea.MouseWheelMsg:
			t.Fatalf("unexpected mouse message without DEC mouse modes: %T", msg)
		default:
		}
	}
}

func osc7501(body string) string { return "\x1b]7501;" + body + "\x1b\\" }

// writeString writes s to the harness's session as if the program had.
func writeString(h *Harness, s string) error {
	_, err := h.session.Write([]byte(s))
	return err
}

func writeVT(t *testing.T, h *Harness, s string) {
	t.Helper()
	if err := writeString(h, s); err != nil {
		t.Fatalf("write vt: %v", err)
	}
}

func TestProgramStatusProtocol(t *testing.T) {
	t.Parallel()
	h := newTerminalHarness(t, 8, 2)
	before := h.session.Terminal().Text()

	stream := osc7501("?") + osc7501("state=blocked:kind=question:progress=42:app=agent:id=job:title=VGl0bGU=:msg=SGVsbG8=")
	for i := range len(stream) {
		if err := writeString(h, stream[i:i+1]); err != nil {
			t.Fatalf("write vt: %v", err)
		}
	}
	after := h.session.Terminal().Text()

	if before != after {
		t.Fatal("program status changed the screen")
	}
	h.WaitMessage[tea.ProgramStatusSupportMsg]()

	got := h.ProgramStatuses()
	if len(got) != 1 {
		t.Fatalf("records = %+v", got)
	}
	want := ansi.ProgramStatus{
		State: ansi.ProgramStateBlocked, Kind: ansi.ProgramStatusKindQuestion,
		Progress: 42, HasProgress: true, App: "agent", ID: "job",
		Title: "Title", Message: "Hello",
	}
	if got[0] != want {
		t.Fatalf("record = %+v, want %+v", got[0], want)
	}

	cloned := h.ProgramStatuses()
	cloned[0].State = ansi.ProgramStateError
	writeVT(t, h, osc7501("state=done:id=job"))
	got = h.ProgramStatuses()
	want = ansi.ProgramStatus{State: ansi.ProgramStateDone, ID: "job"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("replacement retained fields: %+v", got)
	}

	for _, bad := range []string{
		"state=unknown:id=job",
		"state=error:id=job:msg=A",
		"state=error:id=job:msg=Gwo=",
		"state=error:id=/",
		"state=error:id=job:msg=" + strings.Repeat("A", 2800),
	} {
		writeVT(t, h, osc7501(bad))
		got = h.ProgramStatuses()
		if len(got) != 1 || got[0] != want {
			t.Fatalf("invalid report applied: %q: %+v", bad, got)
		}
	}
}

func TestProgramStatusLifecycle(t *testing.T) {
	t.Parallel()
	h := newTerminalHarness(t, 8, 2)
	writeVT(t, h, osc7501("state=idle:app=root")+
		osc7501("state=working:id=build:app=compiler")+
		osc7501("state=blocked:id=build/test")+
		osc7501("state=done:id=build2"))
	got := h.ProgramStatuses()
	if len(got) != 4 || got[2].App != "compiler" || got[3].App != "root" {
		t.Fatalf("inheritance: %+v", got)
	}
	writeVT(t, h, osc7501("state=clear:id=build"))
	got = h.ProgramStatuses()
	if len(got) != 2 || got[1].ID != "build2" {
		t.Fatalf("subtree clear: %+v", got)
	}

	writeVT(t, h, osc7501("state=working:id=w")+osc7501("state=blocked:id=b")+osc7501("state=error:id=e"))
	writeVT(t, h, "\x1b]133;A\x1b\\")
	got = h.ProgramStatuses()
	if len(got) != 3 || got[2].State != ansi.ProgramStateError {
		t.Fatalf("prompt: %+v", got)
	}

	writeVT(t, h, "\x1b[?1049h\x1b[?1049l\x1b[!p")
	if len(h.ProgramStatuses()) != 3 {
		t.Fatal("screen switch or soft reset cleared status")
	}
	writeVT(t, h, "\x1bc")
	if len(h.ProgramStatuses()) != 0 {
		t.Fatal("RIS retained status")
	}
	writeVT(t, h, osc7501("state=done:id=x")+osc7501("state=clear:msg=A"))
	if len(h.ProgramStatuses()) != 1 {
		t.Fatal("invalid clear removed records")
	}
	writeVT(t, h, osc7501("state=clear"))
	if len(h.ProgramStatuses()) != 0 {
		t.Fatal("root clear retained status")
	}
}

func TestProgramStatusBoundedEviction(t *testing.T) {
	t.Parallel()
	h := newTerminalHarness(t, 4, 1)
	var b strings.Builder
	for i := range programStatusMaxRecords {
		fmt.Fprintf(&b, osc7501("state=working:id=j%03d"), i)
	}
	b.WriteString(osc7501("state=done:id=j000"))
	b.WriteString(osc7501("state=idle:id=new"))
	writeVT(t, h, b.String())

	got := h.ProgramStatuses()
	if len(got) != programStatusMaxRecords {
		t.Fatalf("records = %d, want %d", len(got), programStatusMaxRecords)
	}
	for _, s := range got {
		if s.ID == "j001" {
			t.Fatal("least recently updated record survived")
		}
	}
	var updated ansi.ProgramStatus
	for _, s := range got {
		if s.ID == "j000" {
			updated = s
		}
	}
	if updated.State != ansi.ProgramStateDone {
		t.Fatalf("updated record = %+v", updated)
	}
}

func TestProgramStatusInSnapshot(t *testing.T) {
	t.Parallel()
	h := newTerminalHarness(t, 4, 1)
	writeVT(t, h, osc7501("state=working:id=job:progress=0:app=demo"))
	snap := screenSnapshot(t, h.session)
	if len(snap.ProgramStatus) != 1 || snap.ProgramStatus[0].State != "working" {
		t.Fatalf("snapshot status = %+v", snap.ProgramStatus)
	}
	if snap.ProgramStatus[0].Progress == nil || *snap.ProgramStatus[0].Progress != 0 {
		t.Fatalf("progress = %v, want 0", snap.ProgramStatus[0].Progress)
	}
}
