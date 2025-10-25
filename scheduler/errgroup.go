// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package scheduler

import (
	"context"
	"sync"
)

// errorGroup is a collection of goroutines working on subtasks that are part of
// the same overall task. A errorGroup should not be reused for different tasks.
//
// A zero errorGroup is valid and does not cancel on error.
type errorGroup struct {
	cancel  func(error)
	wg      sync.WaitGroup
	sem     chan struct{}
	errOnce sync.Once
	err     error
}

func errorPoolWithContext(ctx context.Context) (*errorGroup, context.Context) {
	ctx, cancel := context.WithCancelCause(ctx)
	return &errorGroup{cancel: cancel}, ctx
}

func (g *errorGroup) wait() error {
	g.wg.Wait()
	if g.cancel != nil {
		g.cancel(g.err)
	}
	return g.err
}

func (g *errorGroup) run(f func() error) {
	if g.sem != nil {
		g.sem <- struct{}{}
	}

	g.wg.Add(1)
	go func() {
		defer func() {
			if g.sem != nil {
				<-g.sem
			}
			g.wg.Done()
		}()

		if err := f(); err != nil {
			g.errOnce.Do(func() {
				g.err = err
				if g.cancel != nil {
					g.cancel(g.err)
				}
			})
		}
	}()
}
