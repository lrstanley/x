// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
	"testing"
	"time"
)

// testTimeline returns a timeline holding events at the given offsets.
func testTimeline(t *testing.T, events ...Event) *Timeline {
	t.Helper()
	tl := newTimeline(Header{Cols: 80, Rows: 24})
	for _, ev := range events {
		tl.appendAt(ev, ev.At())
	}
	return tl
}

func at(ms int) event { return event{at: time.Duration(ms) * time.Millisecond} }

// describe returns a short form of each event, for comparing in tests.
func describe(events []Event) []string {
	out := make([]string, len(events))
	for i, ev := range events {
		ms := ev.At().Milliseconds()
		switch ev := ev.(type) {
		case Output:
			out[i] = fmt.Sprintf("%d output %q", ms, ev.Data)
		case Input:
			out[i] = fmt.Sprintf("%d input %q", ms, ev.Data)
		case Resize:
			out[i] = fmt.Sprintf("%d resize %dx%d", ms, ev.Cols, ev.Rows)
		case Mark:
			out[i] = fmt.Sprintf("%d mark %s", ms, ev.Label)
		case Focus:
			out[i] = fmt.Sprintf("%d focus %t", ms, ev.On)
		case Scroll:
			out[i] = fmt.Sprintf("%d scroll %d", ms, ev.Offset)
		case Theme:
			out[i] = fmt.Sprintf("%d theme %v %v %v", ms, ev.Fg, ev.Bg, ev.Cursor)
		case Exit:
			out[i] = fmt.Sprintf("%d exit %d", ms, ev.Status)
		case Annotation:
			out[i] = fmt.Sprintf("%d annotation %s %s", ms, ev.Kind, string(ev.Data))
		}
	}
	return out
}

func assertEvents(t *testing.T, tl *Timeline, want ...string) {
	t.Helper()
	got := describe(slices.Collect(tl.Events()))
	if !slices.Equal(got, want) {
		t.Fatalf("events:\n got %q\nwant %q", got, want)
	}
	if tl.Len() != len(want) {
		t.Fatalf("Len = %d, want %d", tl.Len(), len(want))
	}
}

func TestTimelineOrdering(t *testing.T) {
	t.Parallel()
	tl := testTimeline(t,
		Output{event: at(0), Data: []byte("a")},
		Input{event: at(5), Data: []byte("b")},
		// An offset before the last one is raised to it.
		Mark{event: at(3), Label: "m"},
		Resize{event: at(10), Cols: 10, Rows: 4},
		Focus{event: at(10), On: false},
		Exit{event: at(12), Status: 2},
	)
	assertEvents(t, tl,
		`0 output "a"`,
		`5 input "b"`,
		`5 mark m`,
		`10 resize 10x4`,
		`10 focus false`,
		`12 exit 2`,
	)
	if got := tl.Duration(); got != 12*time.Millisecond {
		t.Fatalf("Duration = %v, want 12ms", got)
	}
	if from, to := tl.Window(); from != 0 || to != 12*time.Millisecond {
		t.Fatalf("Window = %v, %v, want 0, 12ms", from, to)
	}
}

func TestTimelineNegativeOffset(t *testing.T) {
	t.Parallel()
	tl := testTimeline(t, Output{event: at(-5)})
	if got := tl.Duration(); got != 0 {
		t.Fatalf("first event at %v, want 0", got)
	}
}

func TestTimelineEmpty(t *testing.T) {
	t.Parallel()
	tl := testTimeline(t)
	if tl.Len() != 0 || tl.Duration() != 0 {
		t.Fatalf("empty timeline Len = %d, Duration = %v", tl.Len(), tl.Duration())
	}
	part, err := tl.Between("", "")
	if err != nil {
		t.Fatal(err)
	}
	if part.Len() != 0 {
		t.Fatalf("Between of empty timeline has %d events", part.Len())
	}
	if _, err = tl.Between("a", ""); !errors.Is(err, ErrNoMark) {
		t.Fatalf("Between(a) on empty timeline = %v, want ErrNoMark", err)
	}
}

func TestTimelineAppendClock(t *testing.T) {
	t.Parallel()
	tl := testTimeline(t)
	tl.append(Output{Data: []byte("a")})
	time.Sleep(2 * time.Millisecond)
	tl.append(Output{Data: []byte("b")})
	events := slices.Collect(tl.Events())
	if events[0].At() != 0 {
		t.Fatalf("first event at %v, want 0", events[0].At())
	}
	if events[1].At() < 2*time.Millisecond {
		t.Fatalf("second event at %v, want at least 2ms", events[1].At())
	}
}

func TestTimelineRoundsToMillisecond(t *testing.T) {
	t.Parallel()
	tl := testTimeline(t)
	for _, at := range []time.Duration{
		// -400µs rounds to zero, and -2ms is raised to it.
		-400 * time.Microsecond, -2 * ms,
		1499 * time.Microsecond, 1500 * time.Microsecond,
		// Later in the same millisecond, and before the last event's
		// offset once it is rounded up.
		2600 * time.Microsecond, 2400 * time.Microsecond, 2999 * time.Microsecond,
		10*ms + 1,
	} {
		tl.appendAt(Output{Data: []byte("x")}, at)
	}
	var got []time.Duration
	for ev := range tl.Events() {
		got = append(got, ev.At())
	}
	want := []time.Duration{0, 0, 1 * ms, 2 * ms, 3 * ms, 3 * ms, 3 * ms, 10 * ms}
	if !slices.Equal(got, want) {
		t.Fatalf("offsets = %v, want %v", got, want)
	}

	// The wall clock is rounded too.
	tl = testTimeline(t)
	for range 20 {
		tl.append(Output{Data: []byte("x")})
		time.Sleep(137 * time.Microsecond)
	}
	for ev := range tl.Events() {
		if ev.At()%ms != 0 {
			t.Fatalf("wall clock offset %v is not whole milliseconds", ev.At())
		}
	}
}

func TestTimelineFilters(t *testing.T) {
	t.Parallel()
	tl := testTimeline(t,
		Output{event: at(0), Data: []byte("o")},
		Input{event: at(1), Data: []byte("1")},
		Mark{event: at(2), Label: "x"},
		Input{event: at(3), Data: []byte("2")},
		Mark{event: at(4), Label: "y"},
	)
	var inputs []string
	for in := range tl.Inputs() {
		inputs = append(inputs, fmt.Sprintf("%d:%s", in.At().Milliseconds(), in.Data))
	}
	if want := []string{"1:1", "3:2"}; !slices.Equal(inputs, want) {
		t.Fatalf("Inputs = %q, want %q", inputs, want)
	}
	var marks []string
	for m := range tl.Marks() {
		marks = append(marks, m.Label)
	}
	if want := []string{"x", "y"}; !slices.Equal(marks, want) {
		t.Fatalf("Marks = %q, want %q", marks, want)
	}
	// Stopping early stops the iterator.
	for range tl.Inputs() {
		break
	}
}

func TestTimelineIteratorIsSnapshot(t *testing.T) {
	t.Parallel()
	tl := testTimeline(t, Output{event: at(0), Data: []byte("a")})
	events := tl.Events()
	tl.appendAt(Output{Data: []byte("b")}, time.Millisecond)
	if n := len(slices.Collect(events)); n != 1 {
		t.Fatalf("iterator saw %d events, want the 1 appended before the call", n)
	}
	if tl.Len() != 2 {
		t.Fatalf("Len = %d, want 2", tl.Len())
	}
}

func TestTimelineEventsFrom(t *testing.T) {
	t.Parallel()
	tl := testTimeline(t, Output{event: at(0), Data: []byte("a")}, Output{event: at(1), Data: []byte("b")})
	for _, tc := range []struct {
		from int
		want int
	}{{-1, 2}, {0, 2}, {1, 1}, {2, 0}, {5, 0}} {
		if n := len(slices.Collect(tl.EventsFrom(tc.from))); n != tc.want {
			t.Errorf("EventsFrom(%d) has %d events, want %d", tc.from, n, tc.want)
		}
	}
	// A reader that has read Len events reads the ones appended since.
	read := tl.Len()
	tl.appendAt(Output{Data: []byte("c")}, 2*time.Millisecond)
	got := slices.Collect(tl.EventsFrom(read))
	if len(got) != 1 {
		t.Fatalf("EventsFrom(%d) after an append has %d events, want 1", read, len(got))
	}
	if out, ok := got[0].(Output); !ok || string(out.Data) != "c" {
		t.Fatalf("EventsFrom(%d) after an append = %v, want the event appended", read, got)
	}
}

func TestTimelineBetween(t *testing.T) {
	t.Parallel()
	tl := testTimeline(t,
		Output{event: at(0), Data: []byte("setup")},
		Mark{event: at(10), Label: "start"},
		Input{event: at(15), Data: []byte("k")},
		Output{event: at(20), Data: []byte("frame")},
		Mark{event: at(30), Label: "end"},
		Output{event: at(40), Data: []byte("after")},
		Mark{event: at(50), Label: "end"},
	)

	part, err := tl.Between("start", "end")
	if err != nil {
		t.Fatal(err)
	}
	// Events before the window are kept so replay starts in the right state;
	// events after its end are dropped.
	assertEvents(t, part,
		`0 output "setup"`,
		`10 mark start`,
		`15 input "k"`,
		`20 output "frame"`,
		`30 mark end`,
	)
	if from, to := part.Window(); from != 10*time.Millisecond || to != 30*time.Millisecond {
		t.Fatalf("Window = %v, %v, want 10ms, 30ms", from, to)
	}
	if part.Header() != tl.Header() {
		t.Fatalf("Header = %+v, want %+v", part.Header(), tl.Header())
	}

	tests := []struct {
		name     string
		from, to string
		window   [2]int
		len      int
	}{
		{"start only", "start", "", [2]int{10, 50}, 7},
		{"end only", "", "end", [2]int{0, 30}, 5},
		{"whole", "", "", [2]int{0, 50}, 7},
		{"same label", "end", "end", [2]int{30, 50}, 7},
	}
	for _, tt := range tests {
		got, berr := tl.Between(tt.from, tt.to)
		if berr != nil {
			t.Fatalf("%s: %v", tt.name, berr)
		}
		from, to := got.Window()
		if from.Milliseconds() != int64(tt.window[0]) || to.Milliseconds() != int64(tt.window[1]) || got.Len() != tt.len {
			t.Fatalf("%s: Window = %v, %v with %d events, want %dms, %dms with %d",
				tt.name, from, to, got.Len(), tt.window[0], tt.window[1], tt.len)
		}
	}

	for _, labels := range [][2]string{{"missing", ""}, {"", "missing"}, {"end", "start"}} {
		if _, err = tl.Between(labels[0], labels[1]); !errors.Is(err, ErrNoMark) {
			t.Fatalf("Between(%q, %q) = %v, want ErrNoMark", labels[0], labels[1], err)
		}
	}
}

func TestTimelineBetweenNarrows(t *testing.T) {
	t.Parallel()
	tl := testTimeline(t,
		Mark{event: at(0), Label: "a"},
		Mark{event: at(10), Label: "b"},
		Mark{event: at(20), Label: "c"},
		Mark{event: at(30), Label: "d"},
	)
	outer, err := tl.Between("b", "d")
	if err != nil {
		t.Fatal(err)
	}
	inner, err := outer.Between("", "c")
	if err != nil {
		t.Fatal(err)
	}
	if from, to := inner.Window(); from != 10*time.Millisecond || to != 20*time.Millisecond {
		t.Fatalf("inner Window = %v, %v, want 10ms, 20ms", from, to)
	}
	// "a" is before the outer window, so the inner one cannot start there.
	if _, err = outer.Between("a", ""); !errors.Is(err, ErrNoMark) {
		t.Fatalf("Between(a) inside b..d = %v, want ErrNoMark", err)
	}
	// The outer window's own start mark is inside it.
	same, err := outer.Between("b", "")
	if err != nil {
		t.Fatal(err)
	}
	if from, to := same.Window(); from != 10*time.Millisecond || to != 30*time.Millisecond {
		t.Fatalf("Between(b) inside b..d Window = %v, %v, want 10ms, 30ms", from, to)
	}
}

func TestTimelineBetweenIsFixed(t *testing.T) {
	t.Parallel()
	tl := testTimeline(t, Mark{event: at(0), Label: "a"})
	part, err := tl.Between("a", "")
	if err != nil {
		t.Fatal(err)
	}
	tl.appendAt(Output{Data: []byte("later")}, time.Millisecond)
	if part.Len() != 1 {
		t.Fatalf("Between result grew to %d events", part.Len())
	}
}

func TestTimelineConcurrentReaders(t *testing.T) {
	t.Parallel()
	tl := testTimeline(t)
	const writes = 500
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range writes {
			tl.append(Output{Data: []byte{byte(i)}})
			if i%50 == 0 {
				tl.append(Mark{Label: "m"})
			}
		}
	})
	for range 4 {
		wg.Go(func() {
			for tl.Len() < writes {
				var last time.Duration
				n := 0
				for ev := range tl.Events() {
					if ev.At() < last {
						t.Errorf("offset %v after %v", ev.At(), last)
						return
					}
					last = ev.At()
					n++
				}
				if n > tl.Len() {
					t.Errorf("iterated %d events, more than Len", n)
					return
				}
				_, _ = tl.Between("m", "")
				if _, err := tl.WriteTo(io.Discard); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	wg.Wait()
}
