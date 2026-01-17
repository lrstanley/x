// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Job is a generic runnable entity. See also [JobFunc].
type Job interface {
	Invoke(ctx context.Context) error
}

var _ Job = (*JobFunc)(nil)

// JobFunc is a function that can be used to create a [Job].
type JobFunc func(ctx context.Context) error

func (r JobFunc) Invoke(ctx context.Context) error {
	return r(ctx)
}

// JobLoggerFunc is a function that can be used to create a [Job] that has a logger,
// in addition to the context.
type JobLoggerFunc func(ctx context.Context, l *slog.Logger) error

func (f JobLoggerFunc) Invoke(ctx context.Context) error {
	return f(ctx, LoggerFromContext(ctx))
}

// Run invokes all jobs concurrently, and listens for any termination signals
// (SIGINT, SIGTERM, SIGQUIT, etc).
//
// If any jobs return an error, all jobs will terminate (assuming they listen to
// the provided context), and the first known error will be returned. We will wait
// for all jobs to finish before returning.
func Run(ctx context.Context, jobs ...Job) error {
	if len(jobs) == 0 {
		return errors.New("no jobs provided")
	}

	ctx, cancel := signal.NotifyContext(
		ctx,
		os.Interrupt,
		syscall.SIGTERM,
		syscall.SIGQUIT,
	)
	defer cancel()

	var g *errorGroup
	g, ctx = errorPoolWithContext(ctx)

	for _, runner := range jobs {
		if c, ok := runner.(*Cron); ok {
			if err := c.validate(); err != nil {
				return fmt.Errorf("cron job has invalid spec %qs: %w", c.name, err)
			}
		}
		g.run(func() error {
			return runner.Invoke(ctx)
		})
	}

	return g.wait()
}

var _ Job = (*Cron)(nil)

type Cron struct {
	name            string
	schedule        Schedule
	immediate       bool
	exitOnError     bool
	job             Job
	logger          *slog.Logger
	validationError error
}

// NewCron creates a new cron job with the provided name and underlying job. The
// cron job will run the job at the provided interval, and will exit on error if
// the [Cron.WithExitOnError] flag is set. The default interval is 5 minutes,
// which can be changed with [Cron.WithInterval] (for standard [time.Duration]'s),
// or [Cron.WithSchedule] (for crontab-style schedules).
func NewCron(name string, job Job) *Cron {
	return &Cron{
		name:     name,
		job:      job,
		schedule: Every(5 * time.Minute),
		logger:   slog.Default(),
	}
}

func (c *Cron) validate() error {
	return c.validationError
}

// WithInterval sets the interval at which the cron job will run the underlying
// job. Defaults to 5 minutes, and cannot be less than 1 second.
func (c *Cron) WithInterval(interval time.Duration) *Cron {
	c.schedule = Every(interval)
	return c
}

// WithSchedule sets the schedule at which the cron job will run the underlying
// job. It supports standard crontab-style schedules (e.g. "0 5 * * *") as well
// as "@every 1h30m", "@hourly", "@daily", "@midnight", "@weekly", "@monthly",
// "@yearly", and "@annually".
func (c *Cron) WithSchedule(schedule string) *Cron {
	var err error
	c.schedule, err = Parse(schedule)
	if err != nil {
		c.validationError = fmt.Errorf("failed to parse schedule %s: %w", schedule, err)
		return c
	}
	return c
}

// WithImmediate sets whether the cron job should run the underlying job
// immediately upon creation. This defaults to false. If true, the job will also
// exit on error if the initial immediate run fails.
func (c *Cron) WithImmediate(enabled bool) *Cron {
	c.immediate = enabled
	return c
}

// WithExitOnError sets whether the cron job should exit on error. This defaults
// to false. If true, the job will exit if the underlying job returns an error.
func (c *Cron) WithExitOnError(enabled bool) *Cron {
	c.exitOnError = enabled
	return c
}

// WithLogger sets the logger for the cron job. This defaults to the default
// logger. You can obtain the logger from the context via [LoggerFromContext].
func (c *Cron) WithLogger(logger *slog.Logger) *Cron {
	if logger != nil {
		c.logger = logger
	}
	return c
}

// Invoke runs the cron job. This is typically not called directly, but rather
// via [Run].
func (c *Cron) Invoke(ctx context.Context) error {
	l := c.logger.With(
		"cron", c.name,
		"schedule", c.schedule.String(),
		"exit_on_error", c.exitOnError,
	)

	var lastRun time.Time

	if c.immediate {
		// Jitter the first run by 0-2 seconds.
		time.Sleep(time.Duration(rand.IntN(2)) * time.Second) //nolint:gosec

		lastRun = time.Now()
		l.InfoContext(ctx, "invoking cron")
		if err := c.job.Invoke(withLogger(ctx, l)); err != nil {
			l.ErrorContext(
				ctx,
				"cron failed",
				"error", err,
				"duration", time.Since(lastRun),
			)
			return err
		}
		l.InfoContext(
			ctx,
			"cron complete",
			"duration", time.Since(lastRun),
		)
	}

	var next time.Time

	for {
		time.Sleep(1 * time.Second)
		next = c.schedule.Next(time.Now())

		l.DebugContext(ctx, "waiting for next cron", "next", time.Until(next).Round(time.Second))
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Until(next)):

			lastRun = time.Now()
			l.InfoContext(ctx, "invoking cron")
			if err := c.job.Invoke(withLogger(ctx, l)); err != nil {
				l.ErrorContext(
					ctx,
					"cron failed",
					"error", err,
					"duration", time.Since(lastRun),
				)
				if c.exitOnError {
					return err
				}
			}
			l.InfoContext(
				ctx,
				"cron complete",
				"duration", time.Since(lastRun),
			)
		}
	}
}
