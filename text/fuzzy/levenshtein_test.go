// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package fuzzy

import "testing"

func TestComputeDistance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a, b string
		want int
	}{
		{"both empty", "", "", 0},
		{"left empty", "", "abc", 3},
		{"right empty", "abc", "", 3},
		{"equal", "kitten", "kitten", 0},
		{"classic", "kitten", "sitting", 3},
		{"single insert", "a", "ab", 1},
		{"single delete", "ab", "a", 1},
		{"single substitute", "a", "b", 1},
		{"unicode runes", "café", "cafe", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ComputeDistance(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("ComputeDistance(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
			// Symmetry.
			gotRev := ComputeDistance(tt.b, tt.a)
			if gotRev != tt.want {
				t.Errorf("ComputeDistance(%q, %q) = %d, want %d (symmetry)", tt.b, tt.a, gotRev, tt.want)
			}
		})
	}
}

func TestShortestDistance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		match  string
		values []string
		want   string
	}{
		{"empty values", "foo", nil, ""},
		{"empty values slice", "foo", []string{}, ""},
		{"single candidate", "kitten", []string{"sitting"}, "sitting"},
		{"picks closest", "kitten", []string{"sitting", "kitchen", "xxxxxxx"}, "kitchen"},
		{"tie keeps first", "ab", []string{"ac", "ad"}, "ac"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ShortestDistance(tt.match, tt.values)
			if got != tt.want {
				t.Errorf("ShortestDistance(%q, %v) = %q, want %q", tt.match, tt.values, got, tt.want)
			}
		})
	}
}
