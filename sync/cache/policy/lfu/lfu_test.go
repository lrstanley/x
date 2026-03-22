// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package lfu

import (
	"testing"
)

type tmp struct {
	i int
}

func (t *tmp) GetReferenceCount() int { return t.i }

func TestSet(t *testing.T) {
	cache := New[string, int](WithCapacity(1))
	cache.Set("foo", 1)
	if got := cache.Len(); got != 1 {
		t.Fatalf("invalid length: %d", got)
	}
	if got, ok := cache.Get("foo"); got != 1 || !ok {
		t.Fatalf("invalid value got %d, cachehit %v", got, ok)
	}

	cache.Set("bar", 2)
	if got := cache.Len(); got != 1 {
		t.Fatalf("invalid length: %d", got)
	}
	bar, ok := cache.Get("bar")
	if bar != 2 || !ok {
		t.Fatalf("invalid value bar %d, cachehit %v", bar, ok)
	}

	_, ok = cache.Get("foo")
	if ok {
		t.Fatalf("invalid eviction for key foo %v", ok)
	}

	cache.Set("bar", 100)
	if got := cache.Len(); got != 1 {
		t.Fatalf("invalid length: %d", got)
	}
	bar, ok = cache.Get("bar")
	if bar != 100 || !ok {
		t.Fatalf("invalid replacing value bar %d, cachehit %v", bar, ok)
	}

	t.Run("with initial reference count", func(t *testing.T) {
		cache := New[string, *tmp](WithCapacity(2))
		cache.Set("foo", &tmp{i: 10})
		cache.Set("foo2", &tmp{i: 2})
		if got := cache.Len(); got != 2 {
			t.Fatalf("invalid length: %d", got)
		}

		cache.Set("foo3", &tmp{i: 3})

		_, ok = cache.Get("foo2")
		if ok {
			t.Fatalf("invalid eviction for key foo2 %v", ok)
		}
		_, ok = cache.Get("foo")
		if !ok {
			t.Fatalf("invalid value foo is not found")
		}
	})
}

func TestDelete(t *testing.T) {
	cache := New[string, int](WithCapacity(2))
	cache.Set("foo", 1)
	if got := cache.Len(); got != 1 {
		t.Fatalf("invalid length: %d", got)
	}

	cache.Delete("foo2")
	if got := cache.Len(); got != 1 {
		t.Fatalf("invalid length after deleted does not exist key: %d", got)
	}

	cache.Delete("foo")
	if got := cache.Len(); got != 0 {
		t.Fatalf("invalid length after deleted: %d", got)
	}
	if _, ok := cache.Get("foo"); ok {
		t.Fatalf("invalid get after deleted %v", ok)
	}
}

// check don't panic.
func TestIssue33(_ *testing.T) {
	cache := New[string, int](WithCapacity(2))
	cache.Set("foo1", 1)
	cache.Set("foo2", 2)
	cache.Set("foo3", 3)

	cache.Delete("foo1")
	cache.Delete("foo2")
	cache.Delete("foo3")
}

func TestZeroCap(t *testing.T) {
	cache := New[string, int](WithCapacity(0))
	cache.Set("foo", 1)

	v, ok := cache.Get("foo")
	if !ok {
		t.Error(ok)
	}
	if v != 1 {
		t.Error(v)
	}
}
