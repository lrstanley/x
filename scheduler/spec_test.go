// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package scheduler

import (
	"testing"
	"time"
)

func TestSpecSchedule_Next_dailyMidnightUTC(t *testing.T) {
	t.Parallel()

	s, err := Parse("0 0 * * *")
	if err != nil {
		t.Fatal(err)
	}
	loc := time.UTC
	before := time.Date(2024, 6, 15, 10, 30, 0, 0, loc)
	next := s.Next(before)
	want := time.Date(2024, 6, 16, 0, 0, 0, 0, loc)
	if !next.Equal(want) {
		t.Fatalf("Next = %v, want %v", next, want)
	}
}

func TestSpecSchedule_Next_minuteStep(t *testing.T) {
	t.Parallel()

	s, err := Parse("*/15 * * * *")
	if err != nil {
		t.Fatal(err)
	}
	loc := time.UTC
	before := time.Date(2024, 1, 1, 8, 7, 30, 0, loc)
	next := s.Next(before)
	want := time.Date(2024, 1, 1, 8, 15, 0, 0, loc)
	if !next.Equal(want) {
		t.Fatalf("Next = %v, want %v", next, want)
	}
}

func TestSpecSchedule_Next_namedMonth(t *testing.T) {
	t.Parallel()

	s, err := Parse("0 0 1 jan *")
	if err != nil {
		t.Fatal(err)
	}
	loc := time.UTC
	before := time.Date(2023, 12, 15, 0, 0, 0, 0, loc)
	next := s.Next(before)
	want := time.Date(2024, 1, 1, 0, 0, 0, 0, loc)
	if !next.Equal(want) {
		t.Fatalf("Next = %v, want %v", next, want)
	}
}

func TestSpecSchedule_String(t *testing.T) {
	t.Parallel()

	s, err := Parse("0 12 * * mon-fri")
	if err != nil {
		t.Fatal(err)
	}
	if got := s.String(); got != "0 12 * * mon-fri" {
		t.Fatalf("String() = %q", got)
	}
}
