// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cache

import (
	"sync"
	"testing"
)

func TestNumberConcurrentIncr(t *testing.T) {
	nc := NewNumber[string, int](t.Context())
	nc.Set("counter", 0)

	var wg sync.WaitGroup

	for range 100 {
		wg.Go(func() {
			_ = nc.Increment("counter", 1)
		})
	}

	wg.Wait()

	if counter, _ := nc.Get("counter"); counter != 100 {
		t.Errorf("want %v but got %v", 100, counter)
	}
}

func TestNumberConcurrentDecr(t *testing.T) {
	nc := NewNumber[string, int](t.Context())
	nc.Set("counter", 100)

	var wg sync.WaitGroup

	for range 100 {
		wg.Go(func() {
			_ = nc.Decrement("counter", 1)
		})
	}

	wg.Wait()

	if counter, _ := nc.Get("counter"); counter != 0 {
		t.Errorf("want %v but got %v", 0, counter)
	}
}
