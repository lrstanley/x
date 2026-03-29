// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package conc provides concurrency primitives for bounding and coordinating
// goroutine work:
//
//   - [Group] executes tasks concurrently with optional goroutine limits.
//   - [ErrorGroup] extends [Group] for tasks that return errors.
//   - [ContextGroup] extends [ErrorGroup] for tasks that share a [context.Context],
//     with optional cancel-on-error semantics.
//   - [ResultGroup] executes tasks that return a result, preserving submission order.
//   - [ResultContextGroup] extends [ContextGroup] for tasks that return both a result
//     and an error, preserving submission order.
//   - [Map] is a type-safe generic wrapper around [sync.Map].
//   - [Semaphore] limits concurrent work to a fixed number of logical slots.
//   - [WeightedSemaphore] limits concurrent work by tracking weighted resource usage.
package conc
