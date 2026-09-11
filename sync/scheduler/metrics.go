// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package scheduler

import (
	"sync"
	"sync/atomic"
	"time"
)

const maxRecentRuns = 50

// RunRecord captures details of a single cron execution.
type RunRecord struct {
	Start    time.Time     `json:"start"`
	End      time.Time     `json:"end"`
	Duration time.Duration `json:"duration"`
	Errored  bool          `json:"errored"`
	Error    string        `json:"error,omitzero"`
}

// Metrics tracks execution statistics for a [Cron].
type Metrics struct {
	mu sync.RWMutex

	// firstExecuted is the start time of the earliest recorded run.
	firstExecuted time.Time
	// lastExecuted is the end time of the most recent recorded run.
	lastExecuted time.Time

	// totalExecutions is the number of recorded runs.
	totalExecutions atomic.Int64
	// totalErrors is the number of recorded runs that returned an error.
	totalErrors atomic.Int64
	// totalDuration is the sum of all recorded run durations, in nanoseconds.
	totalDuration atomic.Int64

	// runs stores the most recent executions in a fixed-size ring buffer.
	runs [maxRecentRuns]RunRecord
	// runHead is the index of the next slot to write in runs.
	runHead int
	// runCount is the number of valid entries in runs (at most maxRecentRuns).
	runCount int

	// running indicates whether an execution is currently in progress.
	running atomic.Bool
	// runningSince is the start time of the in-progress execution.
	runningSince time.Time
}

func newMetrics() *Metrics {
	return &Metrics{}
}

// GetFirstExecuted returns the start time of the first recorded execution.
// It returns the zero [time.Time] if the cron has never executed.
func (m *Metrics) GetFirstExecuted() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.firstExecuted
}

// GetLastExecuted returns the end time of the most recent recorded execution.
// It returns the zero [time.Time] if the cron has never executed.
func (m *Metrics) GetLastExecuted() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastExecuted
}

// GetTotalExecutions returns the total number of recorded executions.
func (m *Metrics) GetTotalExecutions() int64 {
	return m.totalExecutions.Load()
}

// GetTotalErrors returns the total number of recorded executions that returned
// an error.
func (m *Metrics) GetTotalErrors() int64 {
	return m.totalErrors.Load()
}

// GetTotalExecutionDuration returns the sum of all recorded execution durations.
func (m *Metrics) GetTotalExecutionDuration() time.Duration {
	return time.Duration(m.totalDuration.Load())
}

// IsRunning reports whether an execution is currently in progress.
func (m *Metrics) IsRunning() bool {
	return m.running.Load()
}

// GetRunningSince returns the start time of the in-progress execution.
// It returns the zero [time.Time] when [Metrics.IsRunning] is false.
func (m *Metrics) GetRunningSince() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.runningSince
}

// GetRecentRuns returns up to the last 50 recorded executions, ordered from
// oldest to newest.
func (m *Metrics) GetRecentRuns() []RunRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.runCount == 0 {
		return nil
	}

	out := make([]RunRecord, m.runCount)
	start := 0
	if m.runCount == len(m.runs) {
		start = m.runHead
	}
	for i := range m.runCount {
		out[i] = m.runs[(start+i)%len(m.runs)]
	}
	return out
}

func (m *Metrics) beginRun() time.Time {
	start := time.Now()

	m.running.Store(true)
	m.mu.Lock()
	m.runningSince = start
	m.mu.Unlock()

	return start
}

func (m *Metrics) endRun(start time.Time, err error) {
	m.record(start, time.Now(), err)

	m.running.Store(false)
	m.mu.Lock()
	m.runningSince = time.Time{}
	m.mu.Unlock()
}

func (m *Metrics) record(start, end time.Time, err error) {
	dur := end.Sub(start)

	m.totalExecutions.Add(1)
	if err != nil {
		m.totalErrors.Add(1)
	}
	m.totalDuration.Add(int64(dur))

	rec := RunRecord{
		Start:    start,
		End:      end,
		Duration: dur,
		Errored:  err != nil,
	}
	if err != nil {
		rec.Error = err.Error()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.firstExecuted.IsZero() {
		m.firstExecuted = start
	}
	m.lastExecuted = end

	m.runs[m.runHead] = rec
	m.runHead = (m.runHead + 1) % len(m.runs)
	if m.runCount < len(m.runs) {
		m.runCount++
	}
}

// Snapshot returns a point-in-time copy of the tracked metrics.
func (m *Metrics) Snapshot() MetricSnapshot {
	runs := m.GetRecentRuns()
	recent := make([]ExportedRunRecord, len(runs))
	for i, r := range runs {
		recent[i] = ExportedRunRecord{
			Start:      r.Start,
			End:        r.End,
			DurationNS: int64(r.Duration),
			Errored:    r.Errored,
			Error:      r.Error,
		}
	}

	return MetricSnapshot{
		FirstExecuted:          m.GetFirstExecuted(),
		LastExecuted:           m.GetLastExecuted(),
		TotalExecutions:        m.GetTotalExecutions(),
		TotalErrors:            m.GetTotalErrors(),
		TotalExecutionDuration: int64(m.GetTotalExecutionDuration()),
		Running:                m.IsRunning(),
		RunningSince:           m.GetRunningSince(),
		RecentRuns:             recent,
	}
}

func (m *Metrics) snapshot() MetricSnapshot {
	return m.Snapshot()
}

// MetricSnapshot is a point-in-time export of [Metrics].
type MetricSnapshot struct {
	FirstExecuted          time.Time           `json:"first_executed,omitzero"`
	LastExecuted           time.Time           `json:"last_executed,omitzero"`
	TotalExecutions        int64               `json:"total_executions"`
	TotalErrors            int64               `json:"total_errors"`
	TotalExecutionDuration int64               `json:"total_execution_duration_ns"`
	Running                bool                `json:"running"`
	RunningSince           time.Time           `json:"running_since,omitzero"`
	RecentRuns             []ExportedRunRecord `json:"recent_runs"`
}

// ExportedRunRecord is a point-in-time export of a single recorded execution.
type ExportedRunRecord struct {
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	DurationNS int64     `json:"duration_ns"`
	Errored    bool      `json:"errored"`
	Error      string    `json:"error,omitzero"`
}
