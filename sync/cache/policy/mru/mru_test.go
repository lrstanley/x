// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package mru

import (
	"strings"
	"testing"
)

func TestSet(t *testing.T) {
	cache := New[string, int](WithCapacity(2))
	cache.Set("foo", 1)
	cache.Set("bar", 2)
	if got := cache.Len(); got != 2 {
		t.Fatalf("invalid length: %d", got)
	}
	if got, ok := cache.Get("foo"); got != 1 || !ok {
		t.Fatalf("invalid value got %d, cachehit %v", got, ok)
	}

	cache.Set("baz", 3)
	if got := cache.Len(); got != 2 {
		t.Fatalf("invalid length: %d", got)
	}
	baz, ok := cache.Get("baz")
	if baz != 3 || !ok {
		t.Fatalf("invalid value baz %d, cachehit %v", baz, ok)
	}

	_, ok = cache.Get("bar")
	if ok {
		t.Log(cache.Keys())
		t.Fatalf("invalid eviction for key bar %v", ok)
	}

	cache.Set("foo", 100)
	if got := cache.Len(); got != 2 {
		t.Fatalf("invalid length: %d", got)
	}
	foo, ok := cache.Get("foo")
	if foo != 100 || !ok {
		t.Fatalf("invalid replacing value foo %d, cachehit %v", foo, ok)
	}
}

func TestDelete(t *testing.T) {
	cache := New[string, int](WithCapacity(1))
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

func TestKeys(t *testing.T) {
	cache := New[string, int]()
	cache.Set("foo", 1)
	cache.Set("bar", 2)
	cache.Set("baz", 3)
	cache.Set("bar", 4)
	cache.Set("foo", 5)

	got := strings.Join(cache.Keys(), ",")
	want := strings.Join([]string{"foo", "bar", "baz"}, ",")
	if got != want {
		t.Errorf("want %q, but got %q", want, got)
	}
	if len(cache.Keys()) != cache.Len() {
		t.Errorf("want number of keys %d, but got %d", len(cache.Keys()), cache.Len())
	}
}
