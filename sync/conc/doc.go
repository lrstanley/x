// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package conc provides concurrency primitives for bounding and coordinating
// goroutine work:
//
//   - [Map] is a type-safe generic wrapper around [sync.Map].
//   - [Semaphore] limits concurrent work to a fixed number of logical slots.
//   - [WeightedSemaphore] limits concurrent work by tracking weighted resource usage.
//   - [ErrorGroup] provides synchronization, error propagation, and context
//     cancellation for groups of goroutines working on subtasks of a common
//     task. See [NewErrorGroup].
package conc
