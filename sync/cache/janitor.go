// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cache

import (
	"context"
	"sync"
	"time"
)

// janitor is a helper for collecting expired entries and pruning them.
type janitor struct {
	ctx  context.Context
	done chan struct{}
	stop func()
}

// newJanitor creates a new janitor. You must call [janitor.run] to start the janitor.
func newJanitor(ctx context.Context) *janitor {
	j := &janitor{
		ctx:  ctx,
		done: make(chan struct{}),
	}
	j.stop = sync.OnceFunc(func() { close(j.done) })
	return j
}

// run with the given cleanup callback function. When the context is cancelled,
// the janitor will be stopped.
func (j *janitor) run(interval time.Duration, cleanup func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			cleanup()
		case <-j.done:
			cleanup()
			return
		case <-j.ctx.Done():
			j.stop()
			return
		}
	}
}
