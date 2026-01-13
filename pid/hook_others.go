// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build !windows

package pid

import (
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

func monitorHook(logger *slog.Logger, sig syscall.Signal, path string, hook func([]string)) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, sig)
	for {
		<-c
		logger.Debug("secondary process signal received", "signal", sig)
		_, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			logger.Debug("secondary process args file not found", "path", path)
			continue
		}
		args, _ := os.ReadFile(path)
		_ = os.Remove(path)
		logger.Debug("secondary process args file found, invoking hook", "path", path, "args", string(args))
		hook(strings.Split(string(args), "\b"))
	}
}
