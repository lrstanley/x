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
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/vt"
	"github.com/lrstanley/x/term/steep/snapshot"
)

// Kinds of the [reel.Annotation] events a harness records, as described by
// reel's annotation.schema.json.
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

// note is the data of an annotation the harness records.
type note interface {
	kind() string
	// clip cuts each text field with [reel.ClipText], marking the note
	// truncated when it cut one.
	clip()
}

// annotate records n, failing the test when it cannot. It does nothing once
// the session is closed.
func (h *Harness) annotate(n note) {
	h.tb.Helper()
	if err := h.record(n); err != nil {
		h.tb.Errorf("steep: %v", err)
	}
}

// record is annotate returning the error instead, for goroutines other than
// the test's, and for failures, which must not fail again.
func (h *Harness) record(n note) error {
	n.clip()
	// Not NewAnnotation: Annotate already checks against the schema.
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

// clipAll is [reel.ClipText] for each of texts, in a new slice.
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

// startNote is a "steep.start" annotation.
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

// readyNote is a "steep.ready" annotation.
type readyNote struct {
	Outcome   outcome `json:"outcome"`
	ElapsedMS int64   `json:"elapsed_ms"`
}

func (*readyNote) kind() string { return kindReady }
func (*readyNote) clip()        {}

// cell is a 0-based screen position.
type cell struct {
	Row int `json:"row"`
	Col int `json:"col"`
}

// inputNote is a "steep.input" annotation.
type inputNote struct {
	InputMeta
	At        *cell `json:"at,omitzero"`
	To        *cell `json:"to,omitzero"`
	Truncated bool  `json:"truncated,omitzero"`
}

func (*inputNote) kind() string { return kindInput }
func (n *inputNote) clip()      { n.Text = reel.ClipText(n.Text, &n.Truncated) }

// mouseNames and buttonNames are the annotation names of mouse actions and
// buttons. Others are named by mouseName and buttonName's fallbacks.
//
//nolint:exhaustive // Only the actions and buttons steep sends have names.
var (
	mouseNames  = map[vt.MouseAction]string{vt.MousePress: "press", vt.MouseRelease: "release", vt.MouseMotion: "motion"}
	buttonNames = map[vt.MouseButton]string{vt.MouseLeft: "left", vt.MouseRight: "right", vt.MouseMiddle: "middle"}
)

// mouseName returns the annotation name of action, or "unknown".
func mouseName(action vt.MouseAction) string {
	if name, ok := mouseNames[action]; ok {
		return name
	}
	return unknownName
}

// buttonName returns the annotation name of b, or "none", such as for
// motion without a button.
func buttonName(b vt.MouseButton) string {
	if name, ok := buttonNames[b]; ok {
		return name
	}
	return "none"
}

// terminalNote is a "steep.terminal" annotation, for terminal changes no
// other timeline event records. It leaves out the clipboard's text, which
// may be a secret.
type terminalNote struct {
	Action string `json:"action"`
}

func (*terminalNote) kind() string { return kindTerminal }
func (*terminalNote) clip()        {}

// programNote is a "steep.program" annotation, for actions on the program
// that bypass its terminal.
type programNote struct {
	Action    string `json:"action"`
	Type      string `json:"type,omitzero"`
	Value     string `json:"value,omitzero"`
	Truncated bool   `json:"truncated,omitzero"`
}

func (*programNote) kind() string { return kindProgram }
func (n *programNote) clip()      { n.Value = reel.ClipText(n.Value, &n.Truncated) }

// valueNote returns v's type and its %+v formatting.
func valueNote(v any) (typ, value string) {
	return fmt.Sprintf("%T", v), fmt.Sprintf("%+v", v)
}

func clipMatches(matches []reel.Match, truncated *bool) {
	for i := range matches {
		matches[i].Text = reel.ClipText(matches[i].Text, truncated)
	}
}

func typeName[T any]() string {
	return reflect.TypeFor[T]().String()
}

// waitNote is a "steep.wait.start" annotation.
type waitNote struct {
	ID int64 `json:"id"`
	WaitMeta
	Text       []string `json:"text,omitzero"`
	Condition  string   `json:"condition,omitzero"`
	Type       string   `json:"type,omitzero"`
	TimeoutMS  int64    `json:"timeout_ms,omitzero"`
	SettleMS   int64    `json:"settle_ms,omitzero"`
	DurationMS int64    `json:"duration_ms,omitzero"`
	Truncated  bool     `json:"truncated,omitzero"`
}

func (*waitNote) kind() string { return kindWaitStart }

func (n *waitNote) clip() {
	n.Text = clipAll(n.Text, &n.Truncated)
	n.Condition = reel.ClipText(n.Condition, &n.Truncated)
}

// waitEndNote is a "steep.wait.end" annotation.
type waitEndNote struct {
	ID        int64        `json:"id"`
	Wait      WaitKind     `json:"wait"`
	Outcome   outcome      `json:"outcome"`
	ElapsedMS int64        `json:"elapsed_ms"`
	Matches   []reel.Match `json:"matches,omitzero"`
	Type      string       `json:"type,omitzero"`
	Truncated bool         `json:"truncated,omitzero"`
}

func (*waitEndNote) kind() string { return kindWaitEnd }
func (n *waitEndNote) clip()      { clipMatches(n.Matches, &n.Truncated) }

// waitRecord is a wait being recorded. A wait sets its outcome before it
// fails the test, and defers [waitRecord.finish] to record the end. A nil
// waitRecord, for a wait without a harness, records nothing.
type waitRecord struct {
	h     *Harness
	start time.Time
	end   waitEndNote
}

// startWait records the start of n and returns its record.
func (h *Harness) startWait(n waitNote) *waitRecord {
	h.tb.Helper()
	n.ID = h.waits.Add(1)
	h.annotate(&n)
	return &waitRecord{h: h, start: time.Now(), end: waitEndNote{ID: n.ID, Wait: n.Kind}}
}

// startWait records the start of n, with the options' timeout, for the
// options' harness, or returns nil without one.
func (o options) startWait(n waitNote) *waitRecord {
	if o.harness == nil {
		return nil
	}
	o.harness.tb.Helper()
	n.TimeoutMS = o.timeout.Milliseconds()
	return o.harness.startWait(n)
}

func (w *waitRecord) ended(o outcome) {
	if w != nil {
		w.end.Outcome = o
	}
}

// satisfied sets the wait as satisfied, where matches locate what it
// waited for.
func (w *waitRecord) satisfied(matches []reel.Match) {
	if w != nil {
		w.end.Outcome = outcomeSatisfied
		w.end.Matches = matches
	}
}

func (w *waitRecord) received(msg uv.Event) {
	if w != nil {
		w.end.Outcome = outcomeSatisfied
		w.end.Type = fmt.Sprintf("%T", msg)
	}
}

// finish records the end of the wait, unless no outcome was set, such as
// when a panic stopped it.
func (w *waitRecord) finish() {
	if w == nil || w.end.Outcome == "" {
		return
	}
	w.h.tb.Helper()
	w.end.ElapsedMS = time.Since(w.start).Milliseconds()
	w.h.annotate(&w.end)
}

// errWaitTimeout is the context cause of a wait that timed out.
var errWaitTimeout = errors.New("steep: timeout")

// doneOutcome returns how a wait on ctx, a context with errWaitTimeout as
// its timeout cause, ended without a match. A wait whose timeout passed timed
// out, even if its parent was canceled too.
func doneOutcome(ctx context.Context) outcome {
	deadline, ok := ctx.Deadline()
	switch {
	case ctx.Err() == nil:
		return outcomeFailed
	case errors.Is(context.Cause(ctx), errWaitTimeout), ok && !time.Now().Before(deadline):
		return outcomeTimeout
	default:
		return outcomeCanceled
	}
}

// dims is a width and height, either of which may be omitted.
type dims struct {
	Width  *int `json:"width,omitzero"`
	Height *int `json:"height,omitzero"`
}

// assertNote is a "steep.assert" annotation.
type assertNote struct {
	AssertMeta
	Pass      bool         `json:"pass"`
	Text      []string     `json:"text,omitzero"`
	Condition string       `json:"condition,omitzero"`
	Type      string       `json:"type,omitzero"`
	Expected  *dims        `json:"expected,omitzero"`
	Actual    *dims        `json:"actual,omitzero"`
	Matches   []reel.Match `json:"matches,omitzero"`
	Truncated bool         `json:"truncated,omitzero"`
}

func (*assertNote) kind() string { return kindAssert }

func (n *assertNote) clip() {
	n.Text = clipAll(n.Text, &n.Truncated)
	n.Condition = reel.ClipText(n.Condition, &n.Truncated)
	clipMatches(n.Matches, &n.Truncated)
}

// noteAssert records n for the options' harness, if any.
func (o options) noteAssert(n assertNote) {
	if o.harness == nil {
		return
	}
	o.harness.tb.Helper()
	o.harness.annotate(&n)
}

// snapshotNote is a "steep.snapshot" annotation.
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

// noteSnapshot records n with the outcome in res.
func (h *Harness) noteSnapshot(n snapshotNote, res snapshot.Result) {
	h.tb.Helper()
	n.Path, n.Updated, n.Missing, n.Diff = res.Path, res.Updated, res.Missing, res.Diff
	h.annotate(&n)
}

// failureNote is a "steep.failure" annotation.
type failureNote struct {
	Message   string `json:"message"`
	Truncated bool   `json:"truncated,omitzero"`
}

func (*failureNote) kind() string { return kindFailure }
func (n *failureNote) clip()      { n.Message = reel.ClipText(n.Message, &n.Truncated) }

// noteFailure records the nth failure the harness reported, and marks it
// (see [Harness.Mark]). Errors are dropped, since reporting them would
// record another failure.
func (h *Harness) noteFailure(n int, msg string) {
	_ = h.record(&failureNote{Message: msg})
	_ = h.session.Mark(failurePrefix + strconv.Itoa(n))
}

// messageNote is a "steep.message" annotation.
type messageNote struct {
	Type      string `json:"type"`
	Value     string `json:"value"`
	Truncated bool   `json:"truncated,omitzero"`
}

func (*messageNote) kind() string { return kindMessage }
func (n *messageNote) clip()      { n.Value = reel.ClipText(n.Value, &n.Truncated) }

// noteMessage records a message the model received. It runs on the
// program's goroutine.
func (h *Harness) noteMessage(msg uv.Event) {
	var n messageNote
	n.Type, n.Value = valueNote(msg)
	_ = h.record(&n)
}

// exitNote is a "steep.exit" annotation.
type exitNote struct {
	Error     string `json:"error,omitzero"`
	Killed    bool   `json:"killed,omitzero"`
	Truncated bool   `json:"truncated,omitzero"`
}

func (*exitNote) kind() string { return kindExit }
func (n *exitNote) clip()      { n.Error = reel.ClipText(n.Error, &n.Truncated) }

// noteExit records how the program ended. It runs on the program's
// goroutine.
func (h *Harness) noteExit(err error) {
	n := exitNote{Killed: errors.Is(err, tea.ErrProgramKilled)}
	if err != nil && !n.Killed {
		n.Error = err.Error()
	}
	_ = h.record(&n)
}

// resultNote is a "steep.result" annotation, recorded by [Harness.Close].
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

// newResultNote returns the test's result so far, with the panic's stack
// for a panicking test.
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
