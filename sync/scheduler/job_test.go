// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestJobFunc_Invoke(t *testing.T) {
	t.Parallel()

	var ran bool
	j := JobFunc(func(_ context.Context) error {
		ran = true
		return nil
	})
	if err := j.Invoke(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("job did not run")
	}
}

func TestJobLoggerFunc_Invoke(t *testing.T) {
	t.Parallel()

	l := slog.New(slog.DiscardHandler)
	ctx := withLogger(context.Background(), l)

	var got *slog.Logger
	j := JobLoggerFunc(func(ctx context.Context, log *slog.Logger) error {
		got = LoggerFromContext(ctx)
		if log != l {
			t.Fatalf("Invoke passes log = %p, want %p", log, l)
		}
		return nil
	})
	if err := j.Invoke(ctx); err != nil {
		t.Fatal(err)
	}
	if got != l {
		t.Fatalf("LoggerFromContext = %p, want %p", got, l)
	}
}

func TestLoggerFromContext_defaultWhenMissing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	if LoggerFromContext(ctx) != slog.Default() {
		t.Fatal("expected slog.Default() when no value in context")
	}
}

func TestLoggerFromContext_withValue(t *testing.T) {
	t.Parallel()

	l := slog.New(slog.DiscardHandler)
	ctx := withLogger(context.Background(), l)
	if LoggerFromContext(ctx) != l {
		t.Fatal("expected logger from context")
	}
}

func TestRun_noJobs(t *testing.T) {
	t.Parallel()

	err := Run(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestRun_contextCancel cannot use [synctest.Test]: [Run] wraps the context with
// [signal.NotifyContext], which registers runtime signal handling outside the
// synctest bubble and triggers a fatal error.
func TestRun_contextCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := Run(ctx, JobFunc(func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestRun_firstJobError(t *testing.T) {
	t.Parallel()

	want := errors.New("fail")
	var secondSeen atomic.Bool

	err := Run(context.Background(),
		JobFunc(func(context.Context) error { return want }),
		JobFunc(func(ctx context.Context) error {
			<-ctx.Done()
			secondSeen.Store(true)
			return nil
		}),
	)
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	if !secondSeen.Load() {
		t.Fatal("expected second job to observe cancellation")
	}
}

func TestRun_invalidCronSchedule(t *testing.T) {
	t.Parallel()

	c := NewCron("bad", JobFunc(func(context.Context) error { return nil })).
		WithSchedule("not valid cron")

	err := Run(context.Background(), c)
	if err == nil {
		t.Fatal("expected error from invalid cron spec")
	}
}

func TestCron_builder(t *testing.T) {
	t.Parallel()

	l := slog.New(slog.DiscardHandler)

	c := NewCron("x", JobFunc(func(context.Context) error { return nil })).
		WithInterval(2 * time.Minute).
		WithImmediate(true).
		WithExitOnError(true).
		WithLogger(l)

	if c.name != "x" {
		t.Fatalf("name = %q", c.name)
	}
	fs, ok := c.schedule.(FrequencySchedule)
	if !ok {
		t.Fatalf("schedule type = %T, want FrequencySchedule", c.schedule)
	}
	if fs.Delay != 2*time.Minute {
		t.Fatalf("schedule delay = %v", fs.Delay)
	}
	if !c.immediate || !c.exitOnError {
		t.Fatal("flags not set")
	}
	if c.logger != l {
		t.Fatal("logger not set")
	}
}

func TestCron_WithLogger_nilIgnored(t *testing.T) {
	t.Parallel()

	def := slog.Default()
	c := NewCron("x", JobFunc(func(context.Context) error { return nil })).WithLogger(nil)
	if c.logger != def {
		t.Fatal("expected default logger when nil passed")
	}
}

func TestCron_WithSchedule_valid(t *testing.T) {
	t.Parallel()

	c := NewCron("x", JobFunc(func(context.Context) error { return nil })).
		WithSchedule("0 * * * *")
	if c.validationError != nil {
		t.Fatal(c.validationError)
	}
	if _, ok := c.schedule.(*SpecSchedule); !ok {
		t.Fatalf("want *SpecSchedule, got %T", c.schedule)
	}
}

func TestCron_validate_returnsParseError(t *testing.T) {
	t.Parallel()

	c := NewCron("x", JobFunc(func(context.Context) error { return nil })).
		WithSchedule("@@@")
	if c.validationError == nil {
		t.Fatal("expected validation error")
	}
	if err := c.validate(); err == nil {
		t.Fatal("validate() should return stored error")
	}
}

func TestCron_Invoke_respectsContextCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 2500*time.Millisecond)
		defer cancel()

		runs := atomic.Int32{}
		job := JobFunc(func(context.Context) error {
			runs.Add(1)
			return nil
		})
		c := NewCron("t", job).WithImmediate(true).WithInterval(1 * time.Hour)

		err := c.Invoke(ctx)
		if err != nil {
			t.Fatalf("Invoke: %v", err)
		}
		if n := runs.Load(); n < 1 {
			t.Fatalf("runs = %d, want at least 1", n)
		}
	})
}

func TestCron_Invoke_exitOnError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		want := errors.New("boom")
		ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
		defer cancel()

		job := JobFunc(func(context.Context) error { return want })
		c := NewCron("t", job).WithImmediate(true).WithExitOnError(true).WithInterval(24 * time.Hour)

		err := c.Invoke(ctx)
		if !errors.Is(err, want) {
			t.Fatalf("err = %v, want %v", err, want)
		}
	})
}
