// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package httpccache (http client cache) implements a caching
// [net/http.RoundTripper] that stores responses and serves them when fresh, with
// pluggable storage backends and HTTP cache semantics.
package httpccache
