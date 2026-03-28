// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package conc

import (
	"sync"
	"testing"
)

func TestMap_zeroValue(t *testing.T) {
	t.Parallel()

	var m Map[string, int]
	m.Store("a", 1)
	v, ok := m.Load("a")
	if !ok || v != 1 {
		t.Fatalf("got (%v, %v), want (1, true)", v, ok)
	}
}

func TestMap_Load_missing(t *testing.T) {
	t.Parallel()

	var m Map[string, int]
	v, ok := m.Load("missing")
	if ok {
		t.Fatal("expected ok to be false for missing key")
	}
	if v != 0 {
		t.Fatalf("expected zero value, got %v", v)
	}
}

func TestMap_Store_Load(t *testing.T) {
	t.Parallel()

	var m Map[int, string]
	m.Store(1, "one")
	m.Store(2, "two")

	tests := []struct {
		key  int
		want string
		ok   bool
	}{
		{1, "one", true},
		{2, "two", true},
		{3, "", false},
	}

	for _, tt := range tests {
		v, ok := m.Load(tt.key)
		if ok != tt.ok || v != tt.want {
			t.Errorf("Load(%d) = (%v, %v), want (%v, %v)", tt.key, v, ok, tt.want, tt.ok)
		}
	}
}

func TestMap_LoadOrStore(t *testing.T) {
	t.Parallel()

	var m Map[string, int]

	actual, loaded := m.LoadOrStore("a", 1)
	if loaded || actual != 1 {
		t.Fatalf("first LoadOrStore: got (%v, %v), want (1, false)", actual, loaded)
	}

	actual, loaded = m.LoadOrStore("a", 2)
	if !loaded || actual != 1 {
		t.Fatalf("second LoadOrStore: got (%v, %v), want (1, true)", actual, loaded)
	}
}

func TestMap_LoadAndDelete(t *testing.T) {
	t.Parallel()

	var m Map[string, int]
	m.Store("a", 42)

	v, loaded := m.LoadAndDelete("a")
	if !loaded || v != 42 {
		t.Fatalf("LoadAndDelete existing: got (%v, %v), want (42, true)", v, loaded)
	}

	v, loaded = m.LoadAndDelete("a")
	if loaded {
		t.Fatalf("LoadAndDelete deleted key: got (%v, %v), want (0, false)", v, loaded)
	}
	if v != 0 {
		t.Fatalf("expected zero value for deleted key, got %v", v)
	}
}

func TestMap_Delete(t *testing.T) {
	t.Parallel()

	var m Map[string, int]
	m.Store("a", 1)
	m.Delete("a")

	_, ok := m.Load("a")
	if ok {
		t.Fatal("key should be deleted")
	}
}

func TestMap_Swap(t *testing.T) {
	t.Parallel()

	var m Map[string, int]

	prev, loaded := m.Swap("a", 1)
	if loaded {
		t.Fatalf("Swap on empty: got (%v, %v), want (0, false)", prev, loaded)
	}
	if prev != 0 {
		t.Fatalf("expected zero value for missing key, got %v", prev)
	}

	prev, loaded = m.Swap("a", 2)
	if !loaded || prev != 1 {
		t.Fatalf("Swap existing: got (%v, %v), want (1, true)", prev, loaded)
	}
}

func TestMap_CompareAndSwap(t *testing.T) {
	t.Parallel()

	var m Map[string, int]
	m.Store("a", 1)

	if m.CompareAndSwap("a", 999, 2) {
		t.Fatal("CompareAndSwap should fail with wrong old value")
	}
	if !m.CompareAndSwap("a", 1, 2) {
		t.Fatal("CompareAndSwap should succeed with correct old value")
	}

	v, _ := m.Load("a")
	if v != 2 {
		t.Fatalf("after CompareAndSwap: got %v, want 2", v)
	}
}

func TestMap_CompareAndDelete(t *testing.T) {
	t.Parallel()

	var m Map[string, int]
	m.Store("a", 1)

	if m.CompareAndDelete("a", 999) {
		t.Fatal("CompareAndDelete should fail with wrong value")
	}
	if !m.CompareAndDelete("a", 1) {
		t.Fatal("CompareAndDelete should succeed with correct value")
	}

	_, ok := m.Load("a")
	if ok {
		t.Fatal("key should be deleted after CompareAndDelete")
	}
}

func TestMap_Range(t *testing.T) {
	t.Parallel()

	var m Map[string, int]
	m.Store("a", 1)
	m.Store("b", 2)
	m.Store("c", 3)

	seen := make(map[string]int)
	m.Range(func(key string, value int) bool {
		seen[key] = value
		return true
	})

	if len(seen) != 3 {
		t.Fatalf("Range visited %d keys, want 3", len(seen))
	}
	for _, k := range []string{"a", "b", "c"} {
		if _, ok := seen[k]; !ok {
			t.Errorf("Range did not visit key %q", k)
		}
	}
}

func TestMap_Range_earlyStop(t *testing.T) {
	t.Parallel()

	var m Map[int, int]
	for i := range 10 {
		m.Store(i, i)
	}

	count := 0
	m.Range(func(_ int, _ int) bool {
		count++
		return count < 3
	})

	if count != 3 {
		t.Fatalf("Range visited %d keys, want 3", count)
	}
}

func TestMap_Clear(t *testing.T) {
	t.Parallel()

	var m Map[string, int]
	m.Store("a", 1)
	m.Store("b", 2)
	m.Clear()

	count := 0
	m.Range(func(_ string, _ int) bool {
		count++
		return true
	})
	if count != 0 {
		t.Fatalf("after Clear, Range visited %d keys, want 0", count)
	}
}

func TestMap_Iter(t *testing.T) {
	t.Parallel()

	var m Map[string, int]
	m.Store("a", 1)
	m.Store("b", 2)
	m.Store("c", 3)

	seen := make(map[string]int)
	for k, v := range m.Iter() {
		seen[k] = v
	}

	if len(seen) != 3 {
		t.Fatalf("Iter visited %d keys, want 3", len(seen))
	}
	for _, k := range []string{"a", "b", "c"} {
		if _, ok := seen[k]; !ok {
			t.Errorf("Iter did not visit key %q", k)
		}
	}
}

func TestMap_Iter_break(t *testing.T) {
	t.Parallel()

	var m Map[int, int]
	for i := range 10 {
		m.Store(i, i)
	}

	count := 0
	for range m.Iter() {
		count++
		if count >= 3 {
			break
		}
	}

	if count != 3 {
		t.Fatalf("Iter visited %d keys before break, want 3", count)
	}
}

func TestMap_Keys(t *testing.T) {
	t.Parallel()

	var m Map[string, int]
	m.Store("a", 1)
	m.Store("b", 2)
	m.Store("c", 3)

	seen := make(map[string]bool)
	for k := range m.Keys() {
		seen[k] = true
	}

	if len(seen) != 3 {
		t.Fatalf("Keys visited %d keys, want 3", len(seen))
	}
	for _, k := range []string{"a", "b", "c"} {
		if !seen[k] {
			t.Errorf("Keys did not visit key %q", k)
		}
	}
}

func TestMap_Values(t *testing.T) {
	t.Parallel()

	var m Map[string, int]
	m.Store("a", 1)
	m.Store("b", 2)
	m.Store("c", 3)

	seen := make(map[int]bool)
	for v := range m.Values() {
		seen[v] = true
	}

	if len(seen) != 3 {
		t.Fatalf("Values visited %d values, want 3", len(seen))
	}
	for _, v := range []int{1, 2, 3} {
		if !seen[v] {
			t.Errorf("Values did not visit value %d", v)
		}
	}
}

func TestMap_concurrent(t *testing.T) {
	t.Parallel()

	var m Map[int, int]
	var wg sync.WaitGroup

	const workers = 32
	const ops = 100

	wg.Add(workers)
	for w := range workers {
		go func() {
			defer wg.Done()
			base := w * ops
			for i := range ops {
				k := base + i
				m.Store(k, k)
				m.Load(k)
				m.LoadOrStore(k, k+1)
				m.Swap(k, k+2)
				m.Delete(k)
			}
		}()
	}
	wg.Wait()
}
