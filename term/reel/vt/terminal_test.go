// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"context"
	"image/color"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel/vt/screen"
)

func TestWriteTextAndLinefeed(t *testing.T) {
	t.Parallel()
	term := openTest(t, 8, 3)
	if _, err := term.Write([]byte("A\nB")); err != nil {
		t.Fatal(err)
	}
	cur := term.Cursor()
	if cur.X != 1 || cur.Y != 1 {
		t.Fatalf("cursor after LF = %d,%d, want 1,1", cur.X, cur.Y)
	}
	text := term.Text()
	if !strings.HasPrefix(text, "A") {
		t.Fatalf("text = %q", text)
	}
	line, _, _ := strings.Cut(text, "\n")
	if len(line) != 8 {
		t.Fatalf("line width = %d, text %q", len(line), text)
	}
}

// TestScreenWrapped checks that the screen marks the rows a line wrapped
// past, and only those.
func TestScreenWrapped(t *testing.T) {
	t.Parallel()
	wrapped := func(scr *screen.Buffer) []bool {
		var out []bool
		for y := range scr.Height() {
			out = append(out, scr.Wrapped(y))
		}
		return out
	}
	for _, tc := range []struct {
		name   string
		writes []string
		want   []bool
	}{
		// A full row waits to wrap until the next character.
		{"full row", []string{"abcd"}, []bool{false, false, false}},
		// Its cells are unchanged when it wraps, in a second write.
		{"wrapped later", []string{"abcd", "e"}, []bool{true, false, false}},
		{"two rows", []string{"abcdefghij"}, []bool{true, true, false}},
		{"newline", []string{"abcd\r\nefgh"}, []bool{false, false, false}},
		// A wide character that does not fit leaves a spacer at the end
		// of the row it wraps from.
		{"wide", []string{"abc\u4e2d"}, []bool{true, false, false}},
		{"cleared", []string{"abcdef", "\x1b[H\x1b[2J"}, []bool{false, false, false}},
	} {
		term := openTest(t, 4, 3)
		for _, w := range tc.writes {
			if _, err := term.Write([]byte(w)); err != nil {
				t.Fatal(err)
			}
			_ = term.Screen()
		}
		scr := term.Screen()
		if got := wrapped(scr); !slices.Equal(got, tc.want) {
			t.Errorf("%s: wrapped rows %v, want %v", tc.name, got, tc.want)
		}
		if got := wrapped(scr.Clone()); !slices.Equal(got, tc.want) {
			t.Errorf("%s: clone's wrapped rows %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestOutputSeq(t *testing.T) {
	t.Parallel()
	term := openTest(t, 8, 2)
	before := term.OutputSeq()
	if _, err := term.Write(nil); err != nil {
		t.Fatal(err)
	}
	if got := term.OutputSeq(); got != before {
		t.Fatalf("empty write: seq=%d, want %d", got, before)
	}
	for i, s := range []string{"A", "B"} {
		if _, err := term.Write([]byte(s)); err != nil {
			t.Fatal(err)
		}
		if got, want := term.OutputSeq(), before+uint64(i)+1; got != want {
			t.Fatalf("after %s: seq=%d, want %d", s, got, want)
		}
	}
}

func TestEnquiryReply(t *testing.T) {
	t.Parallel()
	term := openTest(t, 8, 2)
	got := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 32)
		n, err := term.Read(buf)
		if err != nil {
			got <- []byte(err.Error())
			return
		}
		got <- append([]byte(nil), buf[:n]...)
	}()
	if _, err := term.Write([]byte{0x05}); err != nil {
		t.Fatal(err)
	}
	select {
	case buf := <-got:
		if len(buf) == 0 {
			t.Fatal("empty enquiry reply")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("enquiry produced no reply")
	}
}

func TestXtversionReply(t *testing.T) {
	t.Parallel()
	term := openTest(t, 8, 2)
	if _, err := term.Write([]byte("\x1b[>q")); err != nil {
		t.Fatal(err)
	}
	if reply, want := readReply(t, term, 2*time.Second), "\x1bP>|"+xtversion+"\x1b\\"; reply != want {
		t.Fatalf("XTVERSION reply = %q, want %q", reply, want)
	}
}

func TestLiveInstancesNeverExceedCap(t *testing.T) {
	t.Parallel()
	const capN = 2
	rt := NewRuntime(capN)
	ctx := context.Background()
	var live sync.WaitGroup
	hold := make(chan struct{})
	errCh := make(chan error, capN+1)
	var mu sync.Mutex
	open := 0
	maxOpen := 0
	for range capN {
		live.Add(1)
		go func() {
			defer live.Done()
			term, err := rt.Open(ctx, 8, 2)
			if err != nil {
				errCh <- err
				return
			}
			mu.Lock()
			open++
			if open > maxOpen {
				maxOpen = open
			}
			mu.Unlock()
			<-hold
			mu.Lock()
			open--
			mu.Unlock()
			term.Close()
		}()
	}
	waitUntil(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return open == capN
	})
	blocked := make(chan struct{})
	go func() {
		term, err := rt.Open(ctx, 8, 2)
		if err != nil {
			errCh <- err
			return
		}
		close(blocked)
		term.Close()
	}()
	select {
	case <-blocked:
		t.Fatal("open succeeded past the cap")
	case <-time.After(50 * time.Millisecond):
	}
	close(hold)
	live.Wait()
	select {
	case <-blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("blocked open did not proceed after close")
	}
	mu.Lock()
	defer mu.Unlock()
	if maxOpen > capN {
		t.Fatalf("live instances = %d, cap = %d", maxOpen, capN)
	}
	select {
	case err := <-errCh:
		t.Fatal(err)
	default:
	}
}

func TestResetOnCloseReusesInstance(t *testing.T) {
	t.Parallel()
	rt := NewRuntime(1)
	ctx := context.Background()
	first, err := rt.Open(ctx, 8, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	first.Close()
	second, err := rt.Open(ctx, 8, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if strings.Contains(second.Text(), "hello") {
		t.Fatalf("reused terminal kept previous text %q", second.Text())
	}
}

func BenchmarkWriteOnly(b *testing.B) {
	term, err := testRuntime.Open(context.Background(), 80, 24)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = term.Close() })
	payload := []byte("hello \x1b[1;32mworld\x1b[0m\n")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := term.Write(payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkScreenOnly(b *testing.B) {
	term, err := testRuntime.Open(context.Background(), 80, 24)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = term.Close() })
	payload := []byte("hello \x1b[1;32mworld\x1b[0m\n")
	if _, err := term.Write(payload); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		term.gridFresh = false
		if term.Screen() == nil {
			b.Fatal("nil screen")
		}
	}
}

func BenchmarkWriteAndText(b *testing.B) {
	term, err := testRuntime.Open(context.Background(), 80, 24)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = term.Close() })
	payload := []byte("hello \x1b[1;32mworld\x1b[0m\n")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := term.Write(payload); err != nil {
			b.Fatal(err)
		}
		_ = term.Text()
	}
}

func BenchmarkWriteAndScreen(b *testing.B) {
	term, err := testRuntime.Open(context.Background(), 80, 24)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = term.Close() })
	payload := []byte("hello \x1b[1;32mworld\x1b[0m\n")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := term.Write(payload); err != nil {
			b.Fatal(err)
		}
		if term.Screen() == nil {
			b.Fatal("nil screen")
		}
	}
}

func TestRenderHoldSkipsFrame(t *testing.T) {
	t.Parallel()
	term := openTest(t, 8, 2)
	if _, err := term.Write([]byte("A\x1b[?2026hB")); err != nil {
		t.Fatal(err)
	}
	if !term.Held() {
		t.Fatal("hold did not start")
	}
	if !strings.Contains(term.Text(), "A") || strings.Contains(term.Text(), "B") {
		t.Fatalf("held text = %q", term.Text())
	}
	if _, err := term.Write([]byte("\x1b[?2026l")); err != nil {
		t.Fatal(err)
	}
	if term.Held() {
		t.Fatal("hold did not end")
	}
	if !strings.Contains(term.Text(), "B") {
		t.Fatalf("released text = %q", term.Text())
	}
}

func TestScrollbackReenabledOnCheckout(t *testing.T) {
	t.Parallel()
	rt := NewRuntime(1)
	term, err := rt.Open(context.Background(), 4, 2, WithScrollbackLines(100))
	if err != nil {
		t.Fatal(err)
	}
	if err = term.SetScrollbackMaxLines(0); err != nil {
		t.Fatal(err)
	}
	if _, err = term.Write([]byte("a\nb\nc\nd\n")); err != nil {
		t.Fatal(err)
	}
	if term.ScrollbackLen() != 0 {
		t.Fatalf("disabled scrollback = %d", term.ScrollbackLen())
	}
	if err = term.Close(); err != nil {
		t.Fatal(err)
	}

	term, err = rt.Open(context.Background(), 4, 2, WithScrollbackLines(100))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = term.Close() })
	if _, err = term.Write([]byte("a\nb\nc\nd\n")); err != nil {
		t.Fatal(err)
	}
	if term.ScrollbackLen() == 0 {
		t.Fatal("scrollback stayed disabled after checkout")
	}
}

func TestScrollbackCap(t *testing.T) {
	t.Parallel()
	term := openTest(t, 4, 2)
	var b strings.Builder
	for range 6 {
		b.WriteString("x\n")
	}
	if _, err := term.Write([]byte(b.String())); err != nil {
		t.Fatal(err)
	}
	if term.ScrollbackLen() == 0 {
		t.Fatal("expected scrollback")
	}
	lines := term.Scrollback()
	if len(lines) != term.ScrollbackLen() {
		t.Fatalf("lines %d count %d", len(lines), term.ScrollbackLen())
	}
	if err := term.ClearScrollback(); err != nil {
		t.Fatal(err)
	}
	if term.ScrollbackLen() != 0 {
		t.Fatalf("cleared scrollback = %d", term.ScrollbackLen())
	}
}

func TestProgramStatusAndPrompt(t *testing.T) {
	t.Parallel()
	term := openTest(t, 8, 2)
	seq := "\x1b]7501;state=working:id=build:app=compiler\x1b\\" +
		"\x1b]7501;state=blocked:id=build/test\x1b\\" +
		"\x1b]133;A\x1b\\"
	if _, err := term.Write([]byte(seq)); err != nil {
		t.Fatal(err)
	}
	got := term.ProgramStatuses()
	if len(got) != 0 {
		t.Fatalf("working records survived prompt: %+v", got)
	}
	if term.Prompt().Kind != PromptStart {
		t.Fatalf("prompt = %+v", term.Prompt())
	}
}

// TestStateProgramStatuses checks that a state keeps the records it
// was taken with, that the generation changes exactly when the records do,
// and that OSC 133 A and RIS drop records.
func TestStateProgramStatuses(t *testing.T) {
	t.Parallel()
	term := openTest(t, 8, 2)
	write := func(seq string) screen.State {
		t.Helper()
		if _, err := term.Write([]byte(seq)); err != nil {
			t.Fatal(err)
		}
		return term.State()
	}
	osc := func(body string) string { return "\x1b]7501;" + body + "\x1b\\" }

	start := term.State()
	if start.ProgramStatuses != nil || start.ProgramStatusReported {
		t.Fatalf("fresh terminal: %+v reported=%v", start.ProgramStatuses, start.ProgramStatusReported)
	}

	// A clear with nothing to clear is still a report, but changes no
	// records.
	cleared := write(osc("state=clear"))
	if !cleared.ProgramStatusReported || cleared.ProgramStatusGeneration != start.ProgramStatusGeneration {
		t.Fatalf("clear: reported=%v gen %d -> %d", cleared.ProgramStatusReported, start.ProgramStatusGeneration, cleared.ProgramStatusGeneration)
	}

	working := write(osc("state=working:id=job:app=x"))
	if len(working.ProgramStatuses) != 1 || working.ProgramStatusGeneration == cleared.ProgramStatusGeneration {
		t.Fatalf("working: %+v gen %d", working.ProgramStatuses, working.ProgramStatusGeneration)
	}
	if again := write(osc("state=working:id=job:app=x")); again.ProgramStatusGeneration != working.ProgramStatusGeneration {
		t.Fatal("an identical report changed the generation")
	}

	done := write(osc("state=done:id=job"))
	if working.ProgramStatuses[0].State != screen.ProgramWorking || done.ProgramStatuses[0].State != screen.ProgramDone {
		t.Fatalf("an earlier state changed: %+v, now %+v", working.ProgramStatuses, done.ProgramStatuses)
	}

	// A new prompt drops working and blocked records only, and changes
	// nothing when there are none.
	prompt := write("\x1b]133;A\x1b\\")
	if prompt.ProgramStatusGeneration != done.ProgramStatusGeneration {
		t.Fatal("a prompt that dropped nothing changed the generation")
	}
	write(osc("state=blocked:id=ask"))
	prompt = write("\x1b]133;A\x1b\\")
	if len(prompt.ProgramStatuses) != 1 || prompt.ProgramStatuses[0].ID != "job" {
		t.Fatalf("after OSC 133 A: %+v", prompt.ProgramStatuses)
	}

	reset := write("\x1bc")
	if reset.ProgramStatuses != nil || reset.ProgramStatusReported || reset.ProgramStatusGeneration <= prompt.ProgramStatusGeneration {
		t.Fatalf("after RIS: %+v reported=%v gen %d", reset.ProgramStatuses, reset.ProgramStatusReported, reset.ProgramStatusGeneration)
	}
}

// TestProgramStatusView checks the sorted view against a direct reading of
// its rule, each record taking App from its nearest ancestor that set one,
// for trees with missing levels, and that unchanged updates return the
// records as they were.
func TestProgramStatusView(t *testing.T) {
	t.Parallel()

	// want walks up each id through the records as given.
	want := func(in []screen.ProgramStatus) []screen.ProgramStatus {
		out := slices.Clone(in)
		for i := range out {
			for id := out[i].ID; out[i].App == "" && id != ""; {
				prefix, _, found := strings.CutLast(id, "/")
				if !found {
					prefix = ""
				}
				id = prefix
				if j := slices.IndexFunc(in, func(p screen.ProgramStatus) bool { return p.ID == id }); j >= 0 {
					out[i].App = in[j].App
				}
			}
		}
		slices.SortFunc(out, func(a, b screen.ProgramStatus) int { return strings.Compare(a.ID, b.ID) })
		return out
	}
	ids := []string{"", "a", "a/b", "a/b/c", "a/b/c/d", "a/x", "a/x/y", "a-b", "a-b/c", "b/c", "b/c/d", "b", "c/d/e", "a/b/c2"}
	rng := rand.New(rand.NewPCG(1, 2))
	for range 500 {
		var in []screen.ProgramStatus
		for _, i := range rng.Perm(len(ids))[:rng.IntN(len(ids)+1)] {
			rec := screen.ProgramStatus{ID: ids[i], State: screen.ProgramWorking}
			if rng.IntN(3) == 0 {
				rec.App = "app-" + ids[i]
			}
			in = append(in, rec)
		}
		if got, want := programStatusView(in), want(in); !slices.Equal(got, want) {
			t.Fatalf("programStatusView(%+v)\n got %+v\nwant %+v", in, got, want)
		}
	}

	in := []screen.ProgramStatus{{ID: "a", State: screen.ProgramDone}, {ID: "b", State: screen.ProgramWorking}}
	same := func(a, b []screen.ProgramStatus) bool { return len(a) == len(b) && &a[0] == &b[0] }
	if !same(putProgramStatus(in, in[1]), in) {
		t.Error("putProgramStatus copied the records for an update that changes nothing")
	}
	if got := putProgramStatus(in, in[0]); same(got, in) || got[1] != in[0] {
		t.Errorf("putProgramStatus(%+v, %+v) = %+v, want it moved to the end", in, in[0], got)
	}
	if !same(deleteProgramStatus(in, "c"), in) || !same(deleteWorking(in[:1]), in[:1]) {
		t.Error("deleting nothing copied the records")
	}
	if got := deleteWorking(in); len(got) != 1 || got[0] != in[0] {
		t.Errorf("deleteWorking(%+v) = %+v", in, got)
	}
}

// BenchmarkProgramStatusView builds the sorted view of a full set of
// records three levels deep, newest first, where only one root sets an app,
// so most records look for one all the way up.
func BenchmarkProgramStatusView(b *testing.B) {
	in := make([]screen.ProgramStatus, programStatusMax)
	for i := range in {
		tree := "t" + strconv.Itoa(i%16)
		rec := screen.ProgramStatus{ID: tree, State: screen.ProgramWorking}
		switch {
		case i == 0:
			rec.App = "app"
		case i < 16:
		case i%2 == 1:
			rec = screen.ProgramStatus{ID: tree + "/c" + strconv.Itoa(i), State: screen.ProgramWorking}
		default:
			rec = screen.ProgramStatus{ID: "t" + strconv.Itoa((i-1)%16) + "/c" + strconv.Itoa(i-1) + "/g" + strconv.Itoa(i), State: screen.ProgramDone}
		}
		in[len(in)-1-i] = rec
	}
	b.ReportAllocs()
	for b.Loop() {
		programStatusView(in)
	}
}

// testRuntime is this package's pool. Tests do not use a process-wide one.
var testRuntime = NewRuntime(0)

func TestGridGenerationMatchesScreen(t *testing.T) {
	t.Parallel()
	term := openTest(t, 8, 2)
	before := term.GridGeneration()
	scr := term.Screen()
	if scr == nil {
		t.Fatal("nil screen")
	}
	if before == 0 || scr.Generation() != before {
		t.Fatalf("GridGeneration = %d, Screen generation = %d", before, scr.Generation())
	}
	if again := term.GridGeneration(); again != before {
		t.Fatalf("second GridGeneration = %d, want %d", again, before)
	}
	if _, err := term.Write([]byte("A")); err != nil {
		t.Fatal(err)
	}
	after := term.GridGeneration()
	if after == before || after == 0 {
		t.Fatalf("generation after write = %d, want a new non-zero value", after)
	}
	if got := term.Screen().Generation(); got != after {
		t.Fatalf("GridGeneration = %d, Screen generation = %d", after, got)
	}

	if _, err := term.Write([]byte("\x1b[?2026hB")); err != nil {
		t.Fatal(err)
	}
	held := term.GridGeneration()
	if held == 0 || held != term.Screen().Generation() {
		t.Fatalf("held GridGeneration = %d, Screen generation = %d", held, term.Screen().Generation())
	}
	if _, err := term.Write([]byte("C")); err != nil {
		t.Fatal(err)
	}
	if got := term.GridGeneration(); got != held {
		t.Fatalf("generation during hold = %d, want %d", got, held)
	}
}

// TestCursorColorSetByProgram checks that a cursor color the program sets
// with OSC 12 counts as set, with no host color, and that OSC 112 resets it.
func TestCursorColorSetByProgram(t *testing.T) {
	t.Parallel()
	term := openTest(t, 8, 2)
	if _, set := term.CursorColor(); set {
		t.Fatal("fresh terminal reports a cursor color")
	}
	if _, err := term.Write([]byte("\x1b]12;#ff0000\x07")); err != nil {
		t.Fatal(err)
	}
	want := color.NRGBA{R: 0xff, A: 0xff}
	if c, set := term.CursorColor(); !set || c != want {
		t.Fatalf("after OSC 12: CursorColor = %#v set=%v, want %#v set", c, set, want)
	}
	if st := term.State(); !st.CursorColorSet || st.CursorColor != want {
		t.Fatalf("after OSC 12: state cursor color = %#v set=%v", st.CursorColor, st.CursorColorSet)
	}
	if _, err := term.Write([]byte("\x1b]112\x07")); err != nil {
		t.Fatal(err)
	}
	if c, set := term.CursorColor(); set {
		t.Fatalf("after OSC 112: CursorColor = %#v, want none set", c)
	}
}

// TestGhosttyEnumMapping pins how Ghostty's progress and program status
// values map onto the screen types, including values this build does not
// know.
func TestGhosttyEnumMapping(t *testing.T) {
	t.Parallel()
	for v, want := range map[int32]screen.ProgressState{
		0: screen.ProgressRemove, 1: screen.ProgressSet, 2: screen.ProgressError,
		3: screen.ProgressIndeterminate, 4: screen.ProgressPause, 5: screen.ProgressRemove, -1: screen.ProgressRemove,
	} {
		if got := progressState(v); got != want {
			t.Errorf("progressState(%d) = %v, want %v", v, got, want)
		}
	}
	for v, want := range map[int32]screen.ProgramState{
		0: screen.ProgramIdle, 1: screen.ProgramWorking, 2: screen.ProgramDone,
		3: screen.ProgramBlocked, 4: screen.ProgramError, 6: screen.ProgramIdle, -1: screen.ProgramIdle,
	} {
		if got := programState(v); got != want {
			t.Errorf("programState(%d) = %v, want %v", v, got, want)
		}
	}
	for v, want := range map[int32]screen.ProgramKind{
		0: screen.ProgramKindNone, 1: screen.ProgramKindPermission, 2: screen.ProgramKindQuestion,
		3: screen.ProgramKindAuth, 4: screen.ProgramKindNone, -1: screen.ProgramKindNone,
	} {
		if got := programKind(v); got != want {
			t.Errorf("programKind(%d) = %v, want %v", v, got, want)
		}
	}
}

func TestStateMatchesAccessors(t *testing.T) {
	t.Parallel()
	term := openTest(t, 8, 2)
	if _, err := term.Write([]byte("A\x1b]0;snap-title\x07\x1b]9;4;1;42\x07\x1b]7501;state=working:id=b\x1b\\\x1b]7501;state=idle:app=a\x1b\\")); err != nil {
		t.Fatal(err)
	}
	fg := color.NRGBA{R: 1, G: 2, B: 3, A: 0xff}
	if err := term.SetForeground(fg); err != nil {
		t.Fatal(err)
	}
	got := term.State()
	if got.Title != "snap-title" || got.Foreground != fg {
		t.Fatalf("state title/fg = %q %#v", got.Title, got.Foreground)
	}
	if want := (screen.Progress{Reported: true, State: screen.ProgressSet, Percent: 42}); got.Progress != want {
		t.Fatalf("state progress = %+v, want %+v", got.Progress, want)
	}
	if recs := got.ProgramStatuses; len(recs) != 2 || recs[0].ID != "" || recs[1].App != "a" || !got.ProgramStatusReported {
		t.Fatalf("state program statuses = %+v reported=%v", recs, got.ProgramStatusReported)
	}
	assertState(t, term)

	if _, err := term.Write([]byte("\x1b[?2026hB")); err != nil {
		t.Fatal(err)
	}
	held := term.State()
	assertState(t, term)
	if _, err := term.Write([]byte("C")); err != nil {
		t.Fatal(err)
	}
	if got := term.State(); got.Generation != held.Generation || got.Cursor != held.Cursor {
		t.Fatalf("state during hold = %+v, want generation %d cursor %+v", got, held.Generation, held.Cursor)
	}
}

func assertState(t *testing.T, term *Terminal) {
	t.Helper()
	got := term.State()
	if got.Title != term.Title() {
		t.Fatalf("Title = %q, want %q", got.Title, term.Title())
	}
	if got.AltScreen != term.AltScreen() {
		t.Fatalf("AltScreen = %v, want %v", got.AltScreen, term.AltScreen())
	}
	if cur := term.Cursor(); got.Cursor != cur {
		t.Fatalf("Cursor = %+v, want %+v", got.Cursor, cur)
	}
	if fg := term.Foreground(); got.Foreground != fg {
		t.Fatalf("Foreground = %#v, want %#v", got.Foreground, fg)
	}
	if bg := term.Background(); got.Background != bg {
		t.Fatalf("Background = %#v, want %#v", got.Background, bg)
	}
	cc, set := term.CursorColor()
	if got.CursorColor != cc || got.CursorColorSet != set {
		t.Fatalf("CursorColor = %#v set=%v, want %#v set=%v", got.CursorColor, got.CursorColorSet, cc, set)
	}
	if n := term.ScrollbackLen(); got.ScrollbackLen != n {
		t.Fatalf("ScrollbackLen = %d, want %d", got.ScrollbackLen, n)
	}
	if p := term.Progress(); got.Progress != p {
		t.Fatalf("Progress = %+v, want %+v", got.Progress, p)
	}
	if recs := term.ProgramStatuses(); !slices.Equal(got.ProgramStatuses, recs) {
		t.Fatalf("ProgramStatuses = %+v, want %+v", got.ProgramStatuses, recs)
	}
	if gen := term.GridGeneration(); got.Generation != gen {
		t.Fatalf("Generation = %d, want %d", got.Generation, gen)
	}
	scr := term.Screen()
	if scr == nil {
		t.Fatal("nil screen")
	}
	if scr.Generation() != got.Generation {
		t.Fatalf("Screen generation = %d, state = %d", scr.Generation(), got.Generation)
	}
}

func TestScreenGenerationSurvivesCleanRefresh(t *testing.T) {
	t.Parallel()
	term := openTest(t, 8, 2)
	before := term.Screen().Generation()
	if _, err := term.Write([]byte("A")); err != nil {
		t.Fatal(err)
	}
	_ = term.Cursor()
	scr := term.Screen()
	if scr == nil || len(scr.Text()) == 0 || scr.Text()[0] != 'A' {
		t.Fatalf("text = %q", scr.Text())
	}
	for _, row := range scr.Dirty {
		if row {
			t.Fatal("Screen after Cursor refresh still had a dirty row")
		}
	}
	if scr.Generation() == before || scr.Generation() == 0 {
		t.Fatalf("generation = %d, want a new non-zero value", scr.Generation())
	}
}

func openTest(t *testing.T, cols, rows int) *Terminal {
	t.Helper()
	term, err := testRuntime.Open(context.Background(), cols, rows, WithScrollbackLines(100))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = term.Close() })
	return term
}

func waitUntil(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition was not reached")
}
