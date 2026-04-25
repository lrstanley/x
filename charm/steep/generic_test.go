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

	tea "charm.land/bubbletea/v2"
)

type appendMsg string

type mutableViewModel struct {
	text string
}

func (m *mutableViewModel) View() string {
	return "text=" + m.text
}

func (m *mutableViewModel) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		m.text += msg.Key().Text
	case appendMsg:
		m.text += string(msg)
		if msg == "?" {
			return func() tea.Msg {
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

func (m replacementViewModel) Update(msg tea.Msg) (any, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		m.text += msg.Key().Text
	case appendMsg:
		m.text += string(msg)
	}
	return m, nil
}

func TestViewModelMutableUpdate(t *testing.T) {
	vm := NewViewModel(t, &mutableViewModel{})
	cleanupTestModel(t, vm)

	vm.Type("ab")
	vm.WaitContains(t, []byte("text=ab"))
	vm.ExpectStringContains(t, "text=ab")

	vm.Send(appendMsg("?"))
	vm.WaitContainsString(t, "text=ab?!")
	vm.WaitNotContains(t, []byte("missing"))
	vm.ExpectStringContains(t, "text=ab?!")
	vm.ExpectStringNotContains(t, "missing")
	vm.ExpectWidth(t, 9)
	vm.ExpectHeight(t, 1)
	vm.ExpectDimensions(t, 9, 1)

	if len(vm.Messages()) < 5 {
		t.Fatalf("messages = %d, want at least 5", len(vm.Messages()))
	}
}

func TestViewModelReplacementUpdate(t *testing.T) {
	vm := NewViewModel(t, replacementViewModel{})
	cleanupTestModel(t, vm)

	vm.Type("go")
	vm.Send(appendMsg("!"))
	vm.WaitFor(t, func(bts []byte) bool {
		return strings.Contains(string(bts), "text=go!")
	})
}

type sizeViewModel struct {
	width  int
	height int
}

func (m *sizeViewModel) View() string {
	return fmt.Sprintf("size=%dx%d", m.width, m.height)
}

func (m *sizeViewModel) Update(msg tea.Msg) tea.Cmd {
	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = msg.Width
		m.height = msg.Height
	}
	return nil
}

func TestViewModelInitialSize(t *testing.T) {
	t.Run("uses program default size", func(t *testing.T) {
		vm := NewViewModel(t, &sizeViewModel{})
		cleanupTestModel(t, vm)

		vm.WaitContainsString(t, "size=80x24")
		msg := WaitForMessage[tea.WindowSizeMsg](t, vm)
		if msg.Width != 80 || msg.Height != 24 {
			t.Fatalf("initial size = %dx%d, want 80x24", msg.Width, msg.Height)
		}
	})

	t.Run("explicit size", func(t *testing.T) {
		vm := NewViewModel(t, &sizeViewModel{}, WithInitialTermSize(70, 10))
		cleanupTestModel(t, vm)

		vm.WaitContainsString(t, "size=70x10")
		msg := WaitForMessage[tea.WindowSizeMsg](t, vm)
		if msg.Width != 70 || msg.Height != 10 {
			t.Fatalf("initial size = %dx%d, want 70x10", msg.Width, msg.Height)
		}
	})

	t.Run("explicit zero size", func(t *testing.T) {
		vm := NewViewModel(t, &sizeViewModel{}, WithInitialTermSize(0, 0))
		cleanupTestModel(t, vm)

		vm.WaitContainsString(t, "size=0x0")
		if len(vm.Messages()) != 0 {
			t.Fatalf("messages = %d, want 0", len(vm.Messages()))
		}
	})
}

type asyncViewModel struct {
	width  int
	height int
	text   string
}

func (m *asyncViewModel) Init() tea.Cmd {
	return func() tea.Msg {
		return appendMsg("ready")
	}
}

func (m *asyncViewModel) View() string {
	return fmt.Sprintf("size=%dx%d\ntext=%s", m.width, m.height, m.text)
}

func (m *asyncViewModel) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case appendMsg:
		m.text += string(msg)
	}
	return nil
}

func TestViewModelAsyncBridge(t *testing.T) {
	tm := NewViewModel(t, &asyncViewModel{}, WithInitialTermSize(33, 4))
	cleanupTestModel(t, tm)

	tm.WaitContainsString(t, "size=33x4", "text=ready")

	msg := WaitForMessage[appendMsg](t, tm)
	if msg != "ready" {
		t.Fatalf("message = %q, want ready", msg)
	}
}

func TestViewModelRequirePlainSnapshot(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("UPDATE_SNAPSHOTS", "true")

	vm := NewViewModel(t, &mutableViewModel{text: "\x1b[31mred\x1b[0m"})
	cleanupTestModel(t, vm)
	vm.WaitContainsString(t, "red")
	vm.RequirePlainSnapshot(t)

	got := readSteepSnapshot(t, "TestViewModelRequirePlainSnapshot.snap")
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

func cleanupTestModel(t *testing.T, tm *Model) {
	t.Helper()

	t.Cleanup(func() {
		if err := tm.Quit(); err != nil {
			t.Fatalf("quit failed: %v", err)
		}
		tm.WaitFinished(t)
	})
}
