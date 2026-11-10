// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package flight

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
)

// ErrGoexit is returned to callers waiting on a call whose function invoked
// [runtime.Goexit] (for example through [testing.T.FailNow]) instead of
// returning.
var ErrGoexit = errors.New("flight: function called runtime.Goexit")

// PanicError carries a panic raised by a function run through a [Group], along
// with the stack trace of the goroutine that panicked. Callers that wait
// synchronously ([Group.Do], [Group.DoContext]) re-panic with a *PanicError;
// [Group.DoChan] delivers it as [Result.Err].
type PanicError struct {
	// Value is the value originally passed to panic.
	Value any
	// Stack is the stack trace captured where the panic was recovered.
	Stack []byte
}

// Error implements the error interface.
func (e *PanicError) Error() string {
	return fmt.Sprintf("flight: panic in shared call: %v\n\n%s", e.Value, e.Stack)
}

// Unwrap returns the panic value if it is an error, so [errors.Is] and
// [errors.As] can see through the panic.
func (e *PanicError) Unwrap() error {
	err, _ := e.Value.(error)
	return err
}

// Result is the outcome of a call, as delivered by [Group.DoChan].
type Result[V any] struct {
	// Val is the value returned by the function.
	Val V
	// Err is the error returned by the function, a [*PanicError] if it
	// panicked, or [ErrGoexit] if it called [runtime.Goexit].
	Err error
	// Shared reports whether more than one caller joined the call.
	Shared bool
}

// Group deduplicates concurrent calls that share a key of type K and produce a
// value of type V. The zero value is ready for use. A Group must not be copied
// after first use.
type Group[K comparable, V any] struct {
	mu    sync.Mutex
	calls map[K]*call[V]
}

// call is a single in-flight (or just completed) execution of a function.
type call[V any] struct {
	done chan struct{} // closed once the outcome fields below are set.

	// Outcome. Written by the goroutine running the function before done is
	// closed, read by anyone after done is closed. If the function panicked or
	// called runtime.Goexit, val is zero and err is panic or ErrGoexit.
	val   V
	err   error
	panic *PanicError

	// Guarded by Group.mu.
	joins   int                // callers that have joined, ever.
	waiting int                // callers still waiting for the outcome.
	chans   []chan<- Result[V] // DoChan receivers.

	// Set only for calls started by DoContext. Immutable after creation.
	ctx    context.Context
	cancel context.CancelCauseFunc
}

// outcome returns the result of a completed call to a synchronous caller,
// re-panicking if the function panicked.
func (c *call[V]) outcome() (V, error) {
	if c.panic != nil {
		panic(c.panic)
	}
	return c.val, c.err
}

// Do runs fn and returns its result, unless a call for key is already in
// flight, in which case Do waits for that call and returns its result instead.
// The first caller for a key runs fn in the calling goroutine.
//
// If fn panics, every caller waiting on it (including the one that ran it)
// panics with a [*PanicError]. If fn calls [runtime.Goexit], the goroutine
// running it exits as usual and the other callers receive [ErrGoexit].
func (g *Group[K, V]) Do(key K, fn func() (V, error)) (V, error) {
	c, leader := g.join(key, nil, nil)
	if leader {
		g.run(key, c, fn)
	} else {
		<-c.done
	}
	return c.outcome()
}

// DoContext is like [Group.Do], but each caller stops waiting and returns the
// cause of its context's cancellation (see [context.Cause]) once ctx is done.
// The call itself keeps running for any callers still waiting.
//
// fn always runs in a new goroutine and receives a context that carries the
// values of the first caller's ctx, but none of its deadline or cancellation.
// That context is canceled, with the last departing caller's cause, once every
// caller has stopped waiting. The key is then forgotten, so later calls start
// fresh instead of joining abandoned work. Callers that joined through
// [Group.Do] or [Group.DoChan] never stop waiting, so they keep the call alive.
//
// Panics and [runtime.Goexit] in fn are reported as in [Group.Do]. If no caller
// is waiting when fn panics, the panic is discarded.
func (g *Group[K, V]) DoContext(ctx context.Context, key K, fn func(context.Context) (V, error)) (V, error) {
	if ctx.Err() != nil {
		var zero V
		return zero, context.Cause(ctx)
	}

	c, leader := g.join(key, nil, func(c *call[V]) {
		c.ctx, c.cancel = context.WithCancelCause(context.WithoutCancel(ctx))
	})
	if leader {
		go g.run(key, c, func() (V, error) { return fn(c.ctx) })
	}

	select {
	case <-c.done:
		return c.outcome()
	case <-ctx.Done():
		cause := context.Cause(ctx)
		g.leave(key, c, cause)
		var zero V
		return zero, cause
	}
}

// DoChan is like [Group.Do], but returns a channel that receives the [Result]
// once it is ready, instead of blocking. The channel is buffered and receives
// exactly one value; it is never closed.
//
// If DoChan starts the call, fn runs in a new goroutine. A panic in fn is
// delivered to channel receivers as a [*PanicError] in [Result.Err] rather than
// crashing the program, while callers blocked in [Group.Do] or
// [Group.DoContext] still re-panic.
func (g *Group[K, V]) DoChan(key K, fn func() (V, error)) <-chan Result[V] {
	ch := make(chan Result[V], 1)
	if c, leader := g.join(key, ch, nil); leader {
		go g.run(key, c, fn)
	}
	return ch
}

// Forget makes the next call for key run its function again, even if a call
// for key is still in flight. Callers already waiting on the in-flight call
// still receive its result.
func (g *Group[K, V]) Forget(key K) {
	g.mu.Lock()
	delete(g.calls, key)
	g.mu.Unlock()
}

// join registers a caller for key, starting a new call if none is in flight.
// leader reports whether the caller is responsible for running the function.
// A non-nil ch is registered to receive the result. A non-nil init is applied
// to a newly started call before any other caller can see it.
func (g *Group[K, V]) join(key K, ch chan<- Result[V], init func(*call[V])) (c *call[V], leader bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	c, ok := g.calls[key]
	if !ok {
		c = &call[V]{done: make(chan struct{})}
		if init != nil {
			init(c)
		}
		if g.calls == nil {
			g.calls = make(map[K]*call[V])
		}
		g.calls[key] = c
	}

	c.joins++
	if ch != nil {
		c.chans = append(c.chans, ch)
	}
	c.waiting++
	return c, !ok
}

// leave records that a DoContext caller has stopped waiting on c. When no
// callers remain, the call's context is canceled and the key forgotten.
func (g *Group[K, V]) leave(key K, c *call[V], cause error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	c.waiting--
	if c.waiting > 0 || c.cancel == nil {
		return
	}
	c.cancel(cause)
	if g.calls[key] == c {
		delete(g.calls, key)
	}
}

// run executes fn for c and publishes its outcome, including when fn panics or
// calls [runtime.Goexit]. Panics are recovered here and re-raised by
// [call.outcome] in each synchronous caller.
func (g *Group[K, V]) run(key K, c *call[V], fn func() (V, error)) {
	returned := false
	defer func() {
		if !returned {
			// recover returns nil only during runtime.Goexit, as panic(nil)
			// surfaces as a *runtime.PanicNilError since Go 1.21.
			if r := recover(); r != nil {
				c.panic = &PanicError{Value: r, Stack: debug.Stack()}
				c.err = c.panic
			} else {
				c.err = ErrGoexit
			}
		}
		g.finish(key, c)
	}()

	c.val, c.err = fn()
	returned = true
}

// finish removes c from the group and wakes everyone waiting on it.
func (g *Group[K, V]) finish(key K, c *call[V]) {
	if c.cancel != nil {
		c.cancel(nil) // Release the call's context resources.
	}

	g.mu.Lock()
	if g.calls[key] == c {
		delete(g.calls, key)
	}
	// c is no longer in the map (here, or earlier via Forget or abandonment),
	// so no caller can join it and chans and joins are final.
	chans, shared := c.chans, c.joins > 1
	g.mu.Unlock()

	res := Result[V]{Val: c.val, Err: c.err, Shared: shared}
	for _, ch := range chans {
		ch <- res // Buffered, and each channel is registered once.
	}
	close(c.done)
}
