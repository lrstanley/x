// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

type otherMsg struct{}

func TestMessagesOfType(t *testing.T) {
	messages := []uv.Event{
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

func TestWaitMessageWhere(t *testing.T) {
	h := NewComponentHarness(t, &mutableViewModel{})
	h.Send(appendMsg("first"))
	h.Send(appendMsg("second"))

	got := WaitMessageWhere(t, h, func(msg appendMsg) bool {
		return msg == "second"
	})
	if got != "second" {
		t.Fatalf("message = %q, want second", got)
	}

	matches := WaitMessages[appendMsg](t, h)
	if len(matches) != 2 {
		t.Fatalf("messages = %d, want 2", len(matches))
	}
}
