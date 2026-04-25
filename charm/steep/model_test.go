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

func TestModelHarness(t *testing.T) {
	tm := NewModel(t, rootTestModel{}, WithInitialTermSize(12, 3))

	tm.WaitContainsString(t, "size=12x3")
	tm.ExpectStringContains(t, "size=12x3")
	tm.ExpectWidth(t, 9)
	tm.ExpectHeight(t, 2)
	tm.ExpectDimensions(t, 9, 2)

	tm.Type("ab")
	tm.WaitContains(t, []byte("text=ab"))

	tm.Send(setTextMsg("done"))
	tm.WaitContainsString(t, "text=done")
	tm.WaitNotContainsString(t, "text=ab")
	tm.ExpectStringNotContains(t, "text=ab")

	if len(tm.Messages()) < 4 {
		t.Fatalf("expected at least 4 messages, got %d", len(tm.Messages()))
	}

	if err := tm.Quit(); err != nil {
		t.Fatalf("quit failed: %v", err)
	}
	tm.WaitFinished(t, WithFinalTimeout(time.Second))

	out := string(tm.FinalOutput(t))
	if !strings.Contains(out, "text=done") {
		t.Fatalf("final output = %q, want text=done", out)
	}

	finalModel, ok := tm.FinalModel(t).(rootTestModel)
	if !ok {
		t.Fatalf("final model type = %T, want rootTestModel", tm.FinalModel(t))
	}
	if finalModel.text != "done" {
		t.Fatalf("final model text = %q, want done", finalModel.text)
	}
}

func TestModelRequirePlainSnapshotUsesCurrentOutput(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("UPDATE_SNAPSHOTS", "true")

	tm := NewModel(t, rootTestModel{text: "\x1b[31mred\x1b[0m"})
	t.Cleanup(func() {
		if err := tm.Quit(); err != nil {
			t.Fatalf("quit failed: %v", err)
		}
		tm.WaitFinished(t, WithFinalTimeout(time.Second))
	})

	tm.WaitContainsString(t, "size=80x24", "red")
	tm.RequirePlainSnapshot(t)

	got := readSteepSnapshot(t, "TestModelRequirePlainSnapshotUsesCurrentOutput.snap")
	if got != "size=80x24\ntext=red" {
		t.Fatalf("snapshot = %q, want current plain output", got)
	}
}
