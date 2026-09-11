// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package scheduler

import (
	"encoding/json/v2"
	"iter"
)

const (
	// ExportedMetricsKindCron identifies cron job exports.
	ExportedMetricsKindCron = "cron"
	// ExportedMetricsKindJob identifies non-cron job exports.
	ExportedMetricsKindJob = "job"
)

// MetricCollector marshals scheduler job metrics to JSON.
type MetricCollector struct {
	jobs []Job
}

// NewMetricCollector returns a collector for the provided jobs.
func NewMetricCollector(jobs ...Job) *MetricCollector {
	return &MetricCollector{jobs: append([]Job(nil), jobs...)}
}

// ExportIter yields a point-in-time snapshot for each collected job. Each
// yielded [*ExportedMetrics] is independent and safe to retain after iteration.
func (mc *MetricCollector) ExportIter() iter.Seq[*ExportedMetrics] {
	return func(yield func(*ExportedMetrics) bool) {
		for _, job := range mc.jobs {
			if !yield(exportMetricsForJob(job)) {
				return
			}
		}
	}
}

// MarshalJSON marshals all collected jobs using [encoding/json/v2].
func (mc *MetricCollector) MarshalJSON() ([]byte, error) {
	entries := make([]*ExportedMetrics, 0, len(mc.jobs))
	for entry := range mc.ExportIter() {
		entries = append(entries, entry)
	}
	return json.Marshal(entries)
}

// ExportedMetrics is a point-in-time export of a collected job. Cron-specific
// fields are populated only when [ExportedMetrics.Kind] is [ExportedMetricsKindCron].
type ExportedMetrics struct {
	Kind            string          `json:"kind"`
	Name            string          `json:"name,omitempty"`
	Schedule        string          `json:"schedule,omitempty"`
	Immediate       *bool           `json:"immediate,omitempty"`
	ExitOnError     *bool           `json:"exit_on_error,omitempty"`
	Enabled         *bool           `json:"enabled,omitempty"`
	ValidationError string          `json:"validation_error,omitzero"`
	Metrics         *MetricSnapshot `json:"metrics,omitempty"`
}

func exportMetricsForJob(job Job) *ExportedMetrics {
	cron, ok := job.(*Cron)
	if !ok {
		return &ExportedMetrics{Kind: ExportedMetricsKindJob}
	}
	return cron.exportMetrics()
}

func (c *Cron) exportMetrics() *ExportedMetrics {
	immediate := c.immediate
	exitOnError := c.exitOnError
	enabled := c.enabled

	entry := &ExportedMetrics{
		Kind:        ExportedMetricsKindCron,
		Name:        c.name,
		Immediate:   &immediate,
		ExitOnError: &exitOnError,
		Enabled:     &enabled,
	}
	if c.schedule != nil {
		entry.Schedule = c.schedule.String()
	}
	if c.validationError != nil {
		entry.ValidationError = c.validationError.Error()
	}
	if c.metrics != nil {
		snapshot := c.metrics.snapshot()
		entry.Metrics = &snapshot
	}
	return entry
}
