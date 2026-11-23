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
	"reflect"
	"slices"
	"syscall"
	"time"

	"github.com/lrstanley/x/sync/conc"
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

// Run invokes all jobs concurrently, and listens for termination signals
// (SIGINT and SIGTERM). Once the first signal is received, Run stops listening,
// so a second signal terminates the process using the default behavior (useful
// when a job hangs during shutdown). SIGQUIT is intentionally not handled, so
// Go's default behavior of dumping all goroutine stacks and exiting with a
// non-zero status is preserved.
//
// If any jobs return an error, all jobs will terminate (assuming they listen to
// the provided context), and the first known error will be returned. We will wait
// for all jobs to finish before returning.
//
// A shutdown triggered by a termination signal or by cancellation of the parent
// context is treated as a clean exit: any job error matching that cancellation
// (e.g. a job returning ctx.Err()) is discarded, and Run returns nil. Errors
// returned before such a shutdown, including [context.Canceled], are still
// returned. The parent context's deadline expiring is not a clean exit, so
// jobs returning [context.DeadlineExceeded] in that case are still returned.
func Run(ctx context.Context, jobs ...Job) error {
	if len(jobs) == 0 {
		return errors.New("no jobs provided")
	}

	// signal.Notify is used rather than signal.NotifyContext, so that on a
	// signal, signal handling is stopped before jobs observe the cancellation.
	// On parent cancellation, it is stopped shortly after. Any later signal
	// terminates the process using the default behavior, which is what you want
	// when a job hangs during shutdown.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)

	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	go func() {
		var cause error
		select {
		case sig := <-sigs:
			cause = fmt.Errorf("%s signal received: %w", sig, context.Canceled)
		case <-ctx.Done():
		}
		signal.Stop(sigs)
		if cause != nil {
			cancel(cause)
		}
	}()

	eg := conc.NewGroup().
		WithContext(ctx).
		WithCancelOnError().
		WithFirstError()

	for _, runner := range jobs {
		if c, ok := runner.(*Cron); ok {
			if err := c.validate(); err != nil {
				return fmt.Errorf("cron job has invalid spec %qs: %w", c.name, err)
			}
		}
		eg.Go(func(gctx context.Context) error {
			err := runner.Invoke(gctx)
			if err != nil && isShutdownErr(ctx, err) {
				return nil
			}
			return err
		})
	}

	return eg.Wait()
}

// isShutdownErr reports whether err is the result of ctx (the signal-aware
// context created by [Run]) being canceled, i.e. a signal was received or the
// parent context was canceled. A deadline expiring is not a shutdown. It must be
// checked when the job returns, so that an error returned before shutdown is not
// mistaken for one caused by it.
func isShutdownErr(ctx context.Context, err error) bool {
	cerr := ctx.Err()
	if !errors.Is(cerr, context.Canceled) {
		return false
	}
	return onlyMatches(err, cerr, context.Cause(ctx))
}

// onlyMatches reports whether every error in err's tree matches one of targets,
// so that a joined error such as [errors.Join](ctx.Err(), flushErr) is not
// mistaken for a clean shutdown. Each error is compared for equality before it
// is unwrapped, so a target that itself wraps other errors (e.g. a parent's
// cancellation cause) still matches. Custom Is methods are only consulted on
// errors that wrap nothing, so they cannot hide a wrapped failure.
func onlyMatches(err error, targets ...error) bool {
	for _, t := range targets {
		if reflect.ValueOf(t).Comparable() && err == t { //nolint:errorlint // intentional.
			return true
		}
	}
	switch x := err.(type) { //nolint:errorlint // walking the tree manually.
	case interface{ Unwrap() []error }:
		errs := x.Unwrap()
		for _, e := range errs {
			if !onlyMatches(e, targets...) {
				return false
			}
		}
		return len(errs) > 0
	case interface{ Unwrap() error }:
		if inner := x.Unwrap(); inner != nil {
			return onlyMatches(inner, targets...)
		}
	}
	// err wraps nothing, so only its own Is method is left to check. errors.Is
	// is not used, as its equality check can panic on uncomparable values.
	x, ok := err.(interface{ Is(error) bool }) //nolint:errorlint // intentional.
	return ok && slices.ContainsFunc(targets, x.Is)
}

var _ Job = (*Cron)(nil)

type Cron struct {
	name            string
	schedule        Schedule
	immediate       bool
	exitOnError     bool
	enabled         bool
	enabledFunc     func() bool
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
		enabled:  true,
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

// WithEnabled sets whether the cron job is enabled. This defaults to true. When
// false, scheduled invocations are skipped.
func (c *Cron) WithEnabled(enabled bool) *Cron {
	c.enabled = enabled
	return c
}

// WithEnabledFunc sets a function that is called before each invocation to
// determine whether the cron job should run. When the function returns false,
// the invocation is skipped. This is evaluated in addition to [Cron.WithEnabled].
func (c *Cron) WithEnabledFunc(fn func() bool) *Cron {
	c.enabledFunc = fn
	return c
}

func (c *Cron) shouldInvoke() bool {
	if !c.enabled {
		return false
	}
	if c.enabledFunc != nil && !c.enabledFunc() {
		return false
	}
	return true
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

		if c.shouldInvoke() {
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
			if !c.shouldInvoke() {
				continue
			}

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
