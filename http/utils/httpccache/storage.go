// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package httpccache

import (
	"context"
	"errors"
	"io"
)

// ErrNotFound is returned by [Storage.Get] when the requested key does not
// exist in the storage backend.
var ErrNotFound = errors.New("httpccache: record not found")

// Storage is the backend contract used by the cache transport. Implementations
// store opaque metadata bytes and timestamps separately from the response body.
// Marshaling [CacheEntry] to metadata is the caller's responsibility.
type Storage interface {
	// Get retrieves a cached entry by key. The caller must close the body.
	Get(ctx context.Context, key string) (entry *CacheEntry, body io.ReadCloser, err error)
	// Set stores metadata and streams the body. When body is nil, only metadata
	// and timestamps are updated and existing body bytes are preserved.
	Set(ctx context.Context, key string, entry *CacheEntry, body io.Reader) error
	Delete(ctx context.Context, key string) error
	// Prune removes entries past the storage max-age (if configured) and enforces
	// any max-capacity policy (e.g. evicting oldest). Backends that do not
	// implement pruning return nil.
	Prune(ctx context.Context) error
}
