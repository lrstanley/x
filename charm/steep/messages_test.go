// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

type otherMsg struct{}

func TestMessagesOfType(t *testing.T) {
	messages := []tea.Msg{
		appendMsg("a"),
		otherMsg{},
		appendMsg("b"),
	}

	got := MessagesOfType[appendMsg](messages)
	if len(got) != 2 {
		t.Fatalf("messages = %d, want 2", len(got))
	}
	if got[0] != "a" || got[1] != "b" {
		t.Fatalf("messages = %#v, want [a b]", got)
	}
}

func TestWaitForMessageWhere(t *testing.T) {
	vm := NewViewModel(t, &mutableViewModel{})
	vm.Send(appendMsg("first"))
	vm.Send(appendMsg("second"))

	got := WaitForMessageWhere(t, vm, func(msg appendMsg) bool {
		return msg == "second"
	})
	if got != "second" {
		t.Fatalf("message = %q, want second", got)
	}

	matches := WaitForMessages[appendMsg](t, vm)
	if len(matches) != 2 {
		t.Fatalf("messages = %d, want 2", len(matches))
	}
}
