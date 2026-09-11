// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package scheduler

import (
	"context"
	"encoding/json/v2"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func TestMetrics_recordAndQuery(t *testing.T) {
	t.Parallel()

	m := newMetrics()
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	end := start.Add(250 * time.Millisecond)

	m.record(start, end, nil)

	if got := m.GetFirstExecuted(); !got.Equal(start) {
		t.Fatalf("GetFirstExecuted = %v, want %v", got, start)
	}
	if got := m.GetLastExecuted(); !got.Equal(end) {
		t.Fatalf("GetLastExecuted = %v, want %v", got, end)
	}
	if got := m.GetTotalExecutions(); got != 1 {
		t.Fatalf("GetTotalExecutions = %d, want 1", got)
	}
	if got := m.GetTotalErrors(); got != 0 {
		t.Fatalf("GetTotalErrors = %d, want 0", got)
	}
	if got := m.GetTotalExecutionDuration(); got != 250*time.Millisecond {
		t.Fatalf("GetTotalExecutionDuration = %v, want 250ms", got)
	}

	runs := m.GetRecentRuns()
	if len(runs) != 1 {
		t.Fatalf("GetRecentRuns len = %d, want 1", len(runs))
	}
	if runs[0].Start != start || runs[0].End != end || runs[0].Errored {
		t.Fatalf("unexpected run record: %+v", runs[0])
	}
}

func TestMetrics_recordError(t *testing.T) {
	t.Parallel()

	m := newMetrics()
	want := errors.New("boom")
	start := time.Now()
	end := start.Add(time.Second)

	m.record(start, end, want)

	if got := m.GetTotalErrors(); got != 1 {
		t.Fatalf("GetTotalErrors = %d, want 1", got)
	}
	runs := m.GetRecentRuns()
	if len(runs) != 1 || !runs[0].Errored || runs[0].Error != want.Error() {
		t.Fatalf("unexpected run record: %+v", runs[0])
	}
}

func TestMetrics_recentRunsRingBuffer(t *testing.T) {
	t.Parallel()

	m := newMetrics()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	for i := range maxRecentRuns + 5 {
		runStart := start.Add(time.Duration(i) * time.Minute)
		m.record(runStart, runStart.Add(time.Second), nil)
	}

	if got := m.GetTotalExecutions(); got != int64(maxRecentRuns+5) {
		t.Fatalf("GetTotalExecutions = %d, want %d", got, maxRecentRuns+5)
	}

	runs := m.GetRecentRuns()
	if len(runs) != maxRecentRuns {
		t.Fatalf("GetRecentRuns len = %d, want %d", len(runs), maxRecentRuns)
	}
	if !runs[0].Start.Equal(start.Add(5 * time.Minute)) {
		t.Fatalf("oldest run start = %v, want %v", runs[0].Start, start.Add(5*time.Minute))
	}
	if !runs[len(runs)-1].Start.Equal(start.Add(time.Duration(maxRecentRuns+4) * time.Minute)) {
		t.Fatalf("newest run start = %v", runs[len(runs)-1].Start)
	}
}

func TestMetrics_concurrent(t *testing.T) {
	t.Parallel()

	m := newMetrics()
	const workers = 32
	const perWorker = 100

	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for i := range perWorker {
				start := time.Unix(int64(i), 0).UTC()
				m.record(start, start.Add(time.Millisecond), nil)
			}
		}()
	}
	wg.Wait()

	want := int64(workers * perWorker)
	if got := m.GetTotalExecutions(); got != want {
		t.Fatalf("GetTotalExecutions = %d, want %d", got, want)
	}
	if got := len(m.GetRecentRuns()); got != maxRecentRuns {
		t.Fatalf("GetRecentRuns len = %d, want %d", got, maxRecentRuns)
	}
}

func TestMetrics_running(t *testing.T) {
	t.Parallel()

	m := newMetrics()
	if m.IsRunning() {
		t.Fatal("expected not running initially")
	}
	if !m.GetRunningSince().IsZero() {
		t.Fatal("expected zero running since initially")
	}

	start := m.beginRun()
	if !m.IsRunning() {
		t.Fatal("expected running after beginRun")
	}
	if got := m.GetRunningSince(); !got.Equal(start) {
		t.Fatalf("GetRunningSince = %v, want %v", got, start)
	}

	m.endRun(start, nil)

	if m.IsRunning() {
		t.Fatal("expected not running after endRun")
	}
	if !m.GetRunningSince().IsZero() {
		t.Fatal("expected zero running since after endRun")
	}
	if got := m.GetTotalExecutions(); got != 1 {
		t.Fatalf("GetTotalExecutions = %d, want 1", got)
	}
	if got := m.GetLastExecuted(); got.Before(start) {
		t.Fatalf("GetLastExecuted = %v, want after %v", got, start)
	}
}

func TestCron_GetMetrics_runningDuringExecution(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		running := make(chan struct{})
		release := make(chan struct{})

		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()

		c := NewCron("running", JobFunc(func(context.Context) error {
			close(running)
			<-release
			return nil
		})).WithImmediate(true).WithInterval(24 * time.Hour)

		errCh := make(chan error, 1)
		go func() {
			errCh <- c.Invoke(ctx)
		}()

		<-running
		m := c.GetMetrics()
		if !m.IsRunning() {
			t.Fatal("expected running during job execution")
		}
		if m.GetRunningSince().IsZero() {
			t.Fatal("expected running since during job execution")
		}

		close(release)
		if err := <-errCh; err != nil {
			t.Fatalf("Invoke: %v", err)
		}
		if c.GetMetrics().IsRunning() {
			t.Fatal("expected not running after job completes")
		}
	})
}

func TestMetricCollector_MarshalJSON_running(t *testing.T) {
	t.Parallel()

	c := NewCron("job-a", JobFunc(func(context.Context) error { return nil }))
	start := time.Now()
	c.metrics.beginRun()
	defer c.metrics.endRun(start, nil)

	data, err := json.Marshal(NewMetricCollector(c))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var entries []map[string]any
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	metrics, ok := entries[0]["metrics"].(map[string]any)
	if !ok {
		t.Fatalf("metrics type = %T", entries[0]["metrics"])
	}
	if metrics["running"] != true {
		t.Fatalf("running = %v, want true", metrics["running"])
	}
	if metrics["running_since"] == nil {
		t.Fatal("expected running_since")
	}
}

func TestCron_GetMetrics_recordsExecutions(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 2500*time.Millisecond)
		defer cancel()

		c := NewCron("metrics", JobFunc(func(context.Context) error { return nil })).
			WithImmediate(true).
			WithInterval(1 * time.Hour)

		if err := c.Invoke(ctx); err != nil {
			t.Fatalf("Invoke: %v", err)
		}

		m := c.GetMetrics()
		if got := m.GetTotalExecutions(); got < 1 {
			t.Fatalf("GetTotalExecutions = %d, want at least 1", got)
		}
		if m.GetFirstExecuted().IsZero() || m.GetLastExecuted().IsZero() {
			t.Fatal("expected first and last execution times")
		}
	})
}

func TestCron_GetMetrics_recordsErrors(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		want := errors.New("fail")
		ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
		defer cancel()

		c := NewCron("metrics", JobFunc(func(context.Context) error { return want })).
			WithImmediate(true).
			WithExitOnError(true).
			WithInterval(24 * time.Hour)

		if err := c.Invoke(ctx); !errors.Is(err, want) {
			t.Fatalf("Invoke: %v", err)
		}

		m := c.GetMetrics()
		if got := m.GetTotalExecutions(); got != 1 {
			t.Fatalf("GetTotalExecutions = %d, want 1", got)
		}
		if got := m.GetTotalErrors(); got != 1 {
			t.Fatalf("GetTotalErrors = %d, want 1", got)
		}
		runs := m.GetRecentRuns()
		if len(runs) != 1 || !runs[0].Errored || runs[0].Error != want.Error() {
			t.Fatalf("unexpected run record: %+v", runs[0])
		}
	})
}

func TestCron_GetMetrics_skipsDisabledInvocations(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 2500*time.Millisecond)
		defer cancel()

		c := NewCron("metrics", JobFunc(func(context.Context) error { return nil })).
			WithImmediate(true).
			WithEnabled(false).
			WithInterval(1 * time.Hour)

		if err := c.Invoke(ctx); err != nil {
			t.Fatalf("Invoke: %v", err)
		}
		if got := c.GetMetrics().GetTotalExecutions(); got != 0 {
			t.Fatalf("GetTotalExecutions = %d, want 0", got)
		}
	})
}

func TestMetricCollector_MarshalJSON_cron(t *testing.T) {
	t.Parallel()

	c := NewCron("job-a", JobFunc(func(context.Context) error { return nil })).
		WithSchedule("0 * * * *").
		WithImmediate(true).
		WithExitOnError(true).
		WithEnabled(false)

	start := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	end := start.Add(100 * time.Millisecond)
	c.metrics.record(start, end, errors.New("boom"))

	collector := NewMetricCollector(c, JobFunc(func(context.Context) error { return nil }))
	data, err := json.Marshal(collector)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var entries []map[string]any
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries len = %d, want 2", len(entries))
	}

	first := entries[0]
	if first["kind"] != ExportedMetricsKindCron {
		t.Fatalf("kind = %v, want %q", first["kind"], ExportedMetricsKindCron)
	}
	if first["name"] != "job-a" {
		t.Fatalf("name = %v", first["name"])
	}
	if first["schedule"] != "0 * * * *" {
		t.Fatalf("schedule = %v", first["schedule"])
	}
	if first["immediate"] != true {
		t.Fatalf("immediate = %v", first["immediate"])
	}
	if first["exit_on_error"] != true {
		t.Fatalf("exit_on_error = %v", first["exit_on_error"])
	}
	if first["enabled"] != false {
		t.Fatalf("enabled = %v", first["enabled"])
	}

	metrics, ok := first["metrics"].(map[string]any)
	if !ok {
		t.Fatalf("metrics type = %T", first["metrics"])
	}
	if metrics["total_executions"] != float64(1) {
		t.Fatalf("total_executions = %v", metrics["total_executions"])
	}
	if metrics["total_errors"] != float64(1) {
		t.Fatalf("total_errors = %v", metrics["total_errors"])
	}

	if entries[1]["kind"] != ExportedMetricsKindJob {
		t.Fatalf("non-cron kind = %v, want %q", entries[1]["kind"], ExportedMetricsKindJob)
	}
}

func TestMetricCollector_ExportIter(t *testing.T) {
	t.Parallel()

	c := NewCron("job-a", JobFunc(func(context.Context) error { return nil })).
		WithSchedule("0 * * * *")

	collector := NewMetricCollector(c, JobFunc(func(context.Context) error { return nil }))

	var entries []*ExportedMetrics
	for entry := range collector.ExportIter() {
		entries = append(entries, entry)
	}
	if len(entries) != 2 {
		t.Fatalf("entries len = %d, want 2", len(entries))
	}
	if entries[0].Kind != ExportedMetricsKindCron {
		t.Fatalf("kind = %q, want %q", entries[0].Kind, ExportedMetricsKindCron)
	}
	if entries[0].Name != "job-a" {
		t.Fatalf("name = %q", entries[0].Name)
	}
	if entries[0].Metrics == nil {
		t.Fatal("expected metrics snapshot")
	}
	if entries[1].Kind != ExportedMetricsKindJob {
		t.Fatalf("kind = %q, want %q", entries[1].Kind, ExportedMetricsKindJob)
	}

	entries[0].Name = "mutated"
	for entry := range collector.ExportIter() {
		if entry.Name == "mutated" {
			t.Fatal("expected independent export snapshots")
		}
		break
	}
}

func TestMetricCollector_MarshalJSON_validationError(t *testing.T) {
	t.Parallel()

	c := NewCron("bad", JobFunc(func(context.Context) error { return nil })).
		WithSchedule("@@@")

	data, err := json.Marshal(NewMetricCollector(c))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var entries []map[string]any
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if entries[0]["validation_error"] == nil {
		t.Fatal("expected validation_error")
	}
}
