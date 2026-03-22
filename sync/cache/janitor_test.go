// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cache

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestJanitor(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	janitor := newJanitor(ctx)

	checkDone := make(chan struct{})
	janitor.done = checkDone

	calledClean := new(int64(0))
	go janitor.run(time.Millisecond, func() { atomic.AddInt64(calledClean, 1) })

	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case <-checkDone:
	case <-time.After(time.Second):
		t.Fatalf("failed to call done channel")
	}

	got := atomic.LoadInt64(calledClean)
	if got <= 1 {
		t.Fatalf("failed to call clean callback in janitor: %d", got)
	}
}

func TestCacheJanitor(t *testing.T) {
	c := New(
		t.Context(),
		WithJanitorInterval[string, int](100*time.Millisecond),
	)

	c.Set("1", 10, WithExpiration(10*time.Millisecond))
	c.Set("2", 20, WithExpiration(20*time.Millisecond))
	c.Set("3", 30, WithExpiration(30*time.Millisecond))

	<-time.After(300 * time.Millisecond)

	keys := c.Keys()
	if len(keys) != 0 {
		t.Errorf("want entries is empty but got %d", len(keys))
	}
}
