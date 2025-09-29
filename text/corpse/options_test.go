// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package corpse

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestDefaultTokenizer(t *testing.T) {
	corp := New()
	for _, s := range sampleData {
		got := slices.Collect(corp.tokenize(s.text))
		if !reflect.DeepEqual(got, s.tokenized) {
			t.Errorf("tokenized %q: %v != %v", s.text, slices.Collect(corp.tokenize(s.text)), s.tokenized)
		}
	}
}

func TestTermFilter(t *testing.T) {
	corp := New(
		// result: "The" (tokenizer) -> "THE" (upper) -> "tHE" (lowerFirstChar)
		WithTermFilters(
			TermFilterFunc(strings.ToUpper),
			TermFilterFunc(func(in string) string {
				return strings.Replace(in, in[0:1], strings.ToLower(in[0:1]), 1)
			}),
		),
	)
	for _, s := range sampleData {
		t.Run(s.id, func(t *testing.T) {
			got := slices.Collect(corp.tokenize(s.text))
			if !reflect.DeepEqual(got, s.withFilter) {
				t.Errorf("tokenized %q: %v != %v", s.text, got, s.withFilter)
			}
		})
	}
}

func TestStopTermFilter(t *testing.T) {
	stopWords := []string{"the", "a", "an", "and", "or", "but", "in", "on", "at", "to", "for", "of", "with", "by"}
	corp := New(
		WithTermFilters(StopTermFilter(stopWords)),
	)

	testCases := []struct {
		name     string
		text     string
		expected []string
	}{
		{
			name:     "basic stop word removal",
			text:     "The quick brown fox jumps over the lazy dog",
			expected: []string{"quick", "brown", "fox", "jumps", "over", "lazy", "dog"},
		},
		{
			name:     "multiple stop words",
			text:     "A cat and a dog in the house with the mouse",
			expected: []string{"cat", "dog", "house", "mouse"},
		},
		{
			name:     "no stop words",
			text:     "Quick brown fox jumps",
			expected: []string{"quick", "brown", "fox", "jumps"},
		},
		{
			name:     "all stop words",
			text:     "The and in on at",
			expected: nil,
		},
		{
			name:     "mixed case stop words",
			text:     "The QUICK brown fox AND the lazy DOG",
			expected: []string{"quick", "brown", "fox", "lazy", "dog"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := slices.Collect(corp.tokenize(tc.text))
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("StopTermFilter(%q) = %v, want %v", tc.text, got, tc.expected)
			}
		})
	}
}

func TestPrune(t *testing.T) {
	cases := []struct {
		name      string
		hooks     []PruneHook
		documents []string
		pruned    []string
		notPruned []string
	}{
		{
			name:  "PruneLessThan",
			hooks: []PruneHook{PruneLessThan(2)},
			documents: []string{
				"The quick brown fox jumps over the lazy dog",
				"The quick brown fox jumps over the lazy dog",
				"The quick yellow jumps over the lazy dog",
				"The quick yellow jumps over the lazy dog",
				"unique terms",
			},
			pruned:    []string{"unique", "terms"},
			notPruned: []string{"the", "quick", "brown", "yellow", "fox", "jumps", "over", "lazy", "dog"},
		},
		{
			name:  "PruneMoreThan",
			hooks: []PruneHook{PruneMoreThan(2)},
			documents: []string{
				"The quick brown fox jumps over the lazy dog",
				"The quick brown fox jumps over the lazy dog",
				"The quick yellow jumps over the lazy dog",
				"The quick yellow jumps over the lazy dog",
				"unique terms",
			},
			pruned:    []string{"the", "quick", "jumps", "over", "lazy", "dog"},
			notPruned: []string{"unique", "terms", "brown", "yellow", "fox"},
		},
		{
			name:  "PruneLessThanPercent",
			hooks: []PruneHook{PruneLessThanPercent(50)},
			documents: []string{
				"The quick brown fox jumps over the lazy dog",
				"The quick brown frog jumps over the lazy dog",
				"The quick yellow jumps over the lazy dog",
				"The quick yellow jumps over the lazy dog",
				"unique terms",
			},
			pruned:    []string{"unique", "terms", "fox", "frog"},
			notPruned: []string{"the", "quick", "brown", "yellow", "jumps", "over", "lazy", "dog"},
		},
		{
			name:  "PruneMoreThanPercent",
			hooks: []PruneHook{PruneMoreThanPercent(50)},
			documents: []string{
				"The quick brown fox jumps over the lazy dog",
				"The quick brown frog jumps over the lazy dog",
				"The quick yellow jumps over the lazy dog",
				"The quick yellow jumps over the lazy dog",
				"unique terms",
			},
			pruned:    []string{"the", "quick", "jumps", "over", "lazy", "dog"},
			notPruned: []string{"unique", "terms", "brown", "yellow", "fox", "frog"},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			corp := New(WithPruneHooks(tt.hooks...))
			for _, doc := range tt.documents {
				corp.IndexDocument(doc)
			}

			beforeTotalTerms := len(corp.GetTermFrequency())
			if beforeTotalTerms < 1 {
				t.Errorf("expected at least 1 term, got %d", beforeTotalTerms)
			}

			corp.Prune()
			after := corp.GetTermFrequency()

			for _, term := range tt.pruned {
				if _, ok := after[term]; ok {
					t.Errorf("term %q should be pruned", term)
				}
			}

			for _, term := range tt.notPruned {
				if _, ok := after[term]; !ok {
					t.Errorf("term %q should not be pruned", term)
				}
			}
		})
	}
}
