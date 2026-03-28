// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package rate

import (
	"context"
	"testing"
	"time"
)

func TestNewKeyWindowLimiter_panicsOnBadLimit(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for limit < 1")
		}
	}()
	_ = NewKeyWindowLimiter(0, time.Second, NewLocalCounter(time.Second))
}

func TestNewKeyWindowLimiter_panicsOnBadWindow(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for zero window")
		}
	}()
	_ = NewKeyWindowLimiter(5, 0, NewLocalCounter(time.Second))
}

func TestNewKeyWindowLimiter_panicsOnNilCounter(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for nil counter")
		}
	}()
	_ = NewKeyWindowLimiter(5, time.Second, nil)
}

func TestKeyWindowLimiter_Allow(t *testing.T) {
	t.Parallel()

	window := 2 * time.Second
	lim := NewKeyWindowLimiter(3, window, NewLocalCounter(window))

	for i := range 3 {
		ok, err := lim.Allow("user:1")
		if err != nil {
			t.Fatalf("Allow %d: %v", i, err)
		}
		if !ok {
			t.Fatalf("Allow %d should be permitted", i)
		}
	}

	ok, err := lim.Allow("user:1")
	if err != nil {
		t.Fatalf("Allow 4: %v", err)
	}
	if ok {
		t.Fatal("fourth Allow should be denied")
	}
}

func TestKeyWindowLimiter_Allow_separateKeys(t *testing.T) {
	t.Parallel()

	window := 2 * time.Second
	lim := NewKeyWindowLimiter(2, window, NewLocalCounter(window))

	for range 2 {
		if ok, err := lim.Allow("a"); err != nil || !ok {
			t.Fatalf("Allow(a): ok=%v err=%v", ok, err)
		}
	}
	if ok, _ := lim.Allow("a"); ok {
		t.Fatal("key 'a' should be exhausted")
	}

	if ok, err := lim.Allow("b"); err != nil || !ok {
		t.Fatal("key 'b' should still be allowed")
	}
}

func TestKeyWindowLimiter_AllowN(t *testing.T) {
	t.Parallel()

	window := 2 * time.Second
	lim := NewKeyWindowLimiter(5, window, NewLocalCounter(window))

	ok, err := lim.AllowN("k", 3)
	if err != nil || !ok {
		t.Fatalf("AllowN(3) should succeed: ok=%v err=%v", ok, err)
	}

	ok, err = lim.AllowN("k", 3)
	if err != nil {
		t.Fatalf("AllowN(3) err: %v", err)
	}
	if ok {
		t.Fatal("AllowN(3) should fail (would put rate at 6 > 5)")
	}

	ok, err = lim.AllowN("k", 2)
	if err != nil || !ok {
		t.Fatalf("AllowN(2) should succeed: ok=%v err=%v", ok, err)
	}
}

func TestKeyWindowLimiter_Wait(t *testing.T) {
	t.Parallel()

	window := 200 * time.Millisecond
	lim := NewKeyWindowLimiter(2, window, NewLocalCounter(window))

	for range 2 {
		if ok, err := lim.Allow("k"); err != nil || !ok {
			t.Fatalf("Allow: ok=%v err=%v", ok, err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	start := time.Now()
	if err := lim.Wait(ctx, "k"); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 50*time.Millisecond {
		t.Fatalf("expected some wait time, got %v", elapsed)
	}
}

func TestKeyWindowLimiter_Wait_contextCancel(t *testing.T) {
	t.Parallel()

	window := 5 * time.Second
	lim := NewKeyWindowLimiter(1, window, NewLocalCounter(window))

	if ok, err := lim.Allow("k"); err != nil || !ok {
		t.Fatalf("Allow: ok=%v err=%v", ok, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := lim.Wait(ctx, "k"); err == nil {
		t.Fatal("expected error from context timeout")
	}
}

func TestKeyWindowLimiter_Status(t *testing.T) {
	t.Parallel()

	window := 2 * time.Second
	lim := NewKeyWindowLimiter(5, window, NewLocalCounter(window))

	allowed, r, err := lim.Status("k")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !allowed {
		t.Fatal("should be allowed with 0 events")
	}
	if r != 0 {
		t.Fatalf("rate should be 0, got %v", r)
	}

	for range 3 {
		_, _ = lim.Allow("k")
	}

	allowed, r, err = lim.Status("k")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !allowed {
		t.Fatal("should be allowed at rate 3/5")
	}
	if r < 2.9 || r > 3.1 {
		t.Fatalf("rate should be ~3, got %v", r)
	}

	_, _ = lim.Allow("k")
	_, _ = lim.Allow("k")

	allowed, r, err = lim.Status("k")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if allowed {
		t.Fatal("should not be allowed at rate 5/5")
	}
	if r < 4.9 || r > 5.1 {
		t.Fatalf("rate should be ~5, got %v", r)
	}
}
