// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/lrstanley/x/charm/steep/snapshot"
)

type runResult struct {
	model tea.Model
	err   error
}

// Harness is a test harness for a Bubble Tea program.
type Harness struct {
	tb       testing.TB
	program  *tea.Program
	observer *observer
	output   *bytes.Buffer
	opts     []Option

	resultMu sync.RWMutex
	result   *runResult
	done     chan runResult
}

// NewHarness creates a new test harness for a Bubble Tea program (one which has
// a [tea.Model] as the root model). The test harness will run the program,
// capture its output, and provide assertions for the program's behavior.
func NewHarness(tb testing.TB, model tea.Model, opts ...Option) *Harness {
	tb.Helper()

	cfg := collectOptions(opts...)

	h := &Harness{
		tb:       tb,
		observer: newObserver(tb, model),
		output:   &bytes.Buffer{},
		done:     make(chan runResult, 1),
		opts:     append([]Option(nil), opts...),
	}

	h.program = tea.NewProgram(
		h.observer,
		append(
			cfg.programOpts,
			tea.WithContext(tb.Context()),
			tea.WithInput(nil),
			tea.WithOutput(h.output),
			tea.WithoutSignals(),
			tea.WithWindowSize(cfg.width, cfg.height),
		)...,
	)

	tb.Cleanup(func() {
		h.program.Quit()
		h.WaitFinished()
	})

	go func() {
		finalModel, err := h.program.Run()
		h.done <- runResult{
			model: finalModel,
			err:   err,
		}
	}()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	go func() {
		defer cancel()

		<-ctx.Done()
		tb.Logf("received signal %s, quitting", ctx.Err())
		h.program.Kill()
	}()

	return h
}

// NewComponentHarness creates a new test harness for a Bubble Tea component model.
// This effectively emulates a component as a full Bubble Tea program.
func NewComponentHarness[M any](tb testing.TB, model M, opts ...Option) *Harness {
	tb.Helper()

	m := &componentWrapper[M]{tb: tb, model: model}

	m.validate()

	return NewHarness(tb, m, opts...)
}

func (h *Harness) mergedOpts(call ...Option) []Option {
	return append(h.opts, call...)
}

// Send sends msg to the running Bubble Tea program.
func (h *Harness) Send(msg tea.Msg) {
	h.program.Send(msg)
}

// Type sends s as a sequence of key press messages.
func (h *Harness) Type(s string) {
	for _, r := range s {
		h.Send(tea.KeyPressMsg(tea.Key{Code: r, Text: string(r)}))
	}
}

// Quit asks the running Bubble Tea program to exit.
func (h *Harness) Quit() error {
	h.program.Quit()
	return nil
}

// FinalOutput waits for the Bubble Tea program to finish and returns the last
// captured output.
func (h *Harness) FinalOutput(opts ...Option) io.Reader {
	h.tb.Helper()
	h.WaitFinished(opts...)
	return h.Output()
}

// Output returns the last captured output.
func (h *Harness) Output() io.Reader {
	return h.output
}

// FinalView waits for the Bubble Tea program to finish and returns the last
// captured view content.
func (h *Harness) FinalView(opts ...Option) string {
	h.tb.Helper()
	h.WaitFinished(opts...)
	h.observer.mu.RLock()
	defer h.observer.mu.RUnlock()
	return h.observer.lastViewSnapshot
}

// FinalModel waits for the Bubble Tea program to finish and returns the latest
// underlying root model.
func (h *Harness) FinalModel(opts ...Option) tea.Model {
	h.tb.Helper()
	h.WaitFinished(opts...)
	return h.observer.currentModel()
}

// Messages returns a copy of messages observed by the underlying model.
func (h *Harness) Messages() []tea.Msg {
	return h.observer.messages()
}

// View invokes the current underlying models View method and returns the result.
func (h *Harness) View() string {
	return h.observer.View().Content
}

// WaitFinished waits for the Bubble Tea program to finish.
func (h *Harness) WaitFinished(opts ...Option) {
	h.tb.Helper()

	cfg := collectOptions(h.mergedOpts(opts...)...)
	h.resultMu.RLock()
	if result := h.result; result != nil {
		h.resultMu.RUnlock()
		if result.err != nil && !errors.Is(result.err, tea.ErrProgramKilled) {
			h.tb.Fatalf("bubble tea program failed: %v", result.err)
		}
		return
	}
	h.resultMu.RUnlock()

	timer := time.NewTimer(cfg.timeout)
	defer timer.Stop()

	select {
	case result := <-h.done:
		h.resultMu.Lock()
		h.result = &result
		h.resultMu.Unlock()
		if result.err != nil && !errors.Is(result.err, tea.ErrProgramKilled) {
			h.tb.Fatalf("bubble tea program failed: %v", result.err)
		}
		if wrapper, ok := result.model.(*observer); ok {
			h.observer.replace(wrapper.currentModel())
		}
	case <-timer.C:
		h.tb.Fatalf("timeout waiting for bubble tea program to finish after %s", cfg.timeout)
	}
}

// WaitSettleMessages waits until no messages have been observed for the
// configured settle timeout. See also [WithSettleTimeout], [WithCheckInterval],
// and [WithTimeout].
func (h *Harness) WaitSettleMessages(opts ...Option) *Harness {
	h.tb.Helper()

	cfg := collectOptions(h.mergedOpts(opts...)...)
	h.observer.setSettleIgnore(cfg.settleIgnore)
	defer h.observer.setSettleIgnore(nil)

	deadline := time.Now().Add(cfg.timeout)
	ctx := h.tb.Context()
	timer := time.NewTimer(cfg.timeout)
	defer timer.Stop()

	for {
		h.observer.mu.RLock()
		updateCount, lastReceivedMessage := len(h.observer.observedMsgs), h.observer.lastReceivedMessage
		h.observer.mu.RUnlock()
		now := time.Now()
		quietFor := now.Sub(lastReceivedMessage)

		if quietFor >= cfg.settleTimeout {
			return h
		}

		remainingTimeout := deadline.Sub(now)
		if remainingTimeout <= 0 {
			h.tb.Fatalf(
				"timeout waiting for Update() to settle after %s; last update was %s ago after %d update(s)",
				cfg.timeout,
				quietFor,
				updateCount,
			)
			return h
		}

		timer.Reset(min(cfg.checkInterval, cfg.settleTimeout-quietFor, remainingTimeout))
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			h.tb.Fatalf("wait for Update() to settle canceled: %v", ctx.Err())
		}
	}
}

// WaitSettleView waits until the rendered view string has not changed for the
// configured settle timeout. It polls this harness's [Harness.View] and compares
// each result to the previous sample. See also [WithSettleTimeout],
// [WithCheckInterval], and [WithTimeout].
func (h *Harness) WaitSettleView(opts ...Option) *Harness {
	h.tb.Helper()
	WaitSettleView(h.tb, h, h.mergedOpts(opts...)...)
	return h
}

// WaitViewBytes waits until condition returns true for the latest view output.
func (h *Harness) WaitViewBytes(condition func(view []byte) bool, opts ...Option) *Harness {
	h.tb.Helper()
	WaitView(h.tb, h, condition, h.mergedOpts(opts...)...)
	return h
}

// WaitViewString waits until condition returns true for the latest view output.
func (h *Harness) WaitViewString(condition func(view string) bool, opts ...Option) *Harness {
	h.tb.Helper()
	WaitView(h.tb, h, condition, h.mergedOpts(opts...)...)
	return h
}

// WaitBytes waits until output contains contents.
func (h *Harness) WaitBytes(contents []byte, opts ...Option) *Harness {
	h.tb.Helper()
	WaitBytes(h.tb, h, contents, h.mergedOpts(opts...)...)
	return h
}

// WaitString waits until output contains contents.
func (h *Harness) WaitString(contents string, opts ...Option) *Harness {
	h.tb.Helper()
	WaitString(h.tb, h, contents, h.mergedOpts(opts...)...)
	return h
}

// WaitStrings waits until output contains all contents.
func (h *Harness) WaitStrings(contents []string, opts ...Option) *Harness {
	h.tb.Helper()
	WaitStrings(h.tb, h, contents, h.mergedOpts(opts...)...)
	return h
}

// WaitNotBytes waits until output contains none of the contents.
func (h *Harness) WaitNotBytes(contents []byte, opts ...Option) *Harness {
	h.tb.Helper()
	WaitNotBytes(h.tb, h, contents, h.mergedOpts(opts...)...)
	return h
}

// WaitNotString waits until output contains none of the contents.
func (h *Harness) WaitNotString(contents string, opts ...Option) *Harness {
	h.tb.Helper()
	WaitNotString(h.tb, h, contents, h.mergedOpts(opts...)...)
	return h
}

// WaitNotStrings waits until output contains none of the contents.
func (h *Harness) WaitNotStrings(contents []string, opts ...Option) *Harness {
	h.tb.Helper()
	WaitNotStrings(h.tb, h, contents, h.mergedOpts(opts...)...)
	return h
}

// WaitMatch waits until the latest view output matches the regular expression
// pattern. See [WaitMatch].
func (h *Harness) WaitMatch(pattern string, opts ...Option) *Harness {
	h.tb.Helper()
	WaitMatch(h.tb, h, pattern, h.mergedOpts(opts...)...)
	return h
}

// WaitNotMatch waits until the latest view output does not match the regular
// expression pattern. See [WaitNotMatch].
func (h *Harness) WaitNotMatch(pattern string, opts ...Option) *Harness {
	h.tb.Helper()
	WaitNotMatch(h.tb, h, pattern, h.mergedOpts(opts...)...)
	return h
}

// AssertString reports an error unless content appears in output. It
// allows the test to continue.
func (h *Harness) AssertString(content string, opts ...Option) *Harness {
	h.tb.Helper()
	AssertString(h.tb, h, content, h.mergedOpts(opts...)...)
	return h
}

// RequireString fails the test immediately unless content appears in output.
func (h *Harness) RequireString(content string, opts ...Option) *Harness {
	h.tb.Helper()
	if !AssertString(h.tb, h, content, h.mergedOpts(opts...)...) {
		h.tb.FailNow()
	}
	return h
}

// AssertStrings reports an error unless every substring in contents
// appears in output. It allows the test to continue.
func (h *Harness) AssertStrings(contents []string, opts ...Option) *Harness {
	h.tb.Helper()
	AssertStrings(h.tb, h, contents, h.mergedOpts(opts...)...)
	return h
}

// RequireStrings fails the test immediately unless every substring in
// contents appears in output.
func (h *Harness) RequireStrings(contents []string, opts ...Option) *Harness {
	h.tb.Helper()
	if !AssertStrings(h.tb, h, contents, h.mergedOpts(opts...)...) {
		h.tb.FailNow()
	}
	return h
}

// AssertNotString reports an error if content appears in output. It
// allows the test to continue.
func (h *Harness) AssertNotString(content string, opts ...Option) *Harness {
	h.tb.Helper()
	AssertNotString(h.tb, h, content, h.mergedOpts(opts...)...)
	return h
}

// RequireNotString fails the test immediately if content appears in output.
func (h *Harness) RequireNotString(content string, opts ...Option) *Harness {
	h.tb.Helper()
	if !AssertNotString(h.tb, h, content, h.mergedOpts(opts...)...) {
		h.tb.FailNow()
	}
	return h
}

// AssertNotStrings reports an error if any substring in contents
// appears in output. It allows the test to continue.
func (h *Harness) AssertNotStrings(contents []string, opts ...Option) *Harness {
	h.tb.Helper()
	AssertNotStrings(h.tb, h, contents, h.mergedOpts(opts...)...)
	return h
}

// RequireNotStrings fails the test immediately if any substring in
// contents appears in output.
func (h *Harness) RequireNotStrings(contents []string, opts ...Option) *Harness {
	h.tb.Helper()
	if !AssertNotStrings(h.tb, h, contents, h.mergedOpts(opts...)...) {
		h.tb.FailNow()
	}
	return h
}

// AssertMatch reports an error unless output matches the regular expression
// pattern. See [AssertMatch].
func (h *Harness) AssertMatch(pattern string, opts ...Option) *Harness {
	h.tb.Helper()
	AssertMatch(h.tb, h, pattern, h.mergedOpts(opts...)...)
	return h
}

// RequireMatch fails the test immediately unless output matches the regular
// expression pattern.
func (h *Harness) RequireMatch(pattern string, opts ...Option) *Harness {
	h.tb.Helper()
	if !AssertMatch(h.tb, h, pattern, h.mergedOpts(opts...)...) {
		h.tb.FailNow()
	}
	return h
}

// AssertNotMatch reports an error if output matches the regular expression
// pattern. See [AssertNotMatch].
func (h *Harness) AssertNotMatch(pattern string, opts ...Option) *Harness {
	h.tb.Helper()
	AssertNotMatch(h.tb, h, pattern, h.mergedOpts(opts...)...)
	return h
}

// RequireNotMatch fails the test immediately if output matches the regular
// expression pattern.
func (h *Harness) RequireNotMatch(pattern string, opts ...Option) *Harness {
	h.tb.Helper()
	if !AssertNotMatch(h.tb, h, pattern, h.mergedOpts(opts...)...) {
		h.tb.FailNow()
	}
	return h
}

// AssertHeight reports an error unless output has height rows. It allows the
// test to continue.
func (h *Harness) AssertHeight(height int, opts ...Option) *Harness {
	h.tb.Helper()
	AssertHeight(h.tb, h, height, h.mergedOpts(opts...)...)
	return h
}

// RequireHeight fails the test immediately unless output has height rows.
func (h *Harness) RequireHeight(height int, opts ...Option) *Harness {
	h.tb.Helper()
	if !AssertHeight(h.tb, h, height, h.mergedOpts(opts...)...) {
		h.tb.FailNow()
	}
	return h
}

// AssertWidth reports an error unless output has width columns. It allows the
// test to continue.
func (h *Harness) AssertWidth(width int, opts ...Option) *Harness {
	h.tb.Helper()
	AssertWidth(h.tb, h, width, h.mergedOpts(opts...)...)
	return h
}

// RequireWidth fails the test immediately unless output has width columns.
func (h *Harness) RequireWidth(width int, opts ...Option) *Harness {
	h.tb.Helper()
	if !AssertWidth(h.tb, h, width, h.mergedOpts(opts...)...) {
		h.tb.FailNow()
	}
	return h
}

// AssertDimensions reports an error unless output has width columns and height
// rows. It allows the test to continue.
func (h *Harness) AssertDimensions(width, height int, opts ...Option) *Harness {
	h.tb.Helper()
	AssertDimensions(h.tb, h, width, height, h.mergedOpts(opts...)...)
	return h
}

// RequireDimensions fails the test immediately unless output has width columns
// and height rows.
func (h *Harness) RequireDimensions(width, height int, opts ...Option) *Harness {
	h.tb.Helper()
	if !AssertDimensions(h.tb, h, width, height, h.mergedOpts(opts...)...) {
		h.tb.FailNow()
	}
	return h
}

// AssertSnapshot compares the latest captured program output against a snapshot
// without waiting for the program to finish. It allows the test to continue.
func (h *Harness) AssertSnapshot(opts ...snapshot.Option) *Harness {
	h.tb.Helper()
	snapshot.AssertEqual(h.tb, h.View(), h.snapshotOpts(opts)...)
	return h
}

// RequireSnapshot compares the latest captured program output against a
// snapshot without waiting for the program to finish, failing the test
// immediately if the snapshot does not match.
func (h *Harness) RequireSnapshot(opts ...snapshot.Option) *Harness {
	h.tb.Helper()
	if !snapshot.AssertEqual(h.tb, h.View(), h.snapshotOpts(opts)...) {
		h.tb.FailNow()
	}
	return h
}

func (h *Harness) snapshotOpts(opts []snapshot.Option) []snapshot.Option {
	if !collectOptions(h.mergedOpts()...).stripANSI {
		return opts
	}
	return append([]snapshot.Option{snapshot.WithStripANSI()}, opts...)
}

// AssertSnapshotNoANSI compares the latest captured program output against a
// snapshot after stripping ANSI sequences and without waiting for the program
// to finish. It allows the test to continue.
func (h *Harness) AssertSnapshotNoANSI(opts ...snapshot.Option) *Harness {
	h.tb.Helper()
	return h.AssertSnapshot(append(opts, snapshot.WithStripANSI())...)
}

// RequireSnapshotNoANSI compares the latest captured program output against a
// snapshot after stripping ANSI sequences and without waiting for the program
// to finish, failing the test immediately if the snapshot does not match.
func (h *Harness) RequireSnapshotNoANSI(opts ...snapshot.Option) *Harness {
	h.tb.Helper()
	return h.RequireSnapshot(append(opts, snapshot.WithStripANSI())...)
}
