// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/lrstanley/x/term/bloom/_examples/internal/demo"
	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/steep"
	"github.com/lrstanley/x/term/steep/snapshot"
)

// column returns the column text starts at in the screen's first line
// containing it, or -1.
func column(view, text string) int {
	for line := range strings.SplitSeq(view, "\n") {
		if i := strings.Index(line, text); i >= 0 {
			return i
		}
	}
	return -1
}

// seen polls the screen for d, reporting everything f returns for it.
func seen[T comparable](h *steep.Harness, d time.Duration, f func(view string) T) map[T]bool {
	out := map[T]bool{}
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(8 * time.Millisecond) {
		out[f(h.View())] = true
	}
	return out
}

func TestLaunch(t *testing.T) {
	t.Parallel()

	m := newModel()
	// The numbers change when the test sends the update, so that the
	// snapshots do not depend on timing.
	m.every = time.Hour
	h := steep.NewHarness(t, m, demo.Options("launch", 100, 28)...)

	// Cards shimmer as placeholders until the data arrives.
	h.Wait("deploy").Wait("████").WaitNot("Requests").Sleep(600 * time.Millisecond)
	h.Wait("Requests").Wait("1,021").Wait("updates: 0").Sleep(600 * time.Millisecond)
	h.AssertSnapshot(snapshot.WithSuffix("loaded"))

	// An update rolls the numbers to their new values. Pausing ignores the
	// next one.
	h.SendProgram(statsMsg(1)).Wait("updates: 1").Wait("1,069").Sleep(900 * time.Millisecond)
	h.Key("ctrl+p").Wait("(paused)").SendProgram(statsMsg(2)).Sleep(300 * time.Millisecond)
	if v := h.View(); !strings.Contains(v, "updates: 1 (paused)") {
		t.Fatalf("a paused update went through:\n%s", v)
	}
	h.Key("ctrl+p").WaitNot("(paused)")

	// The tour spotlights each card in turn and decodes what it says.
	h.Key("ctrl+t").Wait("Traffic").Wait("tour: 1").Sleep(1200 * time.Millisecond)
	h.Key("ctrl+t").Wait("Latency").Wait("tour: 2").Sleep(1200 * time.Millisecond)
	h.AssertSnapshot(snapshot.WithSuffix("tour"))
	h.Key("ctrl+t").Wait("Failed requests").Sleep(1200 * time.Millisecond)
	h.Key("ctrl+t").WaitNot("Failed requests").Wait("tour: 0").Sleep(300 * time.Millisecond)

	// A wrong confirmation shakes the input.
	h.Type("paymets", reel.Delay(30*time.Millisecond)).Wait("paymets")
	h.Key("enter")
	cols := seen(h, 600*time.Millisecond, func(v string) int { return column(v, "paymets") })
	if len(cols) < 2 {
		t.Fatalf("the input did not move while shaking: columns %v", cols)
	}
	h.Wait(`type "payments" to confirm`).Sleep(300 * time.Millisecond)

	// The right one deploys, with confetti that settles and a LIVE badge.
	for range len("paymets") {
		h.Key("backspace")
	}
	h.Type("payments", reel.Delay(30*time.Millisecond)).Key("enter")
	confetti := seen(h, 1200*time.Millisecond, func(v string) bool { return strings.ContainsAny(v, "✦•") })
	if !confetti[true] {
		t.Fatal("no confetti was drawn")
	}
	h.Wait("payments is live").Wait("LIVE").WaitNot("✦").WaitNot("•").Sleep(1200 * time.Millisecond)
	h.AssertSnapshot(snapshot.WithSuffix("live"))

	h.Key("ctrl+c")
	h.WaitFinished()
}
