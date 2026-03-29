// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package conc

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

// Group is a group of goroutines used to execute tasks concurrently.
//
// Tasks are submitted with [Group.Go]. Once all tasks have been submitted,
// [Group.Wait] must be called to clean up goroutines and propagate any panics.
//
// Configuration methods (With*) will panic if called after [Group.Go] has been
// invoked. After [Group.Wait] returns, the Group may be reused with the same
// configuration.
//
// Use [NewGroup] to create a Group.
type Group struct {
	wg  sync.WaitGroup
	sem chan struct{}

	// Kept separate because panic(nil) is valid; a nil pointer means no panic.
	panicVal atomic.Pointer[any]
	panicked atomic.Bool
	started  atomic.Bool
}

// NewGroup creates a new [Group]. The zero value is also valid and ready for
// use.
func NewGroup() *Group {
	return &Group{}
}

// Go submits a task to run in a new goroutine. If a concurrency limit is
// configured via [Group.WithMaxGoroutines], Go blocks until a slot is
// available.
//
// If f panics, the panic value is captured and re-raised by [Group.Wait].
func (g *Group) Go(f func()) {
	g.started.Store(true)
	if g.sem != nil {
		g.sem <- struct{}{}
	}
	g.wg.Go(func() {
		defer func() {
			if g.sem != nil {
				<-g.sem
			}
		}()
		defer g.recoverPanic()
		f()
	})
}

// Wait blocks until all submitted tasks complete. If any task panicked, Wait
// re-panics with the first captured panic value after all tasks finish.
//
// After Wait returns, the Group is reset and may be reused.
func (g *Group) Wait() {
	defer g.reset()
	g.wg.Wait()
	if g.panicked.Load() {
		panic(*g.panicVal.Load())
	}
}

// WithMaxGoroutines limits the number of goroutines that may be active at
// once. Panics if n < 1 or if called after [Group.Go].
func (g *Group) WithMaxGoroutines(n int) *Group {
	g.panicIfStarted()
	if n < 1 {
		panic("conc: max goroutines must be at least 1")
	}
	g.sem = make(chan struct{}, n)
	return g
}

// MaxGoroutines returns the configured concurrency limit, or 0 if unlimited.
func (g *Group) MaxGoroutines() int {
	if g.sem == nil {
		return 0
	}
	return cap(g.sem)
}

// WithErrors converts the Group to an [ErrorGroup] for tasks that return
// errors. Panics if called after [Group.Go].
func (g *Group) WithErrors() *ErrorGroup {
	g.panicIfStarted()
	return &ErrorGroup{group: g.deref()}
}

// WithContext converts the Group to a [ContextGroup] for tasks that receive a
// shared [context.Context] and may return errors. Panics if called after
// [Group.Go].
func (g *Group) WithContext(ctx context.Context) *ContextGroup {
	g.panicIfStarted()
	return g.WithErrors().WithContext(ctx)
}

func (g *Group) recoverPanic() {
	if r := recover(); r != nil {
		if g.panicked.CompareAndSwap(false, true) {
			val := any(r)
			g.panicVal.Store(&val)
		}
	}
}

func (g *Group) reset() {
	g.started.Store(false)
	g.panicked.Store(false)
	g.panicVal.Store(nil)
}

func (g *Group) panicIfStarted() {
	if g.started.Load() {
		panic("conc: group cannot be reconfigured after Go()")
	}
}

func (g *Group) deref() Group {
	g.panicIfStarted()
	return Group{sem: g.sem}
}

// ErrorGroup is a group of goroutines for tasks that may return an error.
// Errors are collected and returned by [ErrorGroup.Wait].
//
// By default all errors are combined via [errors.Join]. Use
// [ErrorGroup.WithFirstError] to return only the first recorded error.
//
// Create with [NewGroup().WithErrors()].
type ErrorGroup struct {
	group Group

	onlyFirstError bool

	mu   sync.Mutex
	errs []error
}

// Go submits a task to the group. If all goroutines are busy, Go blocks until
// a slot is available.
func (eg *ErrorGroup) Go(f func() error) {
	eg.group.Go(func() {
		eg.addErr(f())
	})
}

// Wait blocks until all tasks complete, then returns collected errors. If any
// task panicked, Wait re-panics after all tasks finish.
//
// By default all errors are returned via [errors.Join]. If
// [ErrorGroup.WithFirstError] was called, only the first error is returned.
func (eg *ErrorGroup) Wait() error {
	defer func() { eg.errs = nil }()
	eg.group.Wait()

	if len(eg.errs) == 0 {
		return nil
	}
	if eg.onlyFirstError {
		return eg.errs[0]
	}
	return errors.Join(eg.errs...)
}

// WithFirstError configures the group to only return the first recorded error
// rather than a combined error. Panics if called after [ErrorGroup.Go].
func (eg *ErrorGroup) WithFirstError() *ErrorGroup {
	eg.group.panicIfStarted()
	eg.onlyFirstError = true
	return eg
}

// WithMaxGoroutines limits the number of concurrent goroutines. Panics if
// n < 1 or if called after [ErrorGroup.Go].
func (eg *ErrorGroup) WithMaxGoroutines(n int) *ErrorGroup {
	eg.group.WithMaxGoroutines(n)
	return eg
}

// MaxGoroutines returns the configured concurrency limit, or 0 if unlimited.
func (eg *ErrorGroup) MaxGoroutines() int {
	return eg.group.MaxGoroutines()
}

// WithContext converts the ErrorGroup to a [ContextGroup]. Panics if called
// after [ErrorGroup.Go].
func (eg *ErrorGroup) WithContext(ctx context.Context) *ContextGroup {
	eg.group.panicIfStarted()
	dctx, cancel := context.WithCancelCause(ctx)
	return &ContextGroup{
		errorGroup: eg.deref(),
		parentCtx:  ctx,
		ctx:        dctx,
		cancel:     cancel,
	}
}

func (eg *ErrorGroup) addErr(err error) {
	if err != nil {
		eg.mu.Lock()
		eg.errs = append(eg.errs, err)
		eg.mu.Unlock()
	}
}

func (eg *ErrorGroup) deref() ErrorGroup {
	eg.group.panicIfStarted()
	return ErrorGroup{
		group:          eg.group.deref(),
		onlyFirstError: eg.onlyFirstError,
	}
}

// ContextGroup is a group of goroutines for tasks that receive a shared
// [context.Context] and may return errors.
//
// The derived context is canceled when [ContextGroup.Wait] returns. If
// [ContextGroup.WithCancelOnError] is configured, the context is also
// canceled as soon as any task returns a non-nil error or panics.
//
// Create with [NewGroup().WithContext(ctx)].
type ContextGroup struct {
	errorGroup ErrorGroup

	parentCtx     context.Context
	ctx           context.Context
	cancel        context.CancelCauseFunc
	cancelOnError bool
}

// Go submits a task that receives the group's derived context. If the task
// returns an error and [ContextGroup.WithCancelOnError] was called, the
// context is immediately canceled.
func (cg *ContextGroup) Go(f func(ctx context.Context) error) {
	cg.errorGroup.Go(func() error {
		if cg.cancelOnError {
			defer func() {
				if r := recover(); r != nil {
					cg.cancel(nil)
					panic(r)
				}
			}()
		}

		err := f(cg.ctx)
		if err != nil && cg.cancelOnError {
			cg.errorGroup.addErr(err)
			cg.cancel(err)
			return nil
		}
		return err
	})
}

// Wait blocks until all tasks complete and returns collected errors. The
// group's derived context is canceled when Wait returns regardless of
// errors.
//
// After Wait returns, the ContextGroup is ready for reuse with a fresh
// derived context.
func (cg *ContextGroup) Wait() error {
	defer func() {
		cg.cancel(nil)
		cg.ctx, cg.cancel = context.WithCancelCause(cg.parentCtx)
	}()
	return cg.errorGroup.Wait()
}

// WithCancelOnError configures the group to cancel its context as soon as
// any task returns an error or panics. By default, the context is not
// canceled until [ContextGroup.Wait] returns.
//
// Panics if called after [ContextGroup.Go].
func (cg *ContextGroup) WithCancelOnError() *ContextGroup {
	cg.errorGroup.group.panicIfStarted()
	cg.cancelOnError = true
	return cg
}

// WithFirstError configures the group to only return the first recorded
// error. This is especially useful with [ContextGroup.WithCancelOnError]
// where subsequent errors are likely [context.Canceled].
//
// Panics if called after [ContextGroup.Go].
func (cg *ContextGroup) WithFirstError() *ContextGroup {
	cg.errorGroup.WithFirstError()
	return cg
}

// WithFailFast is shorthand for WithCancelOnError().WithFirstError().
//
// Panics if called after [ContextGroup.Go].
func (cg *ContextGroup) WithFailFast() *ContextGroup {
	cg.WithCancelOnError()
	cg.WithFirstError()
	return cg
}

// WithMaxGoroutines limits the number of concurrent goroutines. Panics if
// n < 1 or if called after [ContextGroup.Go].
func (cg *ContextGroup) WithMaxGoroutines(n int) *ContextGroup {
	cg.errorGroup.WithMaxGoroutines(n)
	return cg
}

// MaxGoroutines returns the configured concurrency limit, or 0 if unlimited.
func (cg *ContextGroup) MaxGoroutines() int {
	return cg.errorGroup.MaxGoroutines()
}
