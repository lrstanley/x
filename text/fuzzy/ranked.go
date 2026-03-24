// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fuzzy

import (
	"strings"
	"unicode"
)

// NormalizerFunc is a function that normalizes a string for comparison.
type NormalizerFunc func(string) string

// DefaultNormalizer is the default normalizer function that converts the string
// to lowercase. Use an alternative normalizer function if you need to customize
// the normalization process, like stripping punctuation, whitespace, ansi sequences,
// etc.
func DefaultNormalizer(s string) string {
	return strings.ToLower(s)
}

type result[T any] struct {
	value T
	score int
}

// FindRanked returns a slice of values sorted by their similarity to the filter
// string.
//
// If the filter is empty, the original values are returned as-is.
func FindRanked(filter string, values []string, normalizeFn NormalizerFunc) []string {
	return FindRankedRow(filter, values, func(value string) []string {
		return []string{value}
	}, normalizeFn)
}

// FindRankedStrings returns a slice of values sorted by their similarity to the
// filter string.
//
// If the filter is empty, the original values are returned as-is.
func FindRankedStrings[T ~string](filter string, values [][]T, normalizeFn NormalizerFunc) [][]T {
	return FindRankedRow(filter, values, func(value []T) []string {
		strs := make([]string, len(value))
		for i, v := range value {
			strs[i] = string(v)
		}
		return strs
	}, normalizeFn)
}

// FindRankedRow returns a slice of values sorted by their similarity to the
// filter string. The valuesFn function is used to extract the strings to compare
// from each value.
//
// If the filter is empty, the original values are returned as-is.
func FindRankedRow[T any](filter string, values []T, valuesFn func(T) []string, normalizeFn func(string) string) []T {
	if normalizeFn == nil {
		normalizeFn = DefaultNormalizer
	}
	filter = normalizeFn(filter)
	if filter == "" {
		return values
	}

	var results []result[T]
	var strs []string
	var bestScore, score int

	for _, value := range values {
		strs = valuesFn(value)
		bestScore = -1

		for _, str := range strs {
			score = calculateScore(normalizeFn(str), filter)
			if score > bestScore {
				bestScore = score
			}
		}

		if bestScore > 0 {
			results = append(results, result[T]{
				value: value,
				score: bestScore,
			})
		}
	}

	// Sort by score (highest first).
	for i := range len(results) - 1 {
		for j := i + 1; j < len(results); j++ {
			if results[i].score < results[j].score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	// Extract values in order.
	result := make([]T, len(results))
	for i, r := range results {
		result[i] = r.value
	}

	return result
}

func calculateScore(text, query string) int {
	if query == "" {
		return 0
	}

	// Exact match gets highest score.
	if text == query {
		return 10000
	}

	// Check if query is a substring.
	if strings.Contains(text, query) {
		pos := strings.Index(text, query)
		// Bonus for early position.
		return 1000 - pos
	}

	// Fuzzy matching.
	score := 0
	queryIdx := 0
	lastMatch := -1
	consecutive := 0

	for i, char := range text {
		if queryIdx >= len(query) {
			break
		}

		if unicode.ToLower(char) == unicode.ToLower(rune(query[queryIdx])) {
			// Bonus for consecutive matches.
			if lastMatch == i-1 {
				consecutive++
				score += consecutive * 10
			} else {
				consecutive = 1
			}

			// Bonus for matching at word boundaries.
			if i == 0 || !unicode.IsLetter(rune(text[i-1])) {
				score += 50
			}

			// Bonus for matching uppercase letters.
			if unicode.IsUpper(char) {
				score += 30
			}

			lastMatch = i
			queryIdx++
		}
	}

	// Penalty for unmatched query characters.
	if queryIdx < len(query) {
		return 0
	}

	// Bonus for shorter text (more precise matches).
	score += 100 - len(text)

	return score
}
