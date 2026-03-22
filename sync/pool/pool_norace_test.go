// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build !race

// sync.Pool drops a random fraction of Put values when the race detector is
// enabled (see sync.Pool.Put in the standard library), so these tests assume
// Put/Get round-trips are observed and only run without -race.

package pool

import "testing"

func TestPool_Put_Get_roundTrip(t *testing.T) {
	t.Parallel()

	var p Pool[*int]
	n := 7
	x := &n
	p.Put(x)

	if got := p.Get(); got != x {
		t.Fatalf("Get: want same pointer as Put, got %p want %p", got, x)
	}
}

func TestPool_Prepare_afterPut(t *testing.T) {
	t.Parallel()

	var p Pool[int]
	p.New = func() int { return 0 }
	p.Prepare = func(v int) int { return v + 1 }

	p.Put(5)
	if got := p.Get(); got != 6 {
		t.Fatalf("Get after Put: want 6, got %d", got)
	}
}
