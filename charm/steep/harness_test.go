// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

type setTextMsg string

type rootTestModel struct {
	width  int
	height int
	text   string
}

func (m rootTestModel) Init() tea.Cmd {
	return nil
}

func (m rootTestModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyPressMsg:
		m.text += msg.Key().Text
	case setTextMsg:
		m.text = string(msg)
	case tea.QuitMsg:
		return m, tea.Quit
	}
	return m, nil
}

func (m rootTestModel) View() tea.View {
	return tea.NewView(fmt.Sprintf("size=%dx%d\ntext=%s", m.width, m.height, m.text))
}

func TestHarness(t *testing.T) {
	h := NewHarness(t, rootTestModel{}, WithInitialTermSize(12, 3))

	h.WaitContainsString(t, "size=12x3")
	h.RequireStringContains(t, "size=12x3")
	h.RequireWidth(t, 9)
	h.RequireHeight(t, 2)
	h.RequireDimensions(t, 9, 2)

	h.Type("ab")
	h.WaitContainsBytes(t, []byte("text=ab"))

	h.Send(setTextMsg("done"))
	h.WaitContainsString(t, "text=done")
	h.WaitNotContainsString(t, "text=ab")
	h.RequireStringNotContains(t, "text=ab")

	if len(h.Messages()) < 4 {
		t.Fatalf("expected at least 4 messages, got %d", len(h.Messages()))
	}

	if err := h.Quit(); err != nil {
		t.Fatalf("quit failed: %v", err)
	}
	h.WaitFinished(t, WithTimeout(time.Second))

	out := h.FinalView(t)
	if !strings.Contains(out, "text=done") {
		t.Fatalf("final output = %q, want text=done", out)
	}

	finalModel, ok := h.FinalModel(t).(rootTestModel)
	if !ok {
		t.Fatalf("final model type = %T, want rootTestModel", h.FinalModel(t))
	}
	if finalModel.text != "done" {
		t.Fatalf("final model text = %q, want done", finalModel.text)
	}
}

func TestHarnessMutateRootModel(t *testing.T) {
	h := NewHarness(t, rootTestModel{text: "start"})

	Mutate(t, h, func(m rootTestModel) rootTestModel {
		m.text = "mutated"
		return m
	})

	got := h.View()
	if !strings.Contains(got, "text=mutated") {
		t.Fatalf("view = %q, want text=mutated", got)
	}
	if len(MessagesOfType[mutateMsg[rootTestModel]](h.Messages())) != 0 {
		t.Fatalf("mutate messages should not be exposed")
	}
}

func TestHarnessRequirePlainSnapshotUsesCurrentOutput(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("UPDATE_SNAPSHOTS", "true")

	h := NewHarness(t, rootTestModel{text: "\x1b[31mred\x1b[0m"})

	h.WaitContainsStrings(t, []string{"size=80x24", "red"})
	h.RequireSnapshotNoANSI(t)

	got := readSteepSnapshot(t, "TestHarnessRequirePlainSnapshotUsesCurrentOutput.snap")
	if got != "size=80x24\ntext=red" {
		t.Fatalf("snapshot = %q, want current plain output", got)
	}
}
