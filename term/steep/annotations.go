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

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/vt"
	"github.com/lrstanley/x/term/steep/snapshot"
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

// note is the fields of an annotation the harness records. Each kind has a
// note type of its own, which names the kind and cuts its text, so no
// annotation is recorded with its text uncut.
type note interface {
	// kind returns the kind of the annotation.
	kind() string
	// clip cuts each text field of the note with [reel.ClipText], and marks
	// the note truncated when it cut one.
	clip()
}

// annotate records n in the session's timeline, failing the test when it
// cannot. Once the session is closed, it does nothing.
func (h *Harness) annotate(n note) {
	h.tb.Helper()
	if err := h.record(n); err != nil {
		h.tb.Errorf("steep: %v", err)
	}
}

// record is annotate, which returns the error instead, for goroutines other
// than the test's, and for failures, which must not fail again.
func (h *Harness) record(n note) error {
	n.clip()
	// Annotate checks the annotation against the schema, so it is built
	// without NewAnnotation, which would check it too.
	data, err := json.Marshal(n, json.Deterministic(true))
	if err != nil {
		return fmt.Errorf("steep: %s annotation: %w", n.kind(), err)
	}
	err = h.session.Annotate(reel.Annotation{Kind: n.kind(), Data: data})
	if errors.Is(err, reel.ErrClosed) {
		return nil
	}
	return err
}

// clipAll is [reel.ClipText] for each of texts, in a new slice, or nil for
// nil.
func clipAll(texts []string, truncated *bool) []string {
	if texts == nil {
		return nil
	}
	out := make([]string, len(texts))
	for i, s := range texts {
		out[i] = reel.ClipText(s, truncated)
	}
	return out
}

// outcome is how a wait, or the wait for the program to start, ended.
type outcome string

const (
	outcomeReady     outcome = "ready"
	outcomeSatisfied outcome = "satisfied"
	outcomeTimeout   outcome = "timeout"
	outcomeCanceled  outcome = "canceled"
	outcomeFailed    outcome = "failed"
)

// startNote is a "steep.start" annotation: the harness's configuration.
type startNote struct {
	Test      string `json:"test"`
	Cols      int    `json:"cols"`
	Rows      int    `json:"rows"`
	TimeoutMS int64  `json:"timeout_ms"`
	CheckMS   int64  `json:"check_ms"`
	SettleMS  int64  `json:"settle_ms"`
	SlowMS    int64  `json:"slow_ms,omitzero"`
	TypingMS  int64  `json:"typing_ms,omitzero"`
	ANSI      bool   `json:"ansi"`
	Color     string `json:"color,omitzero"`
	Export    string `json:"export"`
	Messages  bool   `json:"messages,omitzero"`
}

func (*startNote) kind() string { return kindStart }
func (*startNote) clip()        {}

func newStartNote(test string, cfg options) *startNote {
	n := &startNote{
		Test:      test,
		Cols:      cfg.width,
		Rows:      cfg.height,
		TimeoutMS: cfg.timeout.Milliseconds(),
		CheckMS:   cfg.checkInterval.Milliseconds(),
		SettleMS:  cfg.settleTimeout.Milliseconds(),
		SlowMS:    cfg.slow.Milliseconds(),
		TypingMS:  cfg.typingDelay.Milliseconds(),
		ANSI:      !cfg.stripANSI,
		Export:    cfg.policy.String(),
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
	Outcome   outcome `json:"outcome"`
	ElapsedMS int64   `json:"elapsed_ms"`
}

func (*readyNote) kind() string { return kindReady }
func (*readyNote) clip()        {}

// cell is a cell of the screen, counted from 0 at the top left.
type cell struct {
	Row int `json:"row"`
	Col int `json:"col"`
}

// inputNote is a "steep.input" annotation: an input action of the test,
// with the metadata of its hook event.
type inputNote struct {
	InputMeta
	At        *cell `json:"at,omitzero"`
	To        *cell `json:"to,omitzero"`
	Truncated bool  `json:"truncated,omitzero"`
}

func (*inputNote) kind() string { return kindInput }
func (n *inputNote) clip()      { n.Text = reel.ClipText(n.Text, &n.Truncated) }

// mouseNames are the "mouse" values of [vt.MouseAction] values, and
// buttonNames the "button" values of [vt.MouseButton] values. See
// [mouseName] and [buttonName] for those they leave out.
var (
	mouseNames  = map[vt.MouseAction]string{vt.MousePress: "press", vt.MouseRelease: "release", vt.MouseMotion: "motion"}
	buttonNames = map[vt.MouseButton]string{vt.MouseLeft: "left", vt.MouseRight: "right", vt.MouseMiddle: "middle"}
)

// mouseName returns the "mouse" value of action, or "unknown" for an action
// it does not name.
func mouseName(action vt.MouseAction) string {
	if name, ok := mouseNames[action]; ok {
		return name
	}
	return "unknown"
}

// buttonName returns the "button" value of b, or "none" for no button, such
// as [vt.MouseUnknown] for motion without one.
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

func (*terminalNote) kind() string { return kindTerminal }
func (n *terminalNote) clip()      { n.Text = reel.ClipText(n.Text, &n.Truncated) }

// programNote is a "steep.program" annotation: an action of the test on the
// program, past its terminal.
type programNote struct {
	Action    string `json:"action"`
	Type      string `json:"type,omitzero"`
	Value     string `json:"value,omitzero"`
	Truncated bool   `json:"truncated,omitzero"`
}

func (*programNote) kind() string { return kindProgram }
func (n *programNote) clip()      { n.Value = reel.ClipText(n.Value, &n.Truncated) }

// valueNote returns the type of v and its value, as fmt's %+v prints it.
func valueNote(v any) (typ, value string) {
	return fmt.Sprintf("%T", v), fmt.Sprintf("%+v", v)
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
		matches[i].Text = reel.ClipText(matches[i].Text, truncated)
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

// patternMatches returns where re first matches view, if it does.
func patternMatches(re *regexp.Regexp, view string) []match {
	if loc := re.FindStringIndex(view); loc != nil {
		return []match{matchAt(view, loc[0], loc[1])}
	}
	return nil
}

// typeName returns the name of type T, such as for a message type.
func typeName[T any]() string {
	return reflect.TypeFor[T]().String()
}

// waitNote is a "steep.wait.start" annotation: a wait of the test, as it
// starts, with the metadata of its hook event.
type waitNote struct {
	ID int64 `json:"id"`
	WaitMeta
	Text       []string `json:"text,omitzero"`
	Pattern    string   `json:"pattern,omitzero"`
	Type       string   `json:"type,omitzero"`
	TimeoutMS  int64    `json:"timeout_ms,omitzero"`
	SettleMS   int64    `json:"settle_ms,omitzero"`
	DurationMS int64    `json:"duration_ms,omitzero"`
	Truncated  bool     `json:"truncated,omitzero"`
}

func (*waitNote) kind() string { return kindWaitStart }

func (n *waitNote) clip() {
	n.Text = clipAll(n.Text, &n.Truncated)
	n.Pattern = reel.ClipText(n.Pattern, &n.Truncated)
}

// waitEndNote is a "steep.wait.end" annotation: how a wait ended.
type waitEndNote struct {
	ID        int64    `json:"id"`
	Wait      WaitKind `json:"wait"`
	Outcome   outcome  `json:"outcome"`
	ElapsedMS int64    `json:"elapsed_ms"`
	Matches   []match  `json:"matches,omitzero"`
	Type      string   `json:"type,omitzero"`
	Truncated bool     `json:"truncated,omitzero"`
}

func (*waitEndNote) kind() string { return kindWaitEnd }
func (n *waitEndNote) clip()      { clipMatches(n.Matches, &n.Truncated) }

// waitRecord is a wait being recorded. The wait sets how it ended where it
// decides that, before it fails the test for one that was not satisfied, and
// defers [waitRecord.finish], which records the end after the failure. A
// nil waitRecord, of a wait without a harness, records nothing.
type waitRecord struct {
	h     *Harness
	start time.Time
	end   waitEndNote
}

// startWait records the start of n, a wait of the harness, and returns its
// record.
func (h *Harness) startWait(n waitNote) *waitRecord {
	h.tb.Helper()
	n.ID = h.waits.Add(1)
	h.annotate(&n)
	return &waitRecord{h: h, start: time.Now(), end: waitEndNote{ID: n.ID, Wait: n.Kind}}
}

// startWait records the start of n, a wait with the options' timeout, for
// the harness the options are for, and returns its record, or nil without
// one.
func (o options) startWait(n waitNote) *waitRecord {
	if o.harness == nil {
		return nil
	}
	o.harness.tb.Helper()
	n.TimeoutMS = o.timeout.Milliseconds()
	return o.harness.startWait(n)
}

// ended sets how the wait ended.
func (w *waitRecord) ended(o outcome) {
	if w != nil {
		w.end.Outcome = o
	}
}

// foundOn sets the wait as satisfied by view, where find, when not nil,
// finds what the wait waited for.
func (w *waitRecord) foundOn(view string, find func(view string) []match) {
	if w == nil {
		return
	}
	w.end.Outcome = outcomeSatisfied
	if find != nil {
		w.end.Matches = find(view)
	}
}

// received sets the wait as satisfied by msg.
func (w *waitRecord) received(msg uv.Event) {
	if w != nil {
		w.end.Outcome = outcomeSatisfied
		w.end.Type = fmt.Sprintf("%T", msg)
	}
}

// finish records the end of the wait. A wait that did not set how it ended,
// such as one a panic stopped, records no end.
func (w *waitRecord) finish() {
	if w == nil || w.end.Outcome == "" {
		return
	}
	w.h.tb.Helper()
	w.end.ElapsedMS = time.Since(w.start).Milliseconds()
	w.h.annotate(&w.end)
}

// errWaitTimeout is the cause of the context of a wait for messages that
// ran for its timeout.
var errWaitTimeout = errors.New("steep: wait timed out")

// doneOutcome returns how a wait for messages ended, which stopped when ctx,
// a context from [context.WithTimeoutCause] with errWaitTimeout, was done or
// its messages ended.
func doneOutcome(ctx context.Context) outcome {
	switch {
	case ctx.Err() == nil:
		return outcomeFailed
	case errors.Is(context.Cause(ctx), errWaitTimeout):
		return outcomeTimeout
	default:
		return outcomeCanceled
	}
}

// dims is a width and height, either of which may be left out.
type dims struct {
	Width  *int `json:"width,omitzero"`
	Height *int `json:"height,omitzero"`
}

// assertNote is a "steep.assert" annotation: an assertion or requirement of
// the test, with the metadata of its hook event, and whether it passed.
type assertNote struct {
	AssertMeta
	Pass      bool     `json:"pass"`
	Text      []string `json:"text,omitzero"`
	Pattern   string   `json:"pattern,omitzero"`
	Type      string   `json:"type,omitzero"`
	Expected  *dims    `json:"expected,omitzero"`
	Actual    *dims    `json:"actual,omitzero"`
	Matches   []match  `json:"matches,omitzero"`
	Truncated bool     `json:"truncated,omitzero"`
}

func (*assertNote) kind() string { return kindAssert }

func (n *assertNote) clip() {
	n.Text = clipAll(n.Text, &n.Truncated)
	n.Pattern = reel.ClipText(n.Pattern, &n.Truncated)
	clipMatches(n.Matches, &n.Truncated)
}

// noteAssert records n, an assertion of kind, for the harness the options
// are for, if any, as a requirement when they are for one (see
// [withRequired]). find, when not nil, returns where a passing assertion
// found what it asserted.
func (o options) noteAssert(kind AssertKind, n assertNote, find func() []match) {
	if o.harness == nil {
		return
	}
	o.harness.tb.Helper()
	n.AssertMeta = AssertMeta{Kind: kind, Required: o.required}
	if n.Pass && find != nil {
		n.Matches = find()
	}
	o.harness.annotate(&n)
}

// snapshotNote is a "steep.snapshot" annotation: a comparison of the screen
// with a snapshot file, with the metadata of its hook event.
type snapshotNote struct {
	CaptureMeta
	Path      string `json:"path"`
	Pass      bool   `json:"pass"`
	Updated   bool   `json:"updated,omitzero"`
	Missing   bool   `json:"missing,omitzero"`
	Diff      string `json:"diff,omitzero"`
	Truncated bool   `json:"truncated,omitzero"`
}

func (*snapshotNote) kind() string { return kindSnapshot }
func (n *snapshotNote) clip()      { n.Diff = reel.ClipText(n.Diff, &n.Truncated) }

// noteSnapshot records n, a comparison of the screen, with what res, from
// [snapshot.WithResult], says it found.
func (h *Harness) noteSnapshot(n snapshotNote, res snapshot.Result) {
	h.tb.Helper()
	n.Path, n.Updated, n.Missing, n.Diff = res.Path, res.Updated, res.Missing, res.Diff
	h.annotate(&n)
}

// failureNote is a "steep.failure" annotation: a failure the harness
// reported.
type failureNote struct {
	Message   string `json:"message"`
	Truncated bool   `json:"truncated,omitzero"`
}

func (*failureNote) kind() string { return kindFailure }
func (n *failureNote) clip()      { n.Message = reel.ClipText(n.Message, &n.Truncated) }

// noteFailure records a failure the harness reported.
func (h *Harness) noteFailure(msg string) {
	// A failure to record a failure is not reported, which would record
	// another.
	_ = h.record(&failureNote{Message: msg})
}

// messageNote is a "steep.message" annotation: a message the program's
// model received.
type messageNote struct {
	Type      string `json:"type"`
	Value     string `json:"value"`
	Truncated bool   `json:"truncated,omitzero"`
}

func (*messageNote) kind() string { return kindMessage }
func (n *messageNote) clip()      { n.Value = reel.ClipText(n.Value, &n.Truncated) }

// noteMessage records a message the program's model received, from the
// goroutine that runs the program.
func (h *Harness) noteMessage(msg uv.Event) {
	var n messageNote
	n.Type, n.Value = valueNote(msg)
	_ = h.record(&n)
}

// exitNote is a "steep.exit" annotation: how the program ended.
type exitNote struct {
	Error     string `json:"error,omitzero"`
	Killed    bool   `json:"killed,omitzero"`
	Truncated bool   `json:"truncated,omitzero"`
}

func (*exitNote) kind() string { return kindExit }
func (n *exitNote) clip()      { n.Error = reel.ClipText(n.Error, &n.Truncated) }

// noteExit records how the program ended, from the goroutine that ran it.
func (h *Harness) noteExit(err error) {
	n := exitNote{Killed: errors.Is(err, tea.ErrProgramKilled)}
	if err != nil && !n.Killed {
		n.Error = err.Error()
	}
	_ = h.record(&n)
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

func (*resultNote) kind() string { return kindResult }
func (n *resultNote) clip()      { n.Stack = reel.ClipText(n.Stack, &n.Truncated) }

// newResultNote returns the test's result so far. For a test that is
// panicking, it holds the stack of the goroutine from where it panicked.
func (h *Harness) newResultNote() *resultNote {
	n := &resultNote{
		Test:       h.tb.Name(),
		Skipped:    h.tb.Skipped(),
		Failures:   len(h.failures.messages()),
		DurationMS: time.Since(h.start).Milliseconds(),
	}
	n.Failed, n.Panicked = h.testStatus()
	if n.Panicked {
		stack := string(debug.Stack())
		if i := strings.LastIndex(stack, "\npanic("); i >= 0 {
			stack = stack[i+1:]
		}
		n.Stack = stack
	}
	return n
}
