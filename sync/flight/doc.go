// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package flight collapses concurrent calls for the same key into a single
// execution, sharing its result with every caller that asked for it while it
// was in flight. This is useful in front of expensive or rate-limited work,
// such as cache fills, remote lookups, or database queries, where many
// goroutines tend to ask for the same thing at the same moment.
//
// [Group] is generic over its key and value types, so results need no type
// assertions. It offers three ways to join a call:
//
//   - [Group.Do] runs the function in the calling goroutine (if it is the first
//     caller) and blocks until the result is ready.
//   - [Group.DoContext] lets each caller stop waiting when its own context is
//     done. The function runs with a context that is canceled only once every
//     caller has given up, so one impatient caller does not cancel the work for
//     the others.
//   - [Group.DoChan] returns a channel that receives a [Result], for use in
//     select statements.
//
// Results are not cached: once a call completes, the next call for the same
// key runs the function again. Pair a [Group] with a cache to get both.
package flight
