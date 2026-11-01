// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fx

import "testing"

func TestRoll(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		from, to string
		at       float64
		want     string
	}{
		{"start shows the old number", "1,021", "1,058", 0, "1,021"},
		{"end is untouched", "1,021", "1,058", 1, "1,058"},
		{"past the end is untouched", "1,021", "1,058", 1.2, "1,058"},
		{"last digit starts first and the way down is shorter", "1,021", "1,058", 0.25, "1,039"},
		{"digits to the left follow", "1,021", "1,058", 0.37, "1,049"},
		{"counting up", "10", "14", 0.25, "12"},
		{"wrapping past nine", "9", "0", 0.25, "0"},
		{"wrapping past nine, one step", "9", "0", 0, "9"},
		{"wrapping below zero", "1", "9", 0.25, "0"},
		{"unchanged digits stay", "1,058", "1,058", 0.3, "1,058"},
		{"gaining a place", "999", "1,000", 0, "1,999"},
		{"separators are skipped", "1,021", "10,058", 0, "11,021"},
		{"losing a place", "1,000", "999", 0, "000"},
		{"empty from", "", "42", 0.3, "42"},
		{"no digits", "n/a", "42", 0.3, "42"},
		{"not a number", "1,021", "up", 0.3, "up"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := textLayer(tt.to)
			Roll{From: tt.from}.Apply(s, tt.at)
			if got := rowText(s, 0); got != tt.want {
				t.Errorf("%q to %q at t=%v shows %q, want %q", tt.from, tt.to, tt.at, got, tt.want)
			}
		})
	}

	t.Run("unchanged digits keep their colors", func(t *testing.T) {
		t.Parallel()
		s := textLayer("1,058")
		Roll{From: "1,021"}.Apply(s, 0.3)
		for x, want := range []bool{false, false, false, true, true} {
			if moving := s.Cell(x, 0).Style.Fg != nil; moving != want {
				t.Errorf("digit %d tinted = %v, want %v", x, moving, want)
			}
		}
	})

	t.Run("aligned right in a wider node", func(t *testing.T) {
		t.Parallel()
		s := textLayer("   58     ")
		Roll{From: "21"}.Apply(s, 0)
		if got := rowText(s, 0); got != "   21     " {
			t.Fatalf("shows %q", got)
		}
	})

	t.Run("rows are separate numbers", func(t *testing.T) {
		t.Parallel()
		s := textLayer("58\n99")
		Roll{From: "21"}.Apply(s, 0)
		if got := rows(s); got != "21\n21" {
			t.Fatalf("shows %q", got)
		}
	})

	t.Run("rolling digits are dimmed", func(t *testing.T) {
		t.Parallel()
		s := textLayer("58")
		Roll{From: "21"}.Apply(s, 0.2)
		if c := s.Cell(1, 0); c.Style.Fg == nil || rgba(c.Style.Fg) == rgba(env.Fg) {
			t.Fatal("a rolling digit kept its color")
		}
		s = textLayer("58")
		Roll{From: "21"}.Apply(s, 0)
		if c := s.Cell(1, 0); c.Style.Fg != nil {
			t.Fatal("a digit that has not started to roll changed color")
		}
	})

	t.Run("every digit settles by the end", func(t *testing.T) {
		t.Parallel()
		to := "9876543210"
		s := textLayer(to)
		Roll{From: "0123456789"}.Apply(s, 0.999)
		if got := rowText(s, 0); got != to {
			t.Fatalf("shows %q just before the end, want %q", got, to)
		}
	})
}

func BenchmarkRollFullScreen(b *testing.B) {
	// Every row of the frame is a number, with ten digits to roll from.
	benchmarkLayer(b, Roll{From: "1,021,994"})
}
