// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package pool

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

func TestPool_Get_usesNewWhenEmpty(t *testing.T) {
	t.Parallel()

	var p Pool[int]
	p.New = func() int { return 42 }

	if got := p.Get(); got != 42 {
		t.Fatalf("Get: want 42, got %d", got)
	}
}

func TestPool_Get_zeroWhenNewNil(t *testing.T) {
	t.Parallel()

	var p Pool[int]

	if got := p.Get(); got != 0 {
		t.Fatalf("Get: want 0, got %d", got)
	}
}

func TestPool_Prepare(t *testing.T) {
	t.Parallel()

	var p Pool[int]
	p.New = func() int { return 1 }
	p.Prepare = func(v int) int { return v * 10 }

	if got := p.Get(); got != 10 {
		t.Fatalf("Get: want 10, got %d", got)
	}
}

func TestPool_concurrent(t *testing.T) {
	t.Parallel()

	var p Pool[string]
	var n atomic.Int64
	p.New = func() string {
		return strconv.FormatInt(n.Add(1)-1, 10)
	}

	const workers = 32
	const iters = 100

	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for range iters {
				s := p.Get()
				p.Put(s)
			}
		}()
	}
	wg.Wait()
}
