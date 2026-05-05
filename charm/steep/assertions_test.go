// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"bytes"
	"context"
	"fmt"
	"iter"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

func TestAssertStringWithStripANSI(t *testing.T) {
	t.Parallel()
	const out = "plain \x1b[31mred\x1b[0m"
	if !AssertString(t, func() string { return out }, "plain red", WithStripANSI()) {
		t.Fatal("expected strip-then-contains to match")
	}
}

type otherMsg struct{}

func TestMessagesOfType(t *testing.T) {
	t.Parallel()
	messages := []uv.Event{
		appendMsg("a"),
		otherMsg{},
		appendMsg("b"),
	}

	got := slices.Collect(FilterMessagesType[appendMsg](t, slices.Values(messages)))
	if len(got) != 2 {
		t.Fatalf("messages = %d, want 2", len(got))
	}
	if got[0] != "a" || got[1] != "b" {
		t.Fatalf("messages = %#v, want [a b]", got)
	}
}

func TestWaitMessageWhere(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, &mutableViewModel{}, WithWindowSize(80, 1))
	h.SendProgram(appendMsg("first")).SendProgram(appendMsg("second"))

	got := WaitMessageWhere(t, h, func(msg uv.Event) bool {
		m, ok := msg.(appendMsg)
		return ok && m == "second"
	})
	m, ok := got.(appendMsg)
	if !ok || m != "second" {
		t.Fatalf("message = %q, want second", got)
	}

	matches := slices.Collect(FilterMessagesType[appendMsg](t, h.MessageHistory()))
	if len(matches) != 2 {
		t.Fatalf("messages = %d, want 2", len(matches))
	}
}

// softTB suppresses Errorf so Assert* failure paths can be tested without
// failing the outer test.
type softTB struct {
	testing.TB
	nErrors int
}

func (s *softTB) Errorf(format string, args ...any) {
	s.nErrors++
}

type fatalSentinelType struct{}

var fatalSentinel = fatalSentinelType{}

// captureFatalTB records a Fatalf string and panics so callers can recover and
// assert invalid-regexp (and similar) paths without aborting the real test.
type captureFatalTB struct {
	testing.TB
	msg string
}

func (c *captureFatalTB) Fatalf(format string, args ...any) {
	c.msg = fmt.Sprintf(format, args...)
	panic(fatalSentinel)
}

type emptyMsgCollector struct{}

func (emptyMsgCollector) MessageHistory() iter.Seq[uv.Event] {
	return func(yield func(uv.Event) bool) {}
}

func (emptyMsgCollector) Messages(_ context.Context) iter.Seq[uv.Event] {
	return emptyMsgCollector{}.MessageHistory()
}

func (emptyMsgCollector) LiveMessages(_ context.Context) iter.Seq[uv.Event] {
	return emptyMsgCollector{}.MessageHistory()
}

type swapViewModel struct {
	clean bool
}

func (m *swapViewModel) View() string {
	if !m.clean {
		return "foo BAD bar"
	}
	return "all fine"
}

func (m *swapViewModel) Update(msg uv.Event) tea.Cmd {
	if _, ok := msg.(appendMsg); ok {
		m.clean = true
	}
	return nil
}

type delayedPingModel struct{}

func (m *delayedPingModel) View() string { return "ready" }

func (m *delayedPingModel) Init() tea.Cmd {
	return tea.Tick(15*time.Millisecond, func(time.Time) uv.Event {
		return appendMsg("ping")
	})
}

func (m *delayedPingModel) Update(uv.Event) tea.Cmd { return nil }

func TestDimensions(t *testing.T) {
	t.Parallel()
	w, h := Dimensions("")
	if w != 0 || h != 0 {
		t.Fatalf("empty = %dx%d, want 0x0", w, h)
	}
	w, h = Dimensions("a\nbb\nccc")
	if w != 3 || h != 3 {
		t.Fatalf("got %dx%d, want 3x3", w, h)
	}
}

func TestDimensions_withStripANSI(t *testing.T) {
	t.Parallel()
	w, h := Dimensions("plain \x1b[31mred\x1b[0m", WithStripANSI())
	if w != 9 || h != 1 {
		t.Fatalf("got %dx%d, want 9x1 for stripped %q", w, h, "plain red")
	}
}

func TestWaitViewFunc(t *testing.T) {
	t.Parallel()
	got := WaitViewFunc(t, func() string { return "hello" }, func(s string) bool {
		return strings.HasPrefix(s, "hel")
	}, WithTimeout(100*time.Millisecond))
	if got != "hello" {
		t.Fatalf("view = %q", got)
	}
}

func TestWaitString_WaitBytes_package(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, &mutableViewModel{text: "hi"}, WithWindowSize(80, 1))
	WaitString(t, h.View, "text=hi", WithTimeout(2*time.Second))
	out := WaitBytes(t, h.View, []byte("text=hi"), WithTimeout(2*time.Second))
	if !bytes.Contains(out, []byte("text=hi")) {
		t.Fatalf("bytes view = %q", out)
	}
}

func TestWaitString_withStripANSI(t *testing.T) {
	t.Parallel()
	out := WaitString(t, func() string { return "\x1b[31mred\x1b[0m" }, "red", WithStripANSI(), WithTimeout(50*time.Millisecond))
	if strings.TrimSpace(out) != "red" {
		t.Fatalf("output = %q", out)
	}
}

func TestWaitStrings(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, &mutableViewModel{text: "ab"}, WithWindowSize(80, 1))
	WaitStrings(t, h.View, []string{"text=", "ab"}, WithTimeout(2*time.Second))
}

func TestWaitNotString_WaitNotBytes(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, &swapViewModel{}, WithWindowSize(80, 1))
	h.SendProgram(appendMsg("flip")).
		WaitNotString("BAD", WithTimeout(2*time.Second), WithCheckInterval(5*time.Millisecond)).
		WaitNotBytes([]byte("BAD"), WithTimeout(2*time.Second), WithCheckInterval(5*time.Millisecond))
}

func TestWaitNotStrings(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, &swapViewModel{}, WithWindowSize(80, 1))
	h.SendProgram(appendMsg("flip")).
		WaitNotStrings([]string{"foo", "bar"}, WithTimeout(2*time.Second), WithCheckInterval(5*time.Millisecond))
}

func TestWaitMatch_WaitNotMatch_package(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, &swapViewModel{}, WithWindowSize(80, 1))
	h.WaitMatch(`BAD`, WithTimeout(2*time.Second), WithCheckInterval(5*time.Millisecond)).
		SendProgram(appendMsg("flip")).
		WaitNotMatch(`BAD`, WithTimeout(2*time.Second), WithCheckInterval(5*time.Millisecond))
}

func TestWaitMatch_invalidRegexp(t *testing.T) {
	t.Parallel()
	tb := &captureFatalTB{TB: t}
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic from Fatalf")
		}
		if _, ok := r.(fatalSentinelType); !ok {
			panic(r)
		}
		if !strings.Contains(tb.msg, "invalid regexp") {
			t.Fatalf("fatalf = %q", tb.msg)
		}
	}()
	WaitMatch(tb, func() string { return "x" }, "[")
}

func TestWaitSettleView_package(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, &viewSettleStableModel{}, WithWindowSize(80, 1))
	WaitString(t, h.View, "stable")
	WaitSettle(t, h.View,
		WithSettleTimeout(25*time.Millisecond),
		WithTimeout(2*time.Second),
		WithCheckInterval(5*time.Millisecond),
	)
	if !strings.Contains(h.View(), "stable") {
		t.Fatalf("view = %q, want stable", h.View())
	}
}

func TestAssertString_fail(t *testing.T) {
	t.Parallel()
	st := &softTB{TB: t}
	if AssertNo := AssertString(st, func() string { return "abc" }, "nope"); AssertNo {
		t.Fatal("expected false")
	}
	if st.nErrors != 1 {
		t.Fatalf("error calls = %d, want 1", st.nErrors)
	}
}

func TestAssertStrings_fail(t *testing.T) {
	t.Parallel()
	st := &softTB{TB: t}
	if AssertStrings(st, func() string { return "only a" }, []string{"a", "missing"}) {
		t.Fatal("expected false")
	}
	if st.nErrors != 1 {
		t.Fatalf("error calls = %d, want 1", st.nErrors)
	}
}

func TestAssertNotString_fail(t *testing.T) {
	t.Parallel()
	st := &softTB{TB: t}
	if AssertNotString(st, func() string { return "has needle" }, "needle") {
		t.Fatal("expected false")
	}
	if st.nErrors != 1 {
		t.Fatalf("error calls = %d, want 1", st.nErrors)
	}
}

func TestAssertNotStrings_fail(t *testing.T) {
	t.Parallel()
	st := &softTB{TB: t}
	if AssertNotStrings(st, func() string { return "x bad y" }, []string{"zzz", "bad"}) {
		t.Fatal("expected false")
	}
	if st.nErrors != 1 {
		t.Fatalf("error calls = %d, want 1", st.nErrors)
	}
}

func TestAssertMatch_fail(t *testing.T) {
	t.Parallel()
	st := &softTB{TB: t}
	if AssertMatch(st, func() string { return "abc" }, `^\d+$`) {
		t.Fatal("expected false")
	}
	if st.nErrors != 1 {
		t.Fatalf("error calls = %d, want 1", st.nErrors)
	}
}

func TestAssertNotMatch_fail(t *testing.T) {
	t.Parallel()
	st := &softTB{TB: t}
	if AssertNotMatch(st, func() string { return "abc123" }, `\d`) {
		t.Fatal("expected false")
	}
	if st.nErrors != 1 {
		t.Fatalf("error calls = %d, want 1", st.nErrors)
	}
}

func TestAssertHeightWidthDimensions(t *testing.T) {
	t.Parallel()
	v := func() string { return "ab\nxy" }
	if !AssertWidth(t, v, 2) || !AssertHeight(t, v, 2) || !AssertDimensions(t, v, 2, 2) {
		t.Fatal("layout assertions should pass")
	}
	RequireWidth(t, v, 2)
	RequireHeight(t, v, 2)
	RequireDimensions(t, v, 2, 2)
}

func TestAssertHeight_fail(t *testing.T) {
	t.Parallel()
	st := &softTB{TB: t}
	if AssertHeight(st, func() string { return "one\ntwo" }, 99) {
		t.Fatal("expected false")
	}
	if st.nErrors != 1 {
		t.Fatalf("error calls = %d, want 1", st.nErrors)
	}
}

func TestPackageRequireString_andAssertHelpers_ok(t *testing.T) {
	t.Parallel()
	v := func() string { return "alpha beta" }
	RequireString(t, v, "beta")
	RequireStrings(t, v, []string{"alpha", "beta"})
	RequireNotString(t, v, "gamma")
	RequireNotStrings(t, v, []string{"x", "y"})
	if !AssertNotString(t, v, "gamma") || !AssertNotStrings(t, v, []string{"nope"}) {
		t.Fatal("negative asserts should pass")
	}
	RequireMatch(t, v, `b.t`)
	RequireNotMatch(t, v, `^\d`)
	if !AssertMatch(t, v, `alpha`) || !AssertNotMatch(t, v, `^\d+$`) {
		t.Fatal("match asserts should pass")
	}
}

func TestAssertHasMessage_found_andMissing(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, &mutableViewModel{}, WithWindowSize(80, 1))
	h.SendProgram(appendMsg("z"))
	if got := WaitMessage[appendMsg](t, h); got != "z" {
		t.Fatalf("message = %q, want z", got)
	}
	if !AssertHasMessage[appendMsg](t, h) {
		t.Fatal("expected appendMsg in history")
	}
	RequireHasMessage[appendMsg](t, h)

	st := &softTB{TB: t}
	if AssertHasMessage[appendMsg](st, emptyMsgCollector{}) {
		t.Fatal("expected false")
	}
	if st.nErrors != 1 {
		t.Fatalf("error calls = %d, want 1", st.nErrors)
	}
}

func TestWaitMessage_generic(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, &mutableViewModel{}, WithWindowSize(80, 1))
	h.SendProgram(appendMsg("solo"))
	if got := WaitMessage[appendMsg](t, h); got != "solo" {
		t.Fatalf("message = %q", got)
	}
}

func TestWaitNewMessageWhere(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, &delayedPingModel{}, WithWindowSize(80, 1))
	got := WaitLiveMessageWhere(t, h, func(msg uv.Event) bool {
		m, ok := msg.(appendMsg)
		return ok && m == "ping"
	}, WithTimeout(2*time.Second))
	m, ok := got.(appendMsg)
	if !ok || m != "ping" {
		t.Fatalf("got %#v", got)
	}
}

func TestWaitNewMessage_generic(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, &delayedPingModel{}, WithWindowSize(80, 1))
	if got := WaitLiveMessage[appendMsg](t, h); got != "ping" {
		t.Fatalf("message = %q", got)
	}
}

func TestIgnoreMessagesReflect(t *testing.T) {
	t.Parallel()
	msgs := []uv.Event{appendMsg("a"), otherMsg{}, appendMsg("b")}
	got := slices.Collect(IgnoreMessagesReflect(t, slices.Values(msgs), reflect.TypeOf(otherMsg{})))
	if len(got) != 2 || got[0] != appendMsg("a") || got[1] != appendMsg("b") {
		t.Fatalf("got %#v", got)
	}
	all := slices.Collect(IgnoreMessagesReflect(t, slices.Values(msgs)))
	if len(all) != 3 {
		t.Fatalf("ignore none: len = %d", len(all))
	}
}

func TestFilterMessagesFunc(t *testing.T) {
	t.Parallel()
	msgs := []uv.Event{appendMsg("a"), appendMsg("b"), otherMsg{}}
	got := slices.Collect(FilterMessagesFunc(t, slices.Values(msgs), func(m appendMsg) bool {
		return m == "b"
	}))
	if len(got) != 1 || got[0] != "b" {
		t.Fatalf("got %#v, want [b]", got)
	}
	empty := slices.Collect(FilterMessagesFunc(t, slices.Values(msgs), func(m appendMsg) bool {
		return false
	}))
	if len(empty) != 0 {
		t.Fatalf("want empty, got %#v", empty)
	}
}
