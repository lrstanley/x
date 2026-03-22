// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cache

import (
	"context"
	"sync"
)

// Number is a constraint that permits any numeric type.
type Number interface {
	// Integer Signed:
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		// Integer Unsigned:
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr |
		// Float:
		~float32 | ~float64 |
		// Complex:
		~complex64 | ~complex128
}

// NumberCache is a in-memory cache which is able to store only [Number].
type NumberCache[K comparable, V Number] struct {
	*Cache[K, V]
	// nmu is used to do lock in Increment/Decrement process. Note that this must
	// use a separate mutex because mu in the Cache struct is locked in Get,
	// and if we call mu.Lock in Increment/Decrement, it will cause a deadlock.
	nmu sync.Mutex
}

// NewNumber creates a new concurrent-safe cache based on [Number]. The context
// will be used to stop the internal janitor (evicts expired entries) when the
// context is cancelled.
func NewNumber[K comparable, V Number](ctx context.Context, opts ...Option[K, V]) *NumberCache[K, V] {
	return &NumberCache[K, V]{
		Cache: New(ctx, opts...),
	}
}

// Increment an entry of type [Number] by n. Returns the incremented value.
func (nc *NumberCache[K, V]) Increment(key K, n V) V {
	nc.nmu.Lock()
	defer nc.nmu.Unlock()
	got, _ := nc.Cache.Get(key)
	nv := got + n
	nc.Cache.Set(key, nv)
	return nv
}

// Decrement an entry of type [Number] by n. Returns the decremented value.
func (nc *NumberCache[K, V]) Decrement(key K, n V) V {
	nc.nmu.Lock()
	defer nc.nmu.Unlock()
	got, _ := nc.Cache.Get(key)
	nv := got - n
	nc.Cache.Set(key, nv)
	return nv
}
