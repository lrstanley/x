// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package rate provides rate limiters for controlling the frequency of operations.
//
// One strategy is currently supported:
//
//   - [KeyWindowLimiter]: a sliding-window counter limiter keyed by arbitrary strings,
//     suitable for per-user or per-resource rate limiting. Storage is abstracted
//     behind the [WindowCounter] interface; an in-memory implementation is provided
//     by [NewLocalCounter].
//
// All types are safe for concurrent use.
package rate
