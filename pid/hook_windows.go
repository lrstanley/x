// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build windows

package pid

import (
	"errors"
	"log/slog"
	"os"
	"strings"
	"syscall"
	"time"
)

func monitorHook(logger *slog.Logger, _ syscall.Signal, path string, hook func([]string)) {
	ticker := time.NewTicker(SignalRetryDelay)
	defer ticker.Stop()
	for range ticker.C {
		_, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		args, _ := os.ReadFile(path)
		_ = os.Remove(path)
		logger.Debug("secondary process args file found, invoking hook", "path", path, "args", string(args))
		hook(strings.Split(string(args), "\b"))
	}
}
