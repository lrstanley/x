// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package conc

import (
	"context"
	"sync"
)

// ErrorGroup provides synchronization, error propagation, and [context.Context]
// cancellation for groups of goroutines working on subtasks of a common task.
//
// The derived context passed to each function via [ErrorGroup.Go] or
// [ErrorGroup.TryGo] is canceled the first time a function returns a non-nil
// error or when [ErrorGroup.Wait] returns, whichever occurs first.
//
// Use [NewErrorGroup] to create an ErrorGroup.
type ErrorGroup struct {
	ctx    context.Context
	cancel context.CancelCauseFunc

	wg sync.WaitGroup

	sem chan struct{}

	errOnce sync.Once
	err     error
}

// NewErrorGroup returns a new [ErrorGroup] with a derived [context.Context]
// from ctx. If limit is greater than zero, at most that many goroutines may
// be active at once; otherwise no limit is applied.
//
// The derived context is canceled the first time a function passed to
// [ErrorGroup.Go] returns a non-nil error or the first time [ErrorGroup.Wait]
// returns, whichever occurs first.
func NewErrorGroup(ctx context.Context, limit int) *ErrorGroup {
	ctx, cancel := context.WithCancelCause(ctx)
	g := &ErrorGroup{
		ctx:    ctx,
		cancel: cancel,
	}
	if limit > 0 {
		g.sem = make(chan struct{}, limit)
	}
	return g
}

// Go calls the given function in a new goroutine, passing the group's derived
// context. It blocks until the new goroutine can be added without growing the
// number of active goroutines in the group beyond the configured limit.
//
// The first call to return a non-nil error cancels the group's context; its
// error will be returned by [ErrorGroup.Wait].
//
// If f panics, the panic value is re-raised and the goroutine slot is not
// released, matching [sync.WaitGroup.Go] semantics.
func (g *ErrorGroup) Go(f func(ctx context.Context) error) {
	if g.sem != nil {
		g.sem <- struct{}{}
	}

	g.wg.Add(1)
	go g.do(f)
}

// TryGo calls the given function in a new goroutine only if the number of
// active goroutines in the group is currently below the configured limit. The
// return value reports whether the goroutine was started.
//
// If no limit is configured, TryGo always starts the goroutine.
func (g *ErrorGroup) TryGo(f func(ctx context.Context) error) bool {
	if g.sem != nil {
		select {
		case g.sem <- struct{}{}:
		default:
			return false
		}
	}

	g.wg.Add(1)
	go g.do(f)
	return true
}

// Wait blocks until all function calls from [ErrorGroup.Go] have returned, then
// returns the first non-nil error (if any) from them.
//
// In the terminology of [the Go memory model], the return from each function
// passed to Go "synchronizes before" the return of Wait.
//
// [the Go memory model]: https://go.dev/ref/mem
func (g *ErrorGroup) Wait() error {
	g.wg.Wait()
	g.cancel(g.err)
	return g.err
}

// do is the common goroutine body shared by [ErrorGroup.Go] and [ErrorGroup.TryGo].
func (g *ErrorGroup) do(f func(ctx context.Context) error) {
	defer func() {
		if r := recover(); r != nil {
			// f panicked — re-raise without releasing the goroutine slot or
			// decrementing the WaitGroup. This prevents Wait from returning
			// (and the program from continuing) while a fatal panic propagates.
			panic(r)
		}
		g.done()
	}()

	if err := f(g.ctx); err != nil {
		g.errOnce.Do(func() {
			g.err = err
			g.cancel(g.err)
		})
	}
}

func (g *ErrorGroup) done() {
	if g.sem != nil {
		<-g.sem
	}
	g.wg.Done()
}
