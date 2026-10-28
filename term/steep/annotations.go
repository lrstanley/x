// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"runtime/debug"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/vt"
)

// The kinds of the [reel.Annotation] events a harness records in its
// timeline. reel's annotation.schema.json describes each.
const (
	kindStart     = "steep.start"
	kindReady     = "steep.ready"
	kindInput     = "steep.input"
	kindTerminal  = "steep.terminal"
	kindProgram   = "steep.program"
	kindWaitStart = "steep.wait.start"
	kindWaitEnd   = "steep.wait.end"
	kindAssert    = "steep.assert"
	kindSnapshot  = "steep.snapshot"
	kindFailure   = "steep.failure"
	kindMessage   = "steep.message"
	kindExit      = "steep.exit"
	kindResult    = "steep.result"
)

// noteTextMax is the most bytes of text an annotation field holds. Longer
// text is cut, and the annotation marked truncated.
const noteTextMax = 4 << 10

// clip returns s as valid UTF-8, cut to noteTextMax bytes, and sets
// *truncated when it was cut.
func clip(s string, truncated *bool) string {
	s = strings.ToValidUTF8(s, "�")
	if len(s) <= noteTextMax {
		return s
	}
	*truncated = true
	n := noteTextMax
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// clipAll is clip for each of texts, in a new slice, or nil for none.
func clipAll(texts []string, truncated *bool) []string {
	if len(texts) == 0 {
		return nil
	}
	out := make([]string, len(texts))
	for i, s := range texts {
		out[i] = clip(s, truncated)
	}
	return out
}

// annotate records an annotation of kind, with the fields of v, in the
// session's timeline, failing the test when it cannot. Once the session is
// closed, it does nothing.
func (h *Harness) annotate(kind string, v any) {
	h.tb.Helper()
	if err := h.record(kind, v); err != nil {
		h.tb.Errorf("steep: %v", err)
	}
}

// record is annotate, which returns the error instead, for goroutines other
// than the test's, and for failures, which must not fail again.
func (h *Harness) record(kind string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("annotation %q: %w", kind, err)
	}
	err = h.session.Annotate(reel.Annotation{Kind: kind, Data: data})
	if errors.Is(err, reel.ErrClosed) {
		return nil
	}
	return err
}

// startNote is a "steep.start" annotation: the harness's configuration.
type startNote struct {
	Test      string `json:"test"`
	Cols      int    `json:"cols"`
	Rows      int    `json:"rows"`
	TimeoutMS int64  `json:"timeout_ms"`
	CheckMS   int64  `json:"check_ms"`
	SettleMS  int64  `json:"settle_ms"`
	SlowMS    int64  `json:"slow_ms,omitzero"`
	ANSI      bool   `json:"ansi"`
	Color     string `json:"color,omitzero"`
	Export    string `json:"export"`
	Messages  bool   `json:"messages,omitzero"`
}

func newStartNote(test string, cfg options) startNote {
	n := startNote{
		Test:      test,
		Cols:      cfg.width,
		Rows:      cfg.height,
		TimeoutMS: cfg.timeout.Milliseconds(),
		CheckMS:   cfg.checkInterval.Milliseconds(),
		SettleMS:  cfg.settleTimeout.Milliseconds(),
		SlowMS:    cfg.slow.Milliseconds(),
		ANSI:      !cfg.stripANSI,
		Export:    [...]string{exportOff: "off", exportOnFailure: "failure", exportAlways: "always"}[cfg.policy],
		Messages:  cfg.recordMessages,
	}
	if cfg.color != 0 {
		n.Color = cfg.color.String()
	}
	return n
}

// readyNote is a "steep.ready" annotation: how the harness's wait for the
// program to start ended.
type readyNote struct {
	Outcome   string `json:"outcome"`
	ElapsedMS int64  `json:"elapsed_ms"`
}

// cell is a cell of the screen, counted from 0 at the top left.
type cell struct {
	Row int `json:"row"`
	Col int `json:"col"`
}

// inputNote is a "steep.input" annotation: an input action of the test.
type inputNote struct {
	Action    string `json:"action"`
	Text      string `json:"text,omitzero"`
	Key       string `json:"key,omitzero"`
	Mouse     string `json:"mouse,omitzero"`
	Button    string `json:"button,omitzero"`
	At        *cell  `json:"at,omitzero"`
	To        *cell  `json:"to,omitzero"`
	Truncated bool   `json:"truncated,omitzero"`
}

// mouseNames are the "mouse" values of [vt.MouseAction] values, and
// buttonNames the "button" values of [vt.MouseButton] values.
var (
	mouseNames  = map[vt.MouseAction]string{vt.MousePress: "press", vt.MouseRelease: "release", vt.MouseMotion: "motion"}
	buttonNames = map[vt.MouseButton]string{vt.MouseUnknown: "none", vt.MouseLeft: "left", vt.MouseRight: "right", vt.MouseMiddle: "middle"}
)

func buttonName(b vt.MouseButton) string {
	if name, ok := buttonNames[b]; ok {
		return name
	}
	return "none"
}

// terminalNote is a "steep.terminal" annotation: a change the test made to
// the terminal that its timeline does not record otherwise.
type terminalNote struct {
	Action    string `json:"action"`
	Lines     *int   `json:"lines,omitzero"`
	Text      string `json:"text,omitzero"`
	Truncated bool   `json:"truncated,omitzero"`
}

// programNote is a "steep.program" annotation: an action of the test on the
// program, past its terminal.
type programNote struct {
	Action    string `json:"action"`
	Type      string `json:"type,omitzero"`
	Value     string `json:"value,omitzero"`
	Truncated bool   `json:"truncated,omitzero"`
}

// match is where text was found on the screen.
type match struct {
	Text string `json:"text"`
	cell
}

// matchAt returns the match of view[i:j], with the row and column, in cells,
// of view[i].
func matchAt(view string, i, j int) match {
	line := strings.LastIndexByte(view[:i], '\n') + 1
	return match{
		Text: view[i:j],
		cell: cell{Row: strings.Count(view[:line], "\n"), Col: ansi.StringWidth(view[line:i])},
	}
}

// clipMatches clips the text of each of matches.
func clipMatches(matches []match, truncated *bool) {
	for i := range matches {
		matches[i].Text = clip(matches[i].Text, truncated)
	}
}

// textMatches returns where each of texts first appears in view, leaving
// out those that do not.
func textMatches(view string, texts []string) []match {
	var out []match
	for _, text := range texts {
		if i := strings.Index(view, text); i >= 0 && text != "" {
			out = append(out, matchAt(view, i, i+len(text)))
		}
	}
	return out
}

// patternMatches returns where pattern first matches view, if it does.
func patternMatches(view, pattern string) []match {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	if loc := re.FindStringIndex(view); loc != nil {
		return []match{matchAt(view, loc[0], loc[1])}
	}
	return nil
}

// typeName returns the name of type T, such as for a message type.
func typeName[T any]() string {
	return reflect.TypeFor[T]().String()
}

// texts returns contents as strings.
func texts[T string | []byte | []string](contents T) []string {
	switch c := any(contents).(type) {
	case string:
		return []string{c}
	case []byte:
		return []string{string(c)}
	case []string:
		return c
	}
	panic("unreachable")
}

// Outcomes of a wait.
const (
	outcomeSatisfied = "satisfied"
	outcomeTimeout   = "timeout"
	outcomeCanceled  = "canceled"
	outcomeFailed    = "failed"
)

// waitNote is a "steep.wait.start" annotation: a wait of the test, as it
// starts.
type waitNote struct {
	ID         int64    `json:"id"`
	Wait       string   `json:"wait"`
	Text       []string `json:"text,omitzero"`
	Pattern    string   `json:"pattern,omitzero"`
	Type       string   `json:"type,omitzero"`
	Live       bool     `json:"live,omitzero"`
	TimeoutMS  int64    `json:"timeout_ms,omitzero"`
	SettleMS   int64    `json:"settle_ms,omitzero"`
	DurationMS int64    `json:"duration_ms,omitzero"`
	Truncated  bool     `json:"truncated,omitzero"`
}

// waitEndNote is a "steep.wait.end" annotation: how a wait ended.
type waitEndNote struct {
	ID        int64   `json:"id"`
	Wait      string  `json:"wait"`
	Outcome   string  `json:"outcome"`
	ElapsedMS int64   `json:"elapsed_ms"`
	Matches   []match `json:"matches,omitzero"`
	Type      string  `json:"type,omitzero"`
	Truncated bool    `json:"truncated,omitzero"`
}

// waitRecord records a wait: its start when [Harness.startWait] makes it,
// and its end when finish runs, deferred by the wait.
type waitRecord struct {
	h       *Harness
	start   time.Time
	ctx     context.Context
	timeout time.Duration
	end     waitEndNote
	ok      bool
}

// startWait records the start of note, a wait with opts, the harness's
// options merged with the call's. Settle waits record their quiet period,
// and others but a sleep their timeout.
func (h *Harness) startWait(note waitNote, opts []Option) *waitRecord {
	h.tb.Helper()
	cfg := collectOptions(h.tb, opts...)
	note.ID = h.waits.Add(1)
	switch note.Wait {
	case "sleep":
	case "settle", "settle_messages":
		note.SettleMS = cfg.settleTimeout.Milliseconds()
		fallthrough
	default:
		note.TimeoutMS = cfg.timeout.Milliseconds()
	}
	note.Text = clipAll(note.Text, &note.Truncated)
	note.Pattern = clip(note.Pattern, &note.Truncated)
	h.annotate(kindWaitStart, note)
	return &waitRecord{
		h:       h,
		start:   time.Now(),
		ctx:     cfg.ctx,
		timeout: cfg.timeout,
		end:     waitEndNote{ID: note.ID, Wait: note.Wait},
	}
}

// satisfied marks the wait as having found what it waited for, at matches,
// or for a message, a message of type typ.
func (w *waitRecord) satisfied(matches []match, typ string) {
	w.ok = true
	w.end.Matches, w.end.Type = matches, typ
}

// finish records the end of the wait. A wait that was not satisfied failed
// the test: it was canceled when its context is done, timed out when it ran
// for its timeout, and otherwise failed for another reason, such as an
// invalid pattern or a program that failed.
func (w *waitRecord) finish() {
	w.h.tb.Helper()
	elapsed := time.Since(w.start)
	switch {
	case w.ok:
		w.end.Outcome = outcomeSatisfied
	case w.ctx.Err() != nil:
		w.end.Outcome = outcomeCanceled
	case w.end.Wait != "sleep" && elapsed >= w.timeout:
		w.end.Outcome = outcomeTimeout
	default:
		w.end.Outcome = outcomeFailed
	}
	w.end.ElapsedMS = elapsed.Milliseconds()
	clipMatches(w.end.Matches, &w.end.Truncated)
	w.h.annotate(kindWaitEnd, w.end)
}

// dims is a width and height, either of which may be left out.
type dims struct {
	Width  *int `json:"width,omitzero"`
	Height *int `json:"height,omitzero"`
}

// assertNote is a "steep.assert" annotation: an assertion or requirement of
// the test, and whether it passed.
type assertNote struct {
	Assert    string   `json:"assert"`
	Required  bool     `json:"required"`
	Pass      bool     `json:"pass"`
	Text      []string `json:"text,omitzero"`
	Pattern   string   `json:"pattern,omitzero"`
	Type      string   `json:"type,omitzero"`
	Expected  *dims    `json:"expected,omitzero"`
	Actual    *dims    `json:"actual,omitzero"`
	Matches   []match  `json:"matches,omitzero"`
	Truncated bool     `json:"truncated,omitzero"`
}

// finishAssert records note, and for a requirement that did not pass, stops
// the test.
func (h *Harness) finishAssert(note assertNote) {
	h.tb.Helper()
	note.Text = clipAll(note.Text, &note.Truncated)
	note.Pattern = clip(note.Pattern, &note.Truncated)
	clipMatches(note.Matches, &note.Truncated)
	h.annotate(kindAssert, note)
	if note.Required && !note.Pass {
		h.tb.FailNow()
	}
}

// snapshotNote is a "steep.snapshot" annotation: a comparison of the screen
// with a snapshot file.
type snapshotNote struct {
	Format    string `json:"format"`
	Path      string `json:"path"`
	Required  bool   `json:"required"`
	Pass      bool   `json:"pass"`
	Updated   bool   `json:"updated,omitzero"`
	Missing   bool   `json:"missing,omitzero"`
	Diff      string `json:"diff,omitzero"`
	Truncated bool   `json:"truncated,omitzero"`
}

// failureNote is a "steep.failure" annotation: a failure the harness
// reported.
type failureNote struct {
	Message   string `json:"message"`
	Truncated bool   `json:"truncated,omitzero"`
}

// noteFailure records a failure the harness reported.
func (h *Harness) noteFailure(msg string) {
	var n failureNote
	n.Message = clip(msg, &n.Truncated)
	// A failure to record a failure is not reported, which would record
	// another.
	_ = h.record(kindFailure, n)
}

// messageNote is a "steep.message" annotation: a message the program's
// model received.
type messageNote struct {
	Type      string `json:"type"`
	Value     string `json:"value"`
	Truncated bool   `json:"truncated,omitzero"`
}

// noteMessage records a message the program's model received, from the
// goroutine that runs the program.
func (h *Harness) noteMessage(msg uv.Event) {
	var n messageNote
	n.Type, n.Value = valueNote(msg, &n.Truncated)
	_ = h.record(kindMessage, n)
}

// valueNote returns the type of v and its value, as fmt's %+v prints it,
// clipped.
func valueNote(v any, truncated *bool) (typ, value string) {
	return fmt.Sprintf("%T", v), clip(fmt.Sprintf("%+v", v), truncated)
}

// exitNote is a "steep.exit" annotation: how the program ended.
type exitNote struct {
	Error     string `json:"error,omitzero"`
	Killed    bool   `json:"killed,omitzero"`
	Truncated bool   `json:"truncated,omitzero"`
}

// resultNote is a "steep.result" annotation: the test's result, as of
// [Harness.Close].
type resultNote struct {
	Test       string `json:"test"`
	Failed     bool   `json:"failed"`
	Panicked   bool   `json:"panicked,omitzero"`
	Skipped    bool   `json:"skipped,omitzero"`
	Failures   int    `json:"failures"`
	DurationMS int64  `json:"duration_ms"`
	Stack      string `json:"stack,omitzero"`
	Truncated  bool   `json:"truncated,omitzero"`
}

// newResultNote returns the test's result so far. For a test that is
// panicking, it holds the stack of the goroutine from where it panicked.
func (h *Harness) newResultNote() resultNote {
	n := resultNote{
		Test:       h.tb.Name(),
		Panicked:   panicking(),
		Skipped:    h.tb.Skipped(),
		Failures:   len(h.failures.messages()),
		DurationMS: time.Since(h.start).Milliseconds(),
	}
	n.Failed = n.Panicked || h.tb.Failed()
	if n.Panicked {
		stack := string(debug.Stack())
		if i := strings.LastIndex(stack, "\npanic("); i >= 0 {
			stack = stack[i+1:]
		}
		n.Stack = clip(stack, &n.Truncated)
	}
	return n
}

// noteExit records how the program ended, from the goroutine that ran it.
func (h *Harness) noteExit(err error) {
	n := exitNote{Killed: errors.Is(err, tea.ErrProgramKilled)}
	if err != nil && !n.Killed {
		n.Error = clip(err.Error(), &n.Truncated)
	}
	_ = h.record(kindExit, n)
}

// sampleView returns one sample of the view, with ANSI sequences stripped
// when opts say to, for an assertion to check and record.
func (h *Harness) sampleView(opts []Option) string {
	h.tb.Helper()
	return renderedView(h.View, collectOptions(h.tb, opts...).stripANSI)
}

// noteInput records n, an input action, with its text clipped.
func (h *Harness) noteInput(n inputNote) {
	h.tb.Helper()
	n.Text = clip(n.Text, &n.Truncated)
	h.annotate(kindInput, n)
}
