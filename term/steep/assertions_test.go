// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"context"
	"fmt"
	"iter"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/term/reel"
)

func TestAssertStringWithStripANSI(t *testing.T) {
	t.Parallel()
	const out = "plain \x1b[31mred\x1b[0m"
	if !Assert(t, func() string { return out }, "plain red", WithANSI(false)) {
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

	got := h.WaitMessageWhere(func(m appendMsg) bool {
		return m == "second"
	})
	if got != "second" {
		t.Fatalf("message = %q, want second", got)
	}

	matches := slices.Collect(h.FilterMessagesType[appendMsg]())
	seconds := slices.Collect(h.FilterMessagesFunc(func(m appendMsg) bool {
		return m == "second"
	}))
	if len(seconds) != 1 || seconds[0] != "second" {
		t.Fatalf("filtered messages = %#v, want [second]", seconds)
	}
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

func (s *softTB) Errorf(_ string, _ ...any) {
	s.nErrors++
}

func (s *softTB) Error(...any) {
	s.nErrors++
}

type fatalSentinelType struct{}

var fatalSentinel = fatalSentinelType{}

// captureFatalTB records a Fatalf string (or the error passed to Fatal) and
// panics so callers can recover and assert invalid-regexp (and similar) paths
// without aborting the real test.
type captureFatalTB struct {
	testing.TB
	msg string
	err error
}

func (c *captureFatalTB) Fatal(args ...any) {
	c.msg = fmt.Sprint(args...)
	if len(args) > 0 {
		c.err, _ = args[0].(error)
	}
	panic(fatalSentinel)
}

func (c *captureFatalTB) Fatalf(format string, args ...any) {
	c.msg = fmt.Sprintf(format, args...)
	panic(fatalSentinel)
}

type emptyMsgCollector struct{}

func (emptyMsgCollector) MessageHistory() iter.Seq[uv.Event] {
	return func(_ func(uv.Event) bool) {}
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

func TestWaitFor(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, &mutableViewModel{text: "hi"}, WithWindowSize(80, 1))
	h.WaitFor(reel.ScreenFunc(func(s string) bool { return strings.HasPrefix(s, "text=hi") }),
		WithTimeout(2*time.Second)).
		WaitFor(reel.OnCursorLine(reel.Contains("text=")), WithTimeout(2*time.Second))
}

func TestWaitString_WaitBytes(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, &mutableViewModel{text: "hi"}, WithWindowSize(80, 1))
	h.Wait("text=hi", WithTimeout(2*time.Second)).Wait([]byte("text=hi"), WithTimeout(2*time.Second))
}

func TestWait_timeout(t *testing.T) {
	t.Parallel()
	tb := &captureFatalTB{TB: t}
	h := NewComponentHarness(tb, &mutableViewModel{text: "hi"}, WithWindowSize(80, 1), WithIgnoreEnv(true))
	h.Wait("text=hi")
	defer func() {
		if _, ok := recover().(fatalSentinelType); !ok {
			t.Fatal("expected a fatal failure")
		}
		if !strings.Contains(tb.msg, `Wait("nope")`) || !strings.Contains(tb.msg, "text=hi") {
			t.Fatalf("fatalf = %q, want the reason and the last screen", tb.msg)
		}
	}()
	h.Wait("nope", WithTimeout(20*time.Millisecond))
}

func TestWaitStrings(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, &mutableViewModel{text: "ab"}, WithWindowSize(80, 1))
	h.Wait([]string{"text=", "ab"}, WithTimeout(2*time.Second))
}

func TestWaitNotString_WaitNotBytes(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, &swapViewModel{}, WithWindowSize(80, 1))
	h.SendProgram(appendMsg("flip")).
		WaitNot("BAD", WithTimeout(2*time.Second), WithCheck(5*time.Millisecond)).
		WaitNot([]byte("BAD"), WithTimeout(2*time.Second), WithCheck(5*time.Millisecond))
}

func TestWaitNotStrings(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, &swapViewModel{}, WithWindowSize(80, 1))
	h.SendProgram(appendMsg("flip")).
		WaitNot([]string{"foo", "bar"}, WithTimeout(2*time.Second), WithCheck(5*time.Millisecond))
}

func TestWaitFor_matches(t *testing.T) {
	t.Parallel()
	bad := regexp.MustCompile(`BAD`)
	h := NewComponentHarness(t, &swapViewModel{}, WithWindowSize(80, 1))
	h.WaitFor(reel.Matches(bad), WithTimeout(2*time.Second)).
		SendProgram(appendMsg("flip")).
		WaitFor(reel.NotMatches(bad), WithTimeout(2*time.Second))
}

func TestWaitSettleView(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, &viewSettleStableModel{}, WithWindowSize(80, 1))
	h.Wait("stable").WaitSettle(
		WithSettle(25*time.Millisecond),
		WithTimeout(2*time.Second),
		WithCheck(5*time.Millisecond),
	)
	if !strings.Contains(h.View(), "stable") {
		t.Fatalf("view = %q, want stable", h.View())
	}
}

func TestAssertString_fail(t *testing.T) {
	t.Parallel()
	st := &softTB{TB: t}
	if assertNo := Assert(st, func() string { return "abc" }, "nope"); assertNo {
		t.Fatal("expected false")
	}
	if st.nErrors != 1 {
		t.Fatalf("error calls = %d, want 1", st.nErrors)
	}
}

func TestAssertStrings_fail(t *testing.T) {
	t.Parallel()
	st := &softTB{TB: t}
	if Assert(st, func() string { return "only a" }, []string{"a", "missing"}) {
		t.Fatal("expected false")
	}
	if st.nErrors != 1 {
		t.Fatalf("error calls = %d, want 1", st.nErrors)
	}
}

func TestAssertNotString_fail(t *testing.T) {
	t.Parallel()
	st := &softTB{TB: t}
	if AssertNot(st, func() string { return "has needle" }, "needle") {
		t.Fatal("expected false")
	}
	if st.nErrors != 1 {
		t.Fatalf("error calls = %d, want 1", st.nErrors)
	}
}

func TestAssertNotStrings_fail(t *testing.T) {
	t.Parallel()
	st := &softTB{TB: t}
	if AssertNot(st, func() string { return "x bad y" }, []string{"zzz", "bad"}) {
		t.Fatal("expected false")
	}
	if st.nErrors != 1 {
		t.Fatalf("error calls = %d, want 1", st.nErrors)
	}
}

func TestAssertFor_fail(t *testing.T) {
	t.Parallel()
	for _, cond := range []reel.Condition{
		reel.Matches(regexp.MustCompile(`^\d+$`)),
		reel.NotMatches(regexp.MustCompile(`\d`)),
		reel.OnCursorLine(reel.Contains("abc")),
	} {
		st := &softTB{TB: t}
		if AssertFor(st, func() string { return "abc123" }, cond) {
			t.Fatalf("%s held", cond)
		}
		if st.nErrors != 1 {
			t.Fatalf("%s: error calls = %d, want 1", cond, st.nErrors)
		}
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
	Require(t, v, "beta")
	Require(t, v, []string{"alpha", "beta"})
	RequireNot(t, v, "gamma")
	RequireNot(t, v, []string{"x", "y"})
	if !AssertNot(t, v, "gamma") || !AssertNot(t, v, []string{"nope"}) {
		t.Fatal("negative asserts should pass")
	}
	RequireFor(t, v, reel.Matches(regexp.MustCompile(`b.t`)))
	RequireFor(t, v, reel.NotMatches(regexp.MustCompile(`^\d`)))
	if !AssertFor(t, v, reel.Matches(regexp.MustCompile(`alpha`))) {
		t.Fatal("condition asserts should pass")
	}
}

func TestAssertHasMessage_found_andMissing(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, &mutableViewModel{}, WithWindowSize(80, 1))
	h.SendProgram(appendMsg("z"))
	if got := h.WaitMessage[appendMsg](); got != "z" {
		t.Fatalf("message = %q, want z", got)
	}
	if !h.AssertHasMessage[appendMsg]() {
		t.Fatal("expected appendMsg in history")
	}
	h.RequireHasMessage[appendMsg]()

	st := &softTB{TB: t}
	if AssertHasMessage[appendMsg](st, emptyMsgCollector{}) {
		t.Fatal("expected false")
	}
	if st.nErrors != 1 {
		t.Fatalf("error calls = %d, want 1", st.nErrors)
	}
}

func TestWaitNewMessageWhere(t *testing.T) {
	t.Parallel()
	h := NewComponentHarness(t, &delayedPingModel{}, WithWindowSize(80, 1))
	got := h.WaitLiveMessageWhere(func(m appendMsg) bool {
		return m == "ping"
	}, WithTimeout(2*time.Second))
	if got != "ping" {
		t.Fatalf("got %#v", got)
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
	empty := slices.Collect(FilterMessagesFunc(t, slices.Values(msgs), func(_ appendMsg) bool {
		return false
	}))
	if len(empty) != 0 {
		t.Fatalf("want empty, got %#v", empty)
	}
}
