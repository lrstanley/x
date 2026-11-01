// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"testing"
	"time"

	"github.com/lrstanley/x/term/bloom/_examples/internal/demo"
	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/steep"
	"github.com/lrstanley/x/term/steep/snapshot"
)

func TestForms(t *testing.T) {
	t.Parallel()

	h := steep.NewHarness(t, newModel(), demo.Options("forms", 100, 26)...)
	h.Wait("Profile").Wait("focus: 0").Sleep(500 * time.Millisecond)

	h.Type("Ada Lovelace", reel.Delay(40*time.Millisecond)).Wait("Ada Lovelace").Sleep(300 * time.Millisecond)
	h.Key("tab").Type("ada@example.com", reel.Delay(40*time.Millisecond)).Wait("ada@example.com")
	if c := h.CursorPosition(); c.X == 0 {
		t.Fatalf("the cursor is not in the email input: %v", c)
	}

	// The dropdown is anchored below the language input; enter picks.
	h.Key("tab").Type("l").Wait("Haskell").Wait("Gleam").Sleep(500 * time.Millisecond)
	h.Type("e").Wait("Gleam").WaitNot("Haskell").Sleep(400 * time.Millisecond)
	h.Key("enter").WaitNot("start typing").Sleep(300 * time.Millisecond)

	// Saving asks once; the first save goes through.
	h.Key("tab").Key("tab").Wait("focus: save").Key("enter").Wait("Save profile?").Wait("in the form: false")
	h.Sleep(500 * time.Millisecond)
	h.Key("tab").Wait("focus: cancel").Key("tab").Wait("focus: confirm").Key("enter")
	h.Wait("Profile saved").Wait("focus: save").Sleep(800 * time.Millisecond)
	h.WaitNot("Profile saved") // the toast hides itself, which keeps the snapshot below steady

	// The second save asks again, on top.
	h.Key("enter").Wait("Save profile?").Sleep(300 * time.Millisecond)
	h.Key("enter").Wait("Overwrite the saved profile?").Wait("dialogs open: 2").Sleep(900 * time.Millisecond)
	h.AssertSnapshot(snapshot.WithSuffix("stacked"))

	// esc closes the top dialog, a click outside the next.
	h.Key("esc").WaitNot("Overwrite the saved profile?").Wait("focus: confirm").Sleep(500 * time.Millisecond)
	h.LeftClick(1, 1).WaitNot("Save profile?").Wait("focus: save").Sleep(600 * time.Millisecond)

	h.Key("ctrl+c")
	h.WaitFinished()
}
