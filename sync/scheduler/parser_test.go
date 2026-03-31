// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package scheduler

import (
	"strings"
	"testing"
	"time"
)

func TestParse_empty(t *testing.T) {
	t.Parallel()

	_, err := Parse("")
	if err == nil {
		t.Fatal("expected error for empty spec")
	}
}

func TestParse_fieldCount(t *testing.T) {
	t.Parallel()

	_, err := Parse("0 * * *")
	if err == nil {
		t.Fatal("expected error for wrong field count")
	}
	if !strings.Contains(err.Error(), "5 fields") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParse_descriptors(t *testing.T) {
	t.Parallel()

	tests := []string{
		"@yearly",
		"@annually",
		"@monthly",
		"@weekly",
		"@daily",
		"@midnight",
		"@hourly",
	}
	for _, spec := range tests {
		t.Run(spec, func(t *testing.T) {
			t.Parallel()

			s, err := Parse(spec)
			if err != nil {
				t.Fatal(err)
			}
			if s == nil {
				t.Fatal("schedule is nil")
			}
			if got := s.String(); got != spec {
				t.Fatalf("String() = %q, want %q", got, spec)
			}
		})
	}
}

func TestParse_every(t *testing.T) {
	t.Parallel()

	s, err := Parse("@every 45m")
	if err != nil {
		t.Fatal(err)
	}
	fs, ok := s.(FrequencySchedule)
	if !ok {
		t.Fatalf("want FrequencySchedule, got %T", s)
	}
	if fs.Delay != 45*time.Minute {
		t.Fatalf("Delay = %v, want 45m", fs.Delay)
	}
}

func TestParse_every_invalidDuration(t *testing.T) {
	t.Parallel()

	_, err := Parse("@every not-a-duration")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParse_unrecognizedDescriptor(t *testing.T) {
	t.Parallel()

	_, err := Parse("@unknown")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParse_CRON_TZ(t *testing.T) {
	t.Parallel()

	s, err := Parse("CRON_TZ=UTC 0 0 * * *")
	if err != nil {
		t.Fatal(err)
	}
	ss, ok := s.(*SpecSchedule)
	if !ok {
		t.Fatalf("want *SpecSchedule, got %T", s)
	}
	if ss.Location.String() != "UTC" {
		t.Fatalf("Location = %v, want UTC", ss.Location)
	}
}

func TestParse_CRON_TZ_badLocation(t *testing.T) {
	t.Parallel()

	_, err := Parse("CRON_TZ=Nowhere/Invalid 0 0 * * *")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParse_standardFiveField(t *testing.T) {
	t.Parallel()

	s, err := Parse("15 14 1 * *")
	if err != nil {
		t.Fatal(err)
	}
	ss, ok := s.(*SpecSchedule)
	if !ok {
		t.Fatalf("want *SpecSchedule, got %T", s)
	}
	if ss.Source != "15 14 1 * *" {
		t.Fatalf("Source = %q", ss.Source)
	}
}

func TestParse_invalidMinute(t *testing.T) {
	t.Parallel()

	_, err := Parse("99 * * * *")
	if err == nil {
		t.Fatal("expected error for invalid minute")
	}
}
