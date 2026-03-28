// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package conc

import (
	"iter"
	"sync"
)

// Map is a type-safe wrapper around [sync.Map] with comparable keys and typed
// values. Like sync.Map, a Map is safe for concurrent use by multiple goroutines
// without additional locking or coordination. Its zero value is empty and ready
// for use. A Map must not be copied after first use.
//
// In the terminology of [the Go memory model], Map arranges that a write
// operation "synchronizes before" any read operation that observes the effect
// of the write, where read and write operations are defined as follows.
// [Map.Load], [Map.LoadAndDelete], and [Map.LoadOrStore] are read operations;
// [Map.Delete], [Map.LoadAndDelete], and [Map.Clear] are write operations;
// and [Map.LoadOrStore] is a write operation when it returns loaded set to false.
//
// [the Go memory model]: https://go.dev/ref/mem
type Map[K comparable, V any] struct {
	m sync.Map
}

// Load returns the value stored in the map for a key, or the zero value of V if
// no value is present. The ok result indicates whether value was found in the map.
func (m *Map[K, V]) Load(key K) (value V, ok bool) {
	v, ok := m.m.Load(key)
	if !ok {
		return value, false
	}
	return v.(V), true
}

// Store sets the value for a key.
func (m *Map[K, V]) Store(key K, value V) {
	m.m.Store(key, value)
}

// LoadOrStore returns the existing value for the key if present. Otherwise, it
// stores and returns the given value. The loaded result is true if the value was
// loaded, false if stored.
func (m *Map[K, V]) LoadOrStore(key K, value V) (actual V, loaded bool) {
	v, loaded := m.m.LoadOrStore(key, value)
	return v.(V), loaded
}

// LoadAndDelete deletes the value for a key, returning the previous value if any.
// The loaded result reports whether the key was present.
func (m *Map[K, V]) LoadAndDelete(key K) (value V, loaded bool) {
	v, loaded := m.m.LoadAndDelete(key)
	if !loaded {
		return value, false
	}
	return v.(V), true
}

// Delete deletes the value for a key.
func (m *Map[K, V]) Delete(key K) {
	m.m.Delete(key)
}

// Swap stores a value for a key and returns the previous value if any. The loaded
// result reports whether the key was present.
func (m *Map[K, V]) Swap(key K, value V) (previous V, loaded bool) {
	v, loaded := m.m.Swap(key, value)
	if !loaded {
		return previous, false
	}
	return v.(V), true
}

// CompareAndSwap swaps the old and new values for key if the value stored in the
// map is equal to old. The old value must be of a comparable type.
func (m *Map[K, V]) CompareAndSwap(key K, old, new V) (swapped bool) {
	return m.m.CompareAndSwap(key, old, new)
}

// CompareAndDelete deletes the entry for key if its value is equal to old. The
// old value must be of a comparable type.
//
// If there is no current value for key in the map, CompareAndDelete returns false
// (even if the old value is the nil interface value).
func (m *Map[K, V]) CompareAndDelete(key K, old V) (deleted bool) {
	return m.m.CompareAndDelete(key, old)
}

// Range calls f sequentially for each key and value present in the map. If f
// returns false, Range stops the iteration.
//
// Range does not necessarily correspond to any consistent snapshot of the Map's
// contents: no key will be visited more than once, but if the value for any key
// is stored or deleted concurrently (including by f), Range may reflect any
// mapping for that key from any point during the Range call. Range does not
// block other methods on the receiver; even f itself may call any method on m.
//
// Range may be O(N) with the number of elements in the map even if f returns
// false after a constant number of calls.
func (m *Map[K, V]) Range(f func(key K, value V) bool) {
	m.m.Range(func(key, value any) bool {
		return f(key.(K), value.(V))
	})
}

// Iter returns an iterator over all key-value pairs in the map. The same
// consistency guarantees as [Map.Range] apply: no key will be visited more than
// once, but concurrent modifications may be reflected at any point during
// iteration.
func (m *Map[K, V]) Iter() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		m.m.Range(func(key, value any) bool {
			return yield(key.(K), value.(V))
		})
	}
}

// Keys returns an iterator over all keys in the map. The same consistency
// guarantees as [Map.Range] apply.
func (m *Map[K, V]) Keys() iter.Seq[K] {
	return func(yield func(K) bool) {
		m.m.Range(func(key, _ any) bool {
			return yield(key.(K))
		})
	}
}

// Values returns an iterator over all values in the map. The same consistency
// guarantees as [Map.Range] apply.
func (m *Map[K, V]) Values() iter.Seq[V] {
	return func(yield func(V) bool) {
		m.m.Range(func(_, value any) bool {
			return yield(value.(V))
		})
	}
}

// Clear deletes all the entries, resulting in an empty Map.
func (m *Map[K, V]) Clear() {
	m.m.Clear()
}