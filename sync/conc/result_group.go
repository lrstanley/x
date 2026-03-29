// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package conc

import (
	"context"
	"slices"
	"sync"
)

// ResultGroup is a group of goroutines for tasks that return a result of type
// T. Results are returned by [ResultGroup.Wait] in the same order tasks were
// submitted.
//
// Use [NewResultGroup] to create a ResultGroup.
type ResultGroup[T any] struct {
	group     Group
	collector resultCollector[T]
}

// NewResultGroup creates a new [ResultGroup].
func NewResultGroup[T any]() *ResultGroup[T] {
	return &ResultGroup[T]{}
}

// Go submits a task that produces a result. If all goroutines are busy, Go
// blocks until a slot is available.
func (rg *ResultGroup[T]) Go(f func() T) {
	idx := rg.collector.nextIndex()
	rg.group.Go(func() {
		rg.collector.save(idx, f(), false)
	})
}

// Wait blocks until all tasks complete and returns their results in
// submission order. If any task panicked, Wait re-panics after all tasks
// finish.
func (rg *ResultGroup[T]) Wait() []T {
	defer func() { rg.collector = resultCollector[T]{} }()
	rg.group.Wait()
	return rg.collector.collect(true)
}

// WithMaxGoroutines limits concurrent goroutines. Panics if n < 1 or if
// called after [ResultGroup.Go].
func (rg *ResultGroup[T]) WithMaxGoroutines(n int) *ResultGroup[T] {
	rg.group.WithMaxGoroutines(n)
	return rg
}

// MaxGoroutines returns the configured concurrency limit, or 0 if unlimited.
func (rg *ResultGroup[T]) MaxGoroutines() int {
	return rg.group.MaxGoroutines()
}

// WithContext converts the ResultGroup to a [ResultContextGroup] for tasks
// that receive a shared [context.Context] and may return errors alongside
// their result. Panics if called after [ResultGroup.Go].
func (rg *ResultGroup[T]) WithContext(ctx context.Context) *ResultContextGroup[T] {
	rg.group.panicIfStarted()
	return &ResultContextGroup[T]{
		contextGroup: *rg.group.WithContext(ctx),
	}
}

// ResultContextGroup is a group of goroutines for tasks that receive a shared
// [context.Context] and return both a result of type T and an error. Results
// are returned by [ResultContextGroup.Wait] in submission order.
//
// By default, results from errored tasks are excluded. Use
// [ResultContextGroup.WithCollectErrored] to include them.
//
// Create with [NewResultGroup[T]().WithContext(ctx)].
type ResultContextGroup[T any] struct {
	contextGroup   ContextGroup
	collector      resultCollector[T]
	collectErrored bool
}

// Go submits a task that receives the group's context and returns a result
// and an error.
func (rcg *ResultContextGroup[T]) Go(f func(ctx context.Context) (T, error)) {
	idx := rcg.collector.nextIndex()
	rcg.contextGroup.Go(func(ctx context.Context) error {
		res, err := f(ctx)
		rcg.collector.save(idx, res, err != nil)
		return err
	})
}

// Wait blocks until all tasks complete and returns results and any errors.
// By default, results from errored tasks are excluded; use
// [ResultContextGroup.WithCollectErrored] to include them.
func (rcg *ResultContextGroup[T]) Wait() ([]T, error) {
	defer func() { rcg.collector = resultCollector[T]{} }()
	err := rcg.contextGroup.Wait()
	return rcg.collector.collect(rcg.collectErrored), err
}

// WithCollectErrored configures the group to include results from tasks that
// returned errors. By default, only results from successful tasks are
// collected. Panics if called after [ResultContextGroup.Go].
func (rcg *ResultContextGroup[T]) WithCollectErrored() *ResultContextGroup[T] {
	rcg.contextGroup.errorGroup.group.panicIfStarted()
	rcg.collectErrored = true
	return rcg
}

// WithCancelOnError configures the group to cancel its context as soon as
// any task returns an error or panics. Panics if called after
// [ResultContextGroup.Go].
func (rcg *ResultContextGroup[T]) WithCancelOnError() *ResultContextGroup[T] {
	rcg.contextGroup.WithCancelOnError()
	return rcg
}

// WithFirstError configures the group to only return the first recorded
// error. Panics if called after [ResultContextGroup.Go].
func (rcg *ResultContextGroup[T]) WithFirstError() *ResultContextGroup[T] {
	rcg.contextGroup.WithFirstError()
	return rcg
}

// WithFailFast is shorthand for WithCancelOnError().WithFirstError(). Panics
// if called after [ResultContextGroup.Go].
func (rcg *ResultContextGroup[T]) WithFailFast() *ResultContextGroup[T] {
	rcg.contextGroup.WithFailFast()
	return rcg
}

// WithMaxGoroutines limits concurrent goroutines. Panics if n < 1 or if
// called after [ResultContextGroup.Go].
func (rcg *ResultContextGroup[T]) WithMaxGoroutines(n int) *ResultContextGroup[T] {
	rcg.contextGroup.WithMaxGoroutines(n)
	return rcg
}

// MaxGoroutines returns the configured concurrency limit, or 0 if unlimited.
func (rcg *ResultContextGroup[T]) MaxGoroutines() int {
	return rcg.contextGroup.MaxGoroutines()
}

// resultCollector aggregates ordered results from concurrent goroutines. The
// zero value is valid and ready for use.
type resultCollector[T any] struct {
	mu      sync.Mutex
	count   int
	results []T
	errored []int
}

func (rc *resultCollector[T]) nextIndex() int {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	idx := rc.count
	rc.count++
	return idx
}

func (rc *resultCollector[T]) save(index int, value T, errored bool) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	if index >= len(rc.results) {
		grown := make([]T, rc.count)
		copy(grown, rc.results)
		rc.results = grown
	}

	rc.results[index] = value
	if errored {
		rc.errored = append(rc.errored, index)
	}
}

func (rc *resultCollector[T]) collect(includeErrored bool) []T {
	if includeErrored || len(rc.errored) == 0 {
		return rc.results
	}

	slices.Sort(rc.errored)
	filtered := make([]T, 0, len(rc.results)-len(rc.errored))
	errIdx := 0
	for i, v := range rc.results {
		if errIdx < len(rc.errored) && rc.errored[errIdx] == i {
			errIdx++
			continue
		}
		filtered = append(filtered, v)
	}
	return filtered
}
