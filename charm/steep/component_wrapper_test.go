// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/charm/steep/snapshot"
)

type appendMsg string

type mutableViewModel struct {
	text string
}

func (m *mutableViewModel) View() string {
	return "text=" + m.text
}

func (m *mutableViewModel) Update(msg uv.Event) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		m.text += msg.Key().Text
	case appendMsg:
		m.text += string(msg)
		if msg == "?" {
			return func() uv.Event {
				return appendMsg("!")
			}
		}
	}
	return nil
}

type replacementViewModel struct {
	text string
}

func (m replacementViewModel) View() string {
	return "text=" + m.text
}

func (m replacementViewModel) Update(msg uv.Event) (replacementViewModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		m.text += msg.Key().Text
	case appendMsg:
		m.text += string(msg)
	}
	return m, nil
}

func TestComponentHarnessMutableUpdate(t *testing.T) {
	h := NewComponentHarness(t, &mutableViewModel{})

	h.Type("ab")
	h.WaitBytes([]byte("text=ab"))
	h.RequireString("text=ab")

	h.Send(appendMsg("?"))
	h.WaitString("text=ab?!")
	h.WaitNotBytes([]byte("missing"))
	h.RequireString("text=ab?!")
	h.RequireNotString("missing")
	h.RequireWidth(9)
	h.RequireHeight(1)
	h.RequireDimensions(9, 1)

	if len(h.Messages()) < 5 {
		t.Fatalf("messages = %d, want at least 5", len(h.Messages()))
	}
}

func TestComponentHarnessReplacementUpdate(t *testing.T) {
	h := NewComponentHarness(t, replacementViewModel{})

	h.Type("go")
	h.Send(appendMsg("!"))
	h.WaitBytesFunc(func(bts []byte) bool {
		return strings.Contains(string(bts), "text=go!")
	})
}

func TestComponentHarnessMutateMutableModel(t *testing.T) {
	h := NewComponentHarness(t, &mutableViewModel{text: "start"})

	Mutate(h, func(m *mutableViewModel) *mutableViewModel {
		m.text = "mutated"
		return m
	})
	h.Send(appendMsg("!"))

	h.WaitString("text=mutated!")
}

func TestComponentHarnessMutateReplacementModel(t *testing.T) {
	h := NewComponentHarness(t, replacementViewModel{text: "start"})

	Mutate(h, func(m replacementViewModel) replacementViewModel {
		m.text = "mutated"
		return m
	})
	h.Send(appendMsg("!"))

	h.WaitString("text=mutated!")
}

type sizeViewModel struct {
	width  int
	height int
}

func (m *sizeViewModel) View() string {
	return fmt.Sprintf("size=%dx%d", m.width, m.height)
}

func (m *sizeViewModel) Update(msg uv.Event) tea.Cmd {
	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = msg.Width
		m.height = msg.Height
	}
	return nil
}

func TestComponentHarnessInitialSize(t *testing.T) {
	t.Run("uses program default size", func(t *testing.T) {
		h := NewComponentHarness(t, &sizeViewModel{})

		h.WaitString("size=80x24")
		msg := WaitMessage[tea.WindowSizeMsg](t, h)
		if msg.Width != 80 || msg.Height != 24 {
			t.Fatalf("initial size = %dx%d, want 80x24", msg.Width, msg.Height)
		}
	})

	t.Run("explicit size", func(t *testing.T) {
		h := NewComponentHarness(t, &sizeViewModel{}, WithInitialTermSize(70, 10))

		h.WaitString("size=70x10")
		msg := WaitMessage[tea.WindowSizeMsg](t, h)
		if msg.Width != 70 || msg.Height != 10 {
			t.Fatalf("initial size = %dx%d, want 70x10", msg.Width, msg.Height)
		}
	})

	t.Run("explicit zero size", func(t *testing.T) {
		h := NewComponentHarness(t, &sizeViewModel{}, WithInitialTermSize(0, 0))

		h.WaitString("size=0x0")
		msg := WaitMessage[tea.WindowSizeMsg](t, h)
		if msg.Width != 0 || msg.Height != 0 {
			t.Fatalf("initial size = %dx%d, want 0x0", msg.Width, msg.Height)
		}
	})
}

type asyncViewModel struct {
	width  int
	height int
	text   string
}

func (m *asyncViewModel) Init() tea.Cmd {
	return func() uv.Event {
		return appendMsg("ready")
	}
}

func (m *asyncViewModel) View() string {
	return fmt.Sprintf("size=%dx%d\ntext=%s", m.width, m.height, m.text)
}

func (m *asyncViewModel) Update(msg uv.Event) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case appendMsg:
		m.text += string(msg)
	}
	return nil
}

func TestComponentHarnessAsyncBridge(t *testing.T) {
	h := NewComponentHarness(t, &asyncViewModel{}, WithInitialTermSize(33, 4))

	h.WaitStrings([]string{"size=33x4", "text=ready"})

	msg := WaitMessage[appendMsg](t, h)
	if msg != "ready" {
		t.Fatalf("message = %q, want ready", msg)
	}
}

type settleMsg struct{}

type settlingViewModel struct {
	updates int
}

func (m *settlingViewModel) View() string {
	return fmt.Sprintf("updates=%d", m.updates)
}

func (m *settlingViewModel) Update(msg uv.Event) tea.Cmd {
	if _, ok := msg.(settleMsg); !ok {
		return nil
	}

	m.updates++
	if m.updates >= 3 {
		return nil
	}

	return tea.Tick(10*time.Millisecond, func(time.Time) uv.Event {
		return settleMsg{}
	})
}

func TestComponentHarnessWaitSettled(t *testing.T) {
	h := NewComponentHarness(t, &settlingViewModel{})
	h.Send(settleMsg{})
	h.WaitString("updates=3")

	h.WaitSettleMessages(
		WithSettleTimeout(25*time.Millisecond),
		WithTimeout(500*time.Millisecond),
		WithCheckInterval(5*time.Millisecond),
	)
	out := h.View()
	if !strings.Contains(out, "updates=3") {
		t.Fatalf("output = %q, want updates=3", out)
	}

	matches := MessagesOfType[settleMsg](h.Messages())
	if len(matches) != 3 {
		t.Fatalf("settle messages = %d, want 3", len(matches))
	}
}

type settleNoiseTick struct{}

type settlingWithNoiseModel struct {
	updates int
}

func (m *settlingWithNoiseModel) View() string {
	return fmt.Sprintf("updates=%d", m.updates)
}

func (m *settlingWithNoiseModel) Init() tea.Cmd {
	return tea.Tick(5*time.Millisecond, func(time.Time) uv.Event {
		return settleNoiseTick{}
	})
}

func (m *settlingWithNoiseModel) Update(msg uv.Event) tea.Cmd {
	if _, ok := msg.(settleNoiseTick); ok {
		return tea.Tick(5*time.Millisecond, func(time.Time) uv.Event {
			return settleNoiseTick{}
		})
	}
	if _, ok := msg.(settleMsg); !ok {
		return nil
	}
	m.updates++
	if m.updates >= 2 {
		return nil
	}
	return tea.Tick(10*time.Millisecond, func(time.Time) uv.Event {
		return settleMsg{}
	})
}

func TestComponentHarnessWaitSettledIgnoreMsgs(t *testing.T) {
	h := NewComponentHarness(t, &settlingWithNoiseModel{})
	h.Send(settleMsg{})
	h.WaitString("updates=2")

	h.WaitSettleMessages(
		WithSettleIgnoreMsgs(settleNoiseTick{}),
		WithSettleTimeout(25*time.Millisecond),
		WithTimeout(500*time.Millisecond),
		WithCheckInterval(5*time.Millisecond),
	)
	if !strings.Contains(h.View(), "updates=2") {
		t.Fatalf("output = %q, want updates=2", h.View())
	}
	if len(MessagesOfType[settleNoiseTick](h.Messages())) < 3 {
		t.Fatalf("expected periodic noise ticks in message log")
	}
}

type viewSettleTick struct{}

type viewSettleStableModel struct {
	ticks int
}

func (m *viewSettleStableModel) View() string {
	return "stable"
}

func (m *viewSettleStableModel) Init() tea.Cmd {
	return tea.Tick(2*time.Millisecond, func(time.Time) uv.Event {
		return viewSettleTick{}
	})
}

func (m *viewSettleStableModel) Update(msg uv.Event) tea.Cmd {
	if _, ok := msg.(viewSettleTick); !ok {
		return nil
	}
	if m.ticks >= 15 {
		return nil
	}
	m.ticks++
	return tea.Tick(2*time.Millisecond, func(time.Time) uv.Event {
		return viewSettleTick{}
	})
}

func TestComponentHarnessWaitSettledView(t *testing.T) {
	h := NewComponentHarness(t, &viewSettleStableModel{})
	h.WaitString("stable")

	h.WaitSettleView(
		WithSettleTimeout(25*time.Millisecond),
		WithTimeout(2*time.Second),
		WithCheckInterval(5*time.Millisecond),
	)
	if !strings.Contains(h.View(), "stable") {
		t.Fatalf("view = %q, want stable", h.View())
	}
	if len(h.Messages()) < 5 {
		t.Fatalf("messages = %d, want at least 5 (view settled while msgs still arrived)", len(h.Messages()))
	}
}

func TestComponentHarnessSendFilterReceivesOriginalMessage(t *testing.T) {
	seen := make(chan struct{}, 1)
	h := NewComponentHarness(
		t,
		&mutableViewModel{},
		WithProgramOptions(tea.WithFilter(func(_ tea.Model, msg uv.Event) uv.Event {
			if _, ok := msg.(appendMsg); ok {
				select {
				case seen <- struct{}{}:
				default:
				}
			}
			return msg
		})),
	)

	h.Send(appendMsg("x"))
	h.WaitString("text=x")

	select {
	case <-seen:
	default:
		t.Fatalf("filter did not receive appendMsg")
	}
}

func TestComponentHarnessRequirePlainSnapshot(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("UPDATE_SNAPSHOTS", "true")

	h := NewComponentHarness(t, &mutableViewModel{text: "\x1b[31mred\x1b[0m"})
	h.WaitString("red")
	h.RequireViewSnapshot(snapshot.WithStripANSI())

	got := readSteepSnapshot(t, "TestComponentHarnessRequirePlainSnapshot.snap")
	if got != "text=red" {
		t.Fatalf("snapshot = %q, want text=red", got)
	}
}

func readSteepSnapshot(t *testing.T, name string) string {
	t.Helper()

	bts, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("failed to read snapshot: %v", err)
	}
	return string(bts)
}
