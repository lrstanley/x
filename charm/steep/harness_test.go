// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
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

func (m rootTestModel) Update(msg uv.Event) (tea.Model, tea.Cmd) {
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
	t.Parallel()
	h := NewHarness(t, rootTestModel{}, WithWindowSize(12, 3))

	h.WaitString("size=12x3").
		RequireString("size=12x3").
		RequireWidth(9).
		RequireHeight(2).
		RequireDimensions(9, 2).
		Type("ab").WaitBytes([]byte("text=ab")).
		Send(setTextMsg("done")).WaitString("text=done").WaitNotString("text=ab").RequireNotString("text=ab")

	msgs := slices.Collect(h.MessageHistory())
	if len(msgs) < 4 {
		t.Fatalf("expected at least 4 messages, got %d", len(msgs))
	}

	h.Quit()
	h.WaitFinished(WithTimeout(time.Second))

	out := h.FinalView()
	if !strings.Contains(out, "text=done") {
		t.Fatalf("final output = %q, want text=done", out)
	}

	finalModel, ok := h.FinalModel().(rootTestModel)
	if !ok {
		t.Fatalf("final model type = %T, want rootTestModel", h.FinalModel())
	}
	if finalModel.text != "done" {
		t.Fatalf("final model text = %q, want done", finalModel.text)
	}
}

func TestHarnessMutateRootModel(t *testing.T) {
	t.Parallel()
	h := NewHarness(t, rootTestModel{text: "start"})

	Mutate(h, func(m rootTestModel) rootTestModel {
		m.text = "mutated"
		return m
	})

	got := h.View()
	if !strings.Contains(got, "text=mutated") {
		t.Fatalf("view = %q, want text=mutated", got)
	}
	if len(slices.Collect(FilterMessagesType[mutateMsg[rootTestModel]](t, h.MessageHistory()))) != 0 {
		t.Fatalf("mutate messages should not be exposed")
	}
}

func TestHarness_Send_returnsHarnessForChaining(t *testing.T) {
	t.Parallel()
	h := NewHarness(t, rootTestModel{})
	if ptr := h.Send(setTextMsg("x")); ptr != h {
		t.Fatalf("Send should return the same harness, got %p want %p", ptr, h)
	}
}

func TestHarness_Type_returnsHarnessForChaining(t *testing.T) {
	t.Parallel()
	h := NewHarness(t, rootTestModel{})
	if ptr := h.Type(""); ptr != h {
		t.Fatalf("Type should return the same harness, got %p want %p", ptr, h)
	}
}

func TestHarness_Quit_returnsHarnessForChaining(t *testing.T) {
	t.Parallel()
	h := NewHarness(t, rootTestModel{})
	if ptr := h.Quit(); ptr != h {
		t.Fatalf("Quit should return the same harness, got %p want %p", ptr, h)
	}
	h.WaitFinished(WithTimeout(time.Second))
}

func TestHarness_Key_returnsHarnessForChaining(t *testing.T) {
	t.Parallel()
	h := NewHarness(t, rootTestModel{})
	if ptr := h.Key("z"); ptr != h {
		t.Fatalf("Key should return the same harness, got %p want %p", ptr, h)
	}
}

func TestHarness_Key_accumulatesPrintableText(t *testing.T) {
	t.Parallel()
	h := NewHarness(t, rootTestModel{})
	h.Key("a").Key("b").Key(" ").WaitString("text=ab ").RequireString("text=ab ")
}

func TestHarness_Key_mapsNamedKeys(t *testing.T) {
	t.Parallel()
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
		{name: "escape alias escape", key: "escape", wantCode: tea.KeyEscape},
		{name: "backspace", key: "backspace", wantCode: tea.KeyBackspace},
		{name: "delete", key: "delete", wantCode: tea.KeyDelete},
		{name: "arrow up", key: "up", wantCode: tea.KeyUp},
		{name: "arrow down", key: "down", wantCode: tea.KeyDown},
		{name: "arrow left", key: "left", wantCode: tea.KeyLeft},
		{name: "arrow right", key: "right", wantCode: tea.KeyRight},
		{name: "home", key: "home", wantCode: tea.KeyHome},
		{name: "end", key: "end", wantCode: tea.KeyEnd},
		{name: "pgup", key: "pgup", wantCode: tea.KeyPgUp},
		{name: "pageup alias", key: "pageup", wantCode: tea.KeyPgUp},
		{name: "pgdown", key: "pgdown", wantCode: tea.KeyPgDown},
		{name: "pagedown alias", key: "pagedown", wantCode: tea.KeyPgDown},
		{name: "shift+tab", key: "shift+tab", wantCode: tea.KeyTab, wantMod: tea.ModShift},
		{name: "ctrl lowercase letter", key: "ctrl+g", wantCode: 'g', wantMod: tea.ModCtrl},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := NewHarness(t, rootTestModel{})
			h.Key(tt.key)
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

func TestHarness_Key_fallbackMultiCharacterLiteralText(t *testing.T) {
	t.Parallel()
	const arbitrary = "custom-key-token"
	h := NewHarness(t, rootTestModel{})
	h.Key(arbitrary)
	got := WaitMessageWhere(t, h, func(msg uv.Event) bool {
		km, ok := msg.(tea.KeyPressMsg)
		return ok && km.Key().Text == arbitrary && km.Key().Mod == 0
	})
	km := got.(tea.KeyPressMsg)
	if km.Key().Text != arbitrary {
		t.Fatalf("Key.Text = %q, want %q", km.Key().Text, arbitrary)
	}
}
