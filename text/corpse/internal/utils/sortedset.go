// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package utils

import (
	"cmp"
	"slices"
)

// SortedSet is a set of ordered elements. Writes are not concurrent-safe.
type SortedSet[T cmp.Ordered] struct {
	set []T
}

func (s *SortedSet[T]) Add(v T) {
	if len(s.set) == 0 {
		s.set = []T{v}
	}
	i, found := slices.BinarySearch(s.set, v)
	if found {
		return
	}
	s.set = slices.Insert(s.set, i, v)
}

func (s *SortedSet[T]) Remove(v T) {
	if len(s.set) == 0 {
		return
	}
	i, found := slices.BinarySearch(s.set, v)
	if !found {
		return
	}
	s.set = slices.Delete(s.set, i, i+1)
}

func (s *SortedSet[T]) Contains(v T) bool {
	if len(s.set) == 0 {
		return false
	}
	_, found := slices.BinarySearch(s.set, v)
	return found
}

func (s *SortedSet[T]) All() []T {
	return s.set
}

func (s *SortedSet[T]) Clear() {
	s.set = nil
}
