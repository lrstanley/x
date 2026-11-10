// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cache

import "context"

// loadToken identifies one in-flight load started by [Cache.GetOrLoad]. A load
// may store its result only while its token is still the one registered in
// Cache.loading for its key; any write to the key, or a newer load, removes or
// replaces it. The field keeps the type non-zero-sized, so each token has a
// distinct address.
type loadToken struct{ _ byte }

// GetOrLoad returns the value for key if it is cached and not expired.
// Otherwise it calls load, stores the value it returns using opts (applied
// after the cache's default entry options), and returns it.
//
// Concurrent calls for a key that is not cached share a single call to load
// and all receive its result; only the opts of the call that started the load
// are applied. Errors from load are returned to every caller sharing the call
// and are not cached, so the next GetOrLoad tries again.
//
// Each caller stops waiting when its ctx is done and returns
// [context.Cause](ctx). load receives a context carrying the values of the
// first caller's ctx. That context is canceled only once every caller sharing
// the load has stopped waiting, so one caller giving up does not cancel the
// load for the others. See [flight.Group.DoContext] for details.
//
// If [Cache.Set], [Cache.GetOrSet], [Cache.Delete], or [Cache.Clear] changes
// key while load is running, the loaded value is still returned to the callers
// waiting on it, but it is not stored, so the newer state of the cache is
// kept.
//
// If load panics, every caller waiting on it panics with a
// [*flight.PanicError]. load must not call GetOrLoad for the same key, as it
// would wait on itself.
func (c *Cache[K, V]) GetOrLoad(
	ctx context.Context,
	key K,
	load func(ctx context.Context, key K) (V, error),
	opts ...EntryOption,
) (V, error) {
	if v, ok := c.Get(key); ok {
		return v, nil
	}

	return c.loads.DoContext(ctx, key, func(ctx context.Context) (V, error) {
		tok, v, ok := c.beginLoad(key)
		if ok {
			return v, nil // Filled between our miss and starting the load.
		}

		loaded := false
		defer func() { c.endLoad(key, tok, v, loaded, opts) }()

		v, err := load(ctx, key)
		loaded = err == nil
		return v, err
	})
}

// beginLoad registers an in-flight load for key and returns its token. If key
// was filled in the meantime, it returns that value with ok set instead.
func (c *Cache[K, V]) beginLoad(key K) (tok *loadToken, v V, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if e, found := c.cache.Get(key); found && !e.Expired() {
		return nil, e.Value, true
	}

	// An earlier load for key may still be running if all of its callers gave
	// up. Replacing its token means only the newest load stores its result.
	tok = &loadToken{}
	c.loading[key] = tok
	return tok, v, false
}

// endLoad unregisters tok and, if store is set and nothing changed key during
// the load, caches v.
func (c *Cache[K, V]) endLoad(key K, tok *loadToken, v V, store bool, opts []EntryOption) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.loading[key] != tok {
		return // Superseded by a write or a newer load.
	}
	delete(c.loading, key)
	if store {
		c.store(key, v, opts)
	}
}
