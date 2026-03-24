// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package fuzzy

import "unicode/utf8"

// ComputeDistance returns the Levenshtein edit distance between a and b.
//
// It compares Unicode code points (runes). It does not normalize combining
// characters; see https://go.dev/blog/normalization and golang.org/x/text/unicode/norm.
func ComputeDistance(a, b string) int {
	if len(a) == 0 {
		return utf8.RuneCountInString(b)
	}
	if len(b) == 0 {
		return utf8.RuneCountInString(a)
	}
	if a == b {
		return 0
	}

	s := []rune(a)
	t := []rune(b)

	// Keep the shorter sequence as the inner dimension so we use O(min(n,m)) space.
	if len(s) > len(t) {
		s, t = t, s
	}

	prev := make([]int, len(s)+1)
	for j := range prev {
		prev[j] = j
	}
	curr := make([]int, len(s)+1)

	for i := 1; i <= len(t); i++ {
		curr[0] = i
		ti := t[i-1]
		for j := 1; j <= len(s); j++ {
			sj := s[j-1]
			cost := 0
			if ti != sj {
				cost = 1
			}
			// Delete from t, insert into t, or match / substitute.
			curr[j] = min(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(s)]
}

// ShortestDistance returns the element of values with minimum Levenshtein distance
// to match. If several values tie for the minimum distance, the earliest in the
// slice wins. If values is empty, it returns "".
func ShortestDistance(match string, values []string) string {
	if len(values) == 0 {
		return ""
	}
	best := values[0]
	bestDist := ComputeDistance(match, best)
	for i := 1; i < len(values); i++ {
		d := ComputeDistance(match, values[i])
		if d < bestDist {
			bestDist = d
			best = values[i]
		}
	}
	return best
}
