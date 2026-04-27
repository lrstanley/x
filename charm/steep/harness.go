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
	program  *tea.Program
	observer *observer
	output   *bytes.Buffer

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
		observer: newObserver(model),
		output:   &bytes.Buffer{},
		done:     make(chan runResult, 1),
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
		h.WaitFinished(tb)
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

	m := &componentWrapper[M]{model: model}

	m.validate(tb)

	return NewHarness(tb, m, opts...)
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
func (h *Harness) FinalOutput(tb testing.TB, opts ...Option) io.Reader {
	tb.Helper()
	h.WaitFinished(tb, opts...)
	return h.Output()
}

// Output returns the last captured output.
func (h *Harness) Output() io.Reader {
	return h.output
}

// FinalView waits for the Bubble Tea program to finish and returns the last
// captured view content.
func (h *Harness) FinalView(tb testing.TB, opts ...Option) string {
	tb.Helper()
	h.WaitFinished(tb, opts...)
	h.observer.mu.RLock()
	defer h.observer.mu.RUnlock()
	return h.observer.lastViewSnapshot
}

// FinalModel waits for the Bubble Tea program to finish and returns the latest
// underlying root model.
func (h *Harness) FinalModel(tb testing.TB, opts ...Option) tea.Model {
	tb.Helper()
	h.WaitFinished(tb, opts...)
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
func (h *Harness) WaitFinished(tb testing.TB, opts ...Option) {
	tb.Helper()

	cfg := collectOptions(opts...)
	h.resultMu.RLock()
	if result := h.result; result != nil {
		h.resultMu.RUnlock()
		if result.err != nil && !errors.Is(result.err, tea.ErrProgramKilled) {
			tb.Fatalf("bubble tea program failed: %v", result.err)
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
			tb.Fatalf("bubble tea program failed: %v", result.err)
		}
		if wrapper, ok := result.model.(*observer); ok {
			h.observer.replace(wrapper.currentModel())
		}
	case <-timer.C:
		tb.Fatalf("timeout waiting for bubble tea program to finish after %s", cfg.timeout)
	}
}

// WaitSettleMessages waits until no messages have been observed for the
// configured settle timeout. See also [WithSettleTimeout], [WithCheckInterval],
// and [WithTimeout].
func (h *Harness) WaitSettleMessages(tb testing.TB, opts ...Option) *Harness {
	tb.Helper()

	cfg := collectOptions(opts...)
	h.observer.setSettleIgnore(cfg.settleIgnore)
	defer h.observer.setSettleIgnore(nil)

	deadline := time.Now().Add(cfg.timeout)
	ctx := tb.Context()
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
			tb.Fatalf(
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
			tb.Fatalf("wait for Update() to settle canceled: %v", ctx.Err())
		}
	}
}

// WaitSettleView waits until the rendered view string has not changed for the
// configured settle timeout. It polls this harness's [Harness.View] and compares
// each result to the previous sample. See also [WithSettleTimeout],
// [WithCheckInterval], and [WithTimeout].
func (h *Harness) WaitSettleView(tb testing.TB, opts ...Option) *Harness {
	tb.Helper()
	WaitSettleView(tb, h, opts...)
	return h
}

// WaitForBytes waits until condition returns true for the latest view output.
func (h *Harness) WaitForBytes(tb testing.TB, condition func(view []byte) bool, opts ...Option) []byte {
	tb.Helper()
	return WaitFor(tb, h, condition, opts...)
}

// WaitForString waits until condition returns true for the latest view output.
func (h *Harness) WaitForString(tb testing.TB, condition func(view string) bool, opts ...Option) string {
	tb.Helper()
	return WaitFor(tb, h, condition, opts...)
}

// WaitContainsBytes waits until output contains contents.
func (h *Harness) WaitContainsBytes(tb testing.TB, contents []byte, opts ...Option) []byte {
	tb.Helper()
	return WaitContainsBytes(tb, h, contents, opts...)
}

// WaitContainsString waits until output contains contents.
func (h *Harness) WaitContainsString(tb testing.TB, contents string, opts ...Option) string {
	tb.Helper()
	return WaitContainsString(tb, h, contents, opts...)
}

// WaitContainsStrings waits until output contains all contents.
func (h *Harness) WaitContainsStrings(tb testing.TB, contents []string, opts ...Option) string {
	tb.Helper()
	return WaitContainsStrings(tb, h, contents, opts...)
}

// WaitNotContainsBytes waits until output contains none of the contents.
func (h *Harness) WaitNotContainsBytes(tb testing.TB, contents []byte, opts ...Option) []byte {
	tb.Helper()
	return WaitNotContainsBytes(tb, h, contents, opts...)
}

// WaitNotContainsString waits until output contains none of the contents.
func (h *Harness) WaitNotContainsString(tb testing.TB, contents string, opts ...Option) string {
	tb.Helper()
	return WaitNotContainsString(tb, h, contents, opts...)
}

// WaitNotContainsStrings waits until output contains none of the contents.
func (h *Harness) WaitNotContainsStrings(tb testing.TB, contents []string, opts ...Option) string {
	tb.Helper()
	return WaitNotContainsStrings(tb, h, contents, opts...)
}

// AssertStringContains reports an error unless all substrings appear in output.
// It allows the test to continue.
func (h *Harness) AssertStringContains(tb testing.TB, substr ...string) *Harness {
	tb.Helper()
	AssertStringContains(tb, h, substr...)
	return h
}

// RequireStringContains fails the test immediately unless all substrings appear
// in output.
func (h *Harness) RequireStringContains(tb testing.TB, substr ...string) *Harness {
	tb.Helper()
	if !AssertStringContains(tb, h, substr...) {
		tb.FailNow()
	}
	return h
}

// AssertStringNotContains reports an error if any substring appears in output.
// It allows the test to continue.
func (h *Harness) AssertStringNotContains(tb testing.TB, substr ...string) *Harness {
	tb.Helper()
	AssertStringNotContains(tb, h, substr...)
	return h
}

// RequireStringNotContains fails the test immediately if any substring appears
// in output.
func (h *Harness) RequireStringNotContains(tb testing.TB, substr ...string) *Harness {
	tb.Helper()
	if !AssertStringNotContains(tb, h, substr...) {
		tb.FailNow()
	}
	return h
}

// AssertHeight reports an error unless output has height rows. It allows the
// test to continue.
func (h *Harness) AssertHeight(tb testing.TB, height int) *Harness {
	tb.Helper()
	AssertHeight(tb, h, height)
	return h
}

// RequireHeight fails the test immediately unless output has height rows.
func (h *Harness) RequireHeight(tb testing.TB, height int) *Harness {
	tb.Helper()
	if !AssertHeight(tb, h, height) {
		tb.FailNow()
	}
	return h
}

// AssertWidth reports an error unless output has width columns. It allows the
// test to continue.
func (h *Harness) AssertWidth(tb testing.TB, width int) *Harness {
	tb.Helper()
	AssertWidth(tb, h, width)
	return h
}

// RequireWidth fails the test immediately unless output has width columns.
func (h *Harness) RequireWidth(tb testing.TB, width int) *Harness {
	tb.Helper()
	if !AssertWidth(tb, h, width) {
		tb.FailNow()
	}
	return h
}

// AssertDimensions reports an error unless output has width columns and height
// rows. It allows the test to continue.
func (h *Harness) AssertDimensions(tb testing.TB, width, height int) *Harness {
	tb.Helper()
	AssertDimensions(tb, h, width, height)
	return h
}

// RequireDimensions fails the test immediately unless output has width columns
// and height rows.
func (h *Harness) RequireDimensions(tb testing.TB, width, height int) *Harness {
	tb.Helper()
	if !AssertDimensions(tb, h, width, height) {
		tb.FailNow()
	}
	return h
}

// AssertSnapshot compares the latest captured program output against a snapshot
// without waiting for the program to finish. It allows the test to continue.
func (h *Harness) AssertSnapshot(tb testing.TB, opts ...snapshot.Option) *Harness {
	tb.Helper()
	snapshot.AssertEqual(tb, h.View(), opts...)
	return h
}

// RequireSnapshot compares the latest captured program output against a
// snapshot without waiting for the program to finish, failing the test
// immediately if the snapshot does not match.
func (h *Harness) RequireSnapshot(tb testing.TB, opts ...snapshot.Option) *Harness {
	tb.Helper()
	if !snapshot.AssertEqual(tb, h.View(), opts...) {
		tb.FailNow()
	}
	return h
}

// AssertSnapshotNoANSI compares the latest captured program output against a
// snapshot after stripping ANSI sequences and without waiting for the program
// to finish. It allows the test to continue.
func (h *Harness) AssertSnapshotNoANSI(tb testing.TB, opts ...snapshot.Option) *Harness {
	tb.Helper()
	return h.AssertSnapshot(tb, append(opts, snapshot.WithStripANSI())...)
}

// RequireSnapshotNoANSI compares the latest captured program output against a
// snapshot after stripping ANSI sequences and without waiting for the program
// to finish, failing the test immediately if the snapshot does not match.
func (h *Harness) RequireSnapshotNoANSI(tb testing.TB, opts ...snapshot.Option) *Harness {
	tb.Helper()
	return h.RequireSnapshot(tb, append(opts, snapshot.WithStripANSI())...)
}
