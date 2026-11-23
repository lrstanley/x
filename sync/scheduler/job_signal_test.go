// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build unix

package scheduler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

const signalHelperEnv = "SCHEDULER_SIGNAL_HELPER"

// TestSignalHelper is not a real test. It is re-executed as a subprocess by the
// tests below, so that the signals they send cannot affect the test binary. A
// signal is only sent from within a job, after [Run] has registered its
// handler.
func TestSignalHelper(t *testing.T) {
	switch os.Getenv(signalHelperEnv) {
	case "graceful":
		err := Run(context.Background(),
			JobFunc(func(ctx context.Context) error {
				_ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
				<-ctx.Done()
				return ctx.Err()
			}),
			JobFunc(func(ctx context.Context) error {
				<-ctx.Done()
				return context.Cause(ctx)
			}),
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Run: %v\n", err)
			os.Exit(3)
		}
		os.Exit(0)
	case "second-signal":
		_ = Run(context.Background(), JobFunc(func(ctx context.Context) error {
			_ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
			<-ctx.Done()
			// Simulate a job hanging during shutdown. Signal handling must already
			// be stopped, so this signal terminates the process.
			_ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
			time.Sleep(10 * time.Second)
			return nil
		}))
		os.Exit(0) // Should be unreachable.
	case "parent-cancel-signal":
		ctx, cancel := context.WithCancel(context.Background())
		_ = Run(ctx, JobFunc(func(ctx context.Context) error {
			cancel()
			<-ctx.Done()
			// Signal handling is stopped asynchronously after the parent is
			// canceled, so keep signaling until the process is terminated.
			for {
				_ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
				time.Sleep(25 * time.Millisecond)
			}
		}))
		os.Exit(0) // Should be unreachable.
	case "sigquit":
		_ = Run(context.Background(), JobFunc(func(ctx context.Context) error {
			_ = syscall.Kill(syscall.Getpid(), syscall.SIGQUIT)
			select {
			case <-ctx.Done():
			case <-time.After(10 * time.Second):
			}
			return nil
		}))
		os.Exit(0) // Should be unreachable.
	default:
		t.Skip("signal test helper subprocess")
	}
}

func runSignalHelper(t *testing.T, mode string) (stderr string, err error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSignalHelper$", "-test.count=1") //nolint:gosec
	cmd.Env = append(os.Environ(), signalHelperEnv+"="+mode)
	var buf bytes.Buffer
	cmd.Stderr = &buf

	err = cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("helper %q timed out; stderr:\n%s", mode, buf.String())
	}
	return buf.String(), err
}

func requireExitError(t *testing.T, err error, stderr string) *exec.ExitError {
	t.Helper()

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("err = %v, want *exec.ExitError; stderr:\n%s", err, stderr)
	}
	return exitErr
}

func TestRun_signalReturnsNil(t *testing.T) {
	t.Parallel()

	if stderr, err := runSignalHelper(t, "graceful"); err != nil {
		t.Fatalf("helper: %v, want clean exit; stderr:\n%s", err, stderr)
	}
}

func TestRun_secondSignalTerminates(t *testing.T) {
	t.Parallel()

	stderr, err := runSignalHelper(t, "second-signal")
	exitErr := requireExitError(t, err, stderr)
	ws, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !ws.Signaled() || ws.Signal() != syscall.SIGTERM {
		t.Fatalf("exit = %v, want terminated by SIGTERM; stderr:\n%s", exitErr, stderr)
	}
}

func TestRun_signalAfterParentCancelTerminates(t *testing.T) {
	t.Parallel()

	stderr, err := runSignalHelper(t, "parent-cancel-signal")
	exitErr := requireExitError(t, err, stderr)
	ws, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !ws.Signaled() || ws.Signal() != syscall.SIGTERM {
		t.Fatalf("exit = %v, want terminated by SIGTERM; stderr:\n%s", exitErr, stderr)
	}
}

func TestRun_sigquitNotHandled(t *testing.T) {
	t.Parallel()

	stderr, err := runSignalHelper(t, "sigquit")
	if code := requireExitError(t, err, stderr).ExitCode(); code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr:\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "SIGQUIT") {
		t.Fatalf("expected Go's SIGQUIT goroutine dump; stderr:\n%s", stderr)
	}
}
