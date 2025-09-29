// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package utils

import (
	"slices"
	"testing"
)

func TestAppendSorted(t *testing.T) {
	tests := []struct {
		name     string
		input    []int
		value    int
		expected []int
	}{
		{
			name:     "empty slice",
			input:    []int{},
			value:    5,
			expected: []int{5},
		},
		{
			name:     "insert at beginning",
			input:    []int{2, 3, 4},
			value:    1,
			expected: []int{1, 2, 3, 4},
		},
		{
			name:     "insert at end",
			input:    []int{1, 2, 3},
			value:    4,
			expected: []int{1, 2, 3, 4},
		},
		{
			name:     "insert in middle",
			input:    []int{1, 3, 4},
			value:    2,
			expected: []int{1, 2, 3, 4},
		},
		{
			name:     "insert duplicate start of slice",
			input:    []int{1, 2, 3},
			value:    1,
			expected: []int{1, 2, 3},
		},
		{
			name:     "insert duplicate end of slice",
			input:    []int{1, 2, 3},
			value:    3,
			expected: []int{1, 2, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set := &SortedSet[int]{set: slices.Clone(tt.input)}
			set.Add(tt.value)
			if len(set.set) != len(tt.expected) {
				t.Errorf("length mismatch: got %v, want %v", len(set.set), len(tt.expected))
				return
			}
			for i := range set.set {
				if set.set[i] != tt.expected[i] {
					t.Errorf("at index %d: got %v, want %v", i, set.set[i], tt.expected[i])
				}
			}
		})
	}
}

func TestRemoveSorted(t *testing.T) {
	tests := []struct {
		name     string
		input    []int
		value    int
		expected []int
	}{
		{
			name:     "empty slice",
			input:    []int{},
			value:    5,
			expected: []int{},
		},
		{
			name:     "remove from beginning",
			input:    []int{1, 2, 3, 4},
			value:    1,
			expected: []int{2, 3, 4},
		},
		{
			name:     "remove from end",
			input:    []int{1, 2, 3, 4},
			value:    4,
			expected: []int{1, 2, 3},
		},
		{
			name:     "remove from middle",
			input:    []int{1, 2, 3, 4},
			value:    2,
			expected: []int{1, 3, 4},
		},
		{
			name:     "remove non-existent value",
			input:    []int{1, 2, 3},
			value:    5,
			expected: []int{1, 2, 3},
		},
		{
			name:     "remove duplicate",
			input:    []int{1, 2, 2, 3},
			value:    2,
			expected: []int{1, 2, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set := &SortedSet[int]{set: slices.Clone(tt.input)}
			set.Remove(tt.value)
			if len(set.set) != len(tt.expected) {
				t.Errorf("length mismatch: got %v, want %v", len(set.set), len(tt.expected))
				return
			}
			for i := range set.set {
				if set.set[i] != tt.expected[i] {
					t.Errorf("at index %d: got %v, want %v", i, set.set[i], tt.expected[i])
				}
			}
		})
	}
}
