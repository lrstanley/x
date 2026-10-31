// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/steep"
)

// TestInspectorSamples records the sample casts reel's inspector
// (term/reel/inspector) offers, into the directory REEL_INSPECTOR_SAMPLES names.
// It is skipped without it. One sample is of a failing test, so its
// failures are kept from failing this one.
func TestInspectorSamples(t *testing.T) {
	dir := os.Getenv("REEL_INSPECTOR_SAMPLES")
	if dir == "" {
		t.Skip("REEL_INSPECTOR_SAMPLES is not set")
	}
	opts := func(file string) []steep.Option {
		return []steep.Option{
			steep.WithIgnoreEnv(true),
			steep.WithSource(true),
			steep.WithRecordMessages(true),
			steep.WithExportAlways(),
			steep.WithArtifacts(dir),
			steep.WithRecording(reel.Profile{Outputs: reel.MustFiles(file)}),
		}
	}

	t.Run("table-navigation", func(t *testing.T) {
		steep.NewHarness(t, newRankingModel(), opts("table-navigation.cast")...).
			Wait("Tokyo").
			KeyDown().
			KeyDown().
			KeyEsc().
			Assert("Shanghai").
			KeyEsc().
			KeyDown()
	})

	t.Run("failing-wait", func(t *testing.T) {
		tb := &expectFailure{TB: t}
		func() {
			defer tb.recover()
			steep.NewHarness(tb, newRankingModel(), append(opts("failing-wait.cast"), steep.WithTimeout(500*time.Millisecond))...).
				Wait("Tokyo").
				Wait("Atlantis")
		}()
		if !tb.Failed() {
			t.Error("the failing sample passed")
		}
	})
}

// expectFailure is a test that is meant to fail: it logs its failures
// rather than failing the test it wraps. FailNow panics, for recover to
// stop, so the harness runs on the test's goroutine, and sources recorded
// name the test's lines.
type expectFailure struct {
	testing.TB

	mu     sync.Mutex
	failed bool
}

func (e *expectFailure) Fail() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.failed = true
}

func (e *expectFailure) Failed() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.failed
}

// failedNow is what FailNow panics with.
type failedNow struct{}

func (e *expectFailure) FailNow() {
	e.Fail()
	panic(failedNow{})
}

// recover stops the panic of FailNow, and passes any other on.
func (e *expectFailure) recover() {
	if v := recover(); v != nil {
		if _, ok := v.(failedNow); !ok {
			panic(v)
		}
	}
}

func (e *expectFailure) Error(args ...any) {
	e.Log(fmt.Sprint(args...))
	e.Fail()
}

func (e *expectFailure) Errorf(format string, args ...any) {
	e.Logf(format, args...)
	e.Fail()
}

func (e *expectFailure) Fatal(args ...any) {
	e.Log(fmt.Sprint(args...))
	e.FailNow()
}

func (e *expectFailure) Fatalf(format string, args ...any) {
	e.Logf(format, args...)
	e.FailNow()
}
