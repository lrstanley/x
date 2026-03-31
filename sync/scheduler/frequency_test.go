// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package scheduler

import (
	"testing"
	"time"
)

func TestEvery_clampsBelowOneSecond(t *testing.T) {
	t.Parallel()

	s := Every(500 * time.Millisecond)
	if s.Delay != time.Second {
		t.Fatalf("Delay = %v, want 1s", s.Delay)
	}
}

func TestEvery_truncatesSubSecond(t *testing.T) {
	t.Parallel()

	s := Every(1500 * time.Millisecond)
	if s.Delay != time.Second {
		t.Fatalf("Delay = %v, want 1s", s.Delay)
	}
}

func TestEvery_preservesWholeSeconds(t *testing.T) {
	t.Parallel()

	s := Every(90 * time.Second)
	if s.Delay != 90*time.Second {
		t.Fatalf("Delay = %v, want 90s", s.Delay)
	}
}

func TestFrequencySchedule_String(t *testing.T) {
	t.Parallel()

	s := Every(2 * time.Hour)
	if got := s.String(); got != "@every 2h0m0s" {
		t.Fatalf("String() = %q", got)
	}
}

func TestFrequencySchedule_Next(t *testing.T) {
	t.Parallel()

	s := Every(10 * time.Second)
	loc := time.UTC
	start := time.Date(2024, 3, 15, 12, 0, 30, 123456789, loc)
	next := s.Next(start)
	want := time.Date(2024, 3, 15, 12, 0, 40, 0, loc)
	if !next.Equal(want) {
		t.Fatalf("Next = %v, want %v", next, want)
	}
}
