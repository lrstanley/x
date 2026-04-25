// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
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

	if len(vm.Messages()) != 4 {
		t.Fatalf("messages = %d, want 4", len(vm.Messages()))
	}
	if err := vm.Quit(); err != nil {
		t.Fatalf("quit failed: %v", err)
	}
	vm.WaitFinished(t)
}

func TestViewModelReplacementUpdate(t *testing.T) {
	vm := NewViewModel(t, replacementViewModel{})

	vm.Type("go")
	vm.Send(appendMsg("!"))
	vm.WaitFor(t, func(bts []byte) bool {
		return strings.Contains(string(bts), "text=go!")
	})

	out := string(vm.FinalOutput(t))
	if out != "text=go!" {
		t.Fatalf("final output = %q, want text=go!", out)
	}

	finalModel, ok := vm.FinalModel(t).(replacementViewModel)
	if !ok {
		t.Fatalf("final model type = %T, want replacementViewModel", vm.FinalModel(t))
	}
	if finalModel.text != "go!" {
		t.Fatalf("final model text = %q, want go!", finalModel.text)
	}
}
