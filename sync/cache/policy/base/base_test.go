// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package base

import (
	"strings"
	"testing"
)

func TestSet(t *testing.T) {
	cache := New[string, int]()
	cache.Set("foo", 1)
	if got := cache.Len(); got != 1 {
		t.Fatalf("invalid length: %d", got)
	}
	if got, ok := cache.Get("foo"); got != 1 || !ok {
		t.Fatalf("invalid value got %d, cachehit %v", got, ok)
	}

	cache.Set("bar", 2)
	if got := cache.Len(); got != 2 {
		t.Fatalf("invalid length: %d", got)
	}
	bar, ok := cache.Get("bar")
	if bar != 2 || !ok {
		t.Fatalf("invalid value bar %d, cachehit %v", bar, ok)
	}

	cache.Set("bar", 100)
	if got := cache.Len(); got != 2 {
		t.Fatalf("invalid length: %d", got)
	}
	bar, ok = cache.Get("bar")
	if bar != 100 || !ok {
		t.Fatalf("invalid replacing value bar %d, cachehit %v", bar, ok)
	}
}

func TestGet(t *testing.T) {
	cache := New[string, int]()
	_, ok := cache.Get("nope")
	if ok {
		t.Fatalf("invalid get for missing key %v", ok)
	}
}

func TestDelete(t *testing.T) {
	cache := New[string, int]()
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
	cache.Set("bar", 4) // again
	cache.Set("foo", 5) // again

	got := strings.Join(cache.Keys(), ",")
	want := strings.Join([]string{"baz", "bar", "foo"}, ",")
	if got != want {
		t.Errorf("want %q, but got %q", want, got)
	}
	if len(cache.Keys()) != cache.Len() {
		t.Errorf("want number of keys %d, but got %d", len(cache.Keys()), cache.Len())
	}
}

func TestClear(t *testing.T) {
	cache := New[string, int]()
	cache.Set("foo", 1)
	cache.Set("bar", 2)
	cache.Clear()
	if got := cache.Len(); got != 0 {
		t.Fatalf("invalid length after clear: %d", got)
	}
	if _, ok := cache.Get("foo"); ok {
		t.Fatalf("expected foo to be gone after clear")
	}
	if _, ok := cache.Get("bar"); ok {
		t.Fatalf("expected bar to be gone after clear")
	}
}
