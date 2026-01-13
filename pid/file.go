// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package pid

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	MaxSignalRetries = 10
	SignalRetryDelay = 100 * time.Millisecond
)

// File represents a pidfile for a given application.
type File struct {
	firstPID int

	mu          sync.RWMutex
	appID       string
	logger      *slog.Logger
	dir         string
	signal      syscall.Signal
	onSecondary func([]string)
}

// New creates a pidfile instance based on the provided application ID.
func New(appID string) *File {
	dir := os.TempDir()
	if dir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			panic(err)
		}
		dir = cwd
	}
	return &File{
		appID:       appID,
		logger:      slog.New(slog.DiscardHandler),
		dir:         dir,
		signal:      syscall.SIGTERM,
		onSecondary: nil,
	}
}

// WithAppID sets the application ID for the pidfile.
func (pf *File) WithAppID(appID string) *File {
	pf.mu.Lock()
	pf.appID = appID
	pf.mu.Unlock()
	return pf
}

// WithLogger sets the logger for the pidfile.
func (pf *File) WithLogger(logger *slog.Logger) *File {
	pf.mu.Lock()
	pf.logger = logger
	pf.mu.Unlock()
	return pf
}

func (pf *File) log() *slog.Logger {
	pf.mu.RLock()
	defer pf.mu.RUnlock()
	return pf.logger
}

// WithDir sets the directory for the pidfile. Defaults to the system temp directory.
func (pf *File) WithDir(dir string) *File {
	pf.mu.Lock()
	pf.dir = dir
	pf.mu.Unlock()
	return pf
}

// WithSignal sets the signal that will be sent to the first process when a new
// process is created. Defaults to SIGTERM.
func (pf *File) WithSignal(sig syscall.Signal) *File {
	pf.mu.Lock()
	pf.signal = sig
	pf.mu.Unlock()
	return pf
}

// WithSecondary sets the hook that will be called when a secondary process is created.
func (pf *File) WithSecondary(fn func(args []string)) *File {
	pf.mu.Lock()
	pf.onSecondary = fn
	pf.mu.Unlock()
	return pf
}

// FirstPID returns the PID of the first process. Should not be called until after
// [File.Create] has been called.
func (pf *File) FirstPID() int {
	pf.mu.RLock()
	defer pf.mu.RUnlock()
	return pf.firstPID
}

// IsFirst returns true if the current process is the first process.
func (pf *File) IsFirst() bool {
	return pf.FirstPID() == os.Getpid()
}

// path returns the path to the pidfile.
func (pf *File) path() string {
	pf.mu.RLock()
	defer pf.mu.RUnlock()
	return filepath.Join(pf.dir, fmt.Sprintf("%s.pid", pf.appID))
}

func lookupProcess(pid int) *os.Process {
	process, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}
	err = process.Signal(syscall.Signal(0))
	if err != nil {
		return nil
	}
	return process
}

// Create creates the pidfile.
func (pf *File) Create() error {
	pf.mu.RLock()
	sig := pf.signal
	hook := pf.onSecondary
	pf.mu.RUnlock()

	if hook != nil {
		go func() {
			c := make(chan os.Signal, 1)
			signal.Notify(c, sig)
			for {
				<-c
				pf.log().Debug("secondary process signal received", "signal", sig)
				_, err := os.Stat(pf.path() + ".args")
				if errors.Is(err, os.ErrNotExist) {
					pf.log().Debug("secondary process args file not found", "path", pf.path()+".args")
					continue
				}
				args, _ := os.ReadFile(pf.path() + ".args")
				_ = os.Remove(pf.path() + ".args")
				pf.log().Debug("secondary process args file found, invoking hook", "path", pf.path()+".args", "args", string(args))
				hook(strings.Split(string(args), "\b"))
			}
		}()
	}

	pf.firstPID = os.Getpid()
	pid := os.Getpid()
	f, err := os.OpenFile(pf.path(), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640) //nolint:gosec
	if err == nil {
		pf.log().Debug("pidfile created", "path", pf.path(), "pid", pid)
		_, _ = f.WriteString(strconv.Itoa(pid))
		_ = f.Sync()
		_ = f.Close()
		return nil
	}
	_ = f.Close()

	if !os.IsExist(err) {
		return err
	}

	time.Sleep(time.Millisecond * 100)
	data, err := os.ReadFile(pf.path())
	if err != nil {
		return err
	}

	pid, err = strconv.Atoi(string(data))
	if err != nil {
		return err
	}

	process := lookupProcess(pid)

	if process == nil {
		// Process not found, but file exists, delete and recreate it.
		pf.log().Debug("process not found, deleting and recreating pidfile", "path", pf.path())
		err = os.Remove(pf.path())
		if err != nil {
			return err
		}
		return os.WriteFile(pf.path(), []byte(strconv.Itoa(os.Getpid())), 0o640) //nolint:gosec
	}

	pf.firstPID = pid
	if hook != nil {
		var file *os.File
		for range MaxSignalRetries {
			pf.log().Debug("attempting to open args lock file", "path", pf.path()+".args.lock")
			file, err = os.OpenFile(pf.path()+".args.lock", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640) //nolint:gosec
			if err == nil {
				defer func() { //nolint:gocritic
					_ = file.Close()
					_ = os.Remove(pf.path() + ".args.lock")
				}()
				break
			}
			time.Sleep(SignalRetryDelay)
		}

		pf.log().Debug("writing args to file", "path", pf.path()+".args", "args", strings.Join(os.Args, "\b"))
		err = os.WriteFile(pf.path()+".args", []byte(strings.Join(os.Args, "\b")), 0o640) //nolint:gosec
		if err != nil {
			return err
		}

		time.Sleep(SignalRetryDelay)

		pf.log().Debug("signaling primary process", "signal", sig)
		err = process.Signal(sig)
		if err != nil {
			return err
		}
	}
	return nil
}

// Remove removes the pidfile.
func (pf *File) Remove() error {
	pid, err := os.ReadFile(pf.path())
	if err != nil {
		return err
	}

	if string(pid) != strconv.Itoa(os.Getpid()) {
		pf.log().Debug("pidfile does not match current process, skipping removal", "path", pf.path(), "pid", string(pid), "current_pid", os.Getpid())
		return nil
	}

	pf.log().Debug("removing pidfile", "path", pf.path())
	err = os.Remove(pf.path())
	if err != nil {
		return err
	}

	pf.mu.RLock()
	hook := pf.onSecondary
	pf.mu.RUnlock()

	if hook != nil {
		pf.log().Debug("removing args lock and args files", "path", pf.path()+".args.lock", "path", pf.path()+".args")
		_ = os.Remove(pf.path() + ".args.lock")
		pf.log().Debug("removed args lock file", "path", pf.path()+".args.lock")
		_ = os.Remove(pf.path() + ".args")
	}
	return nil
}
