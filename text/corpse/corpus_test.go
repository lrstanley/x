// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package corpse

import (
	"testing"
)

var sampleData = []struct {
	id         string
	text       string
	tokenized  []string
	withFilter []string
}{
	{
		id:         "brown-fox",
		text:       "The quick brown fox jumps over the lazy dog.",
		tokenized:  []string{"the", "quick", "brown", "fox", "jumps", "over", "the", "lazy", "dog"},
		withFilter: []string{"tHE", "qUICK", "bROWN", "fOX", "jUMPS", "oVER", "tHE", "lAZY", "dOG"},
	},
	{
		id:         "yellow-fox",
		text:       "The slow yellow fox jumps over the fast cat.",
		tokenized:  []string{"the", "slow", "yellow", "fox", "jumps", "over", "the", "fast", "cat"},
		withFilter: []string{"tHE", "sLOW", "yELLOW", "fOX", "jUMPS", "oVER", "tHE", "fAST", "cAT"},
	},
	{
		id:         "foo-bar",
		text:       "Foo bar@baz",
		tokenized:  []string{"foo", "bar", "baz"},
		withFilter: []string{"fOO", "bAR", "bAZ"},
	},
	{
		id:         "walking-store",
		text:       "I was walking to the store. Alphabetically, working, testing, and so on.",
		tokenized:  []string{"i", "was", "walking", "to", "the", "store", "alphabetically", "working", "testing", "and", "so", "on"},
		withFilter: []string{"i", "wAS", "wALKING", "tO", "tHE", "sTORE", "aLPHABETICALLY", "wORKING", "tESTING", "aND", "sO", "oN"},
	},
	{
		id:         "lorem-ipsum",
		text:       "Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.",
		tokenized:  []string{"lorem", "ipsum", "dolor", "sit", "amet", "consectetur", "adipiscing", "elit", "sed", "do", "eiusmod", "tempor", "incididunt", "ut", "labore", "et", "dolore", "magna", "aliqua"},
		withFilter: []string{"lOREM", "iPSUM", "dOLOR", "sIT", "aMET", "cONSECTETUR", "aDIPISCING", "eLIT", "sED", "dO", "eIUSMOD", "tEMPOR", "iNCIDIDUNT", "uT", "lABORE", "eT", "dOLORE", "mAGNA", "aLIQUA"},
	},
}

func TestCorpus(t *testing.T) {
	corp := New()
	for _, s := range sampleData {
		corp.IndexDocument(s.text)
	}

	for _, sample := range sampleData {
		vector := corp.CreateVector(sample.text)
		if len(vector) < 1 {
			t.Errorf("expected vector for %q to be non-empty", sample.text)
		}

		hasFoundValue := false
		for _, v := range vector {
			if v != 0.0 {
				hasFoundValue = true
				break
			}
		}
		if !hasFoundValue {
			t.Errorf("expected vector for %q to have at least one non-zero value", sample.text)
		}
	}

	nonExistant := corp.CreateVector("non-existent")
	for _, v := range nonExistant {
		if v != 0.0 {
			t.Errorf("expected vector for non-existent text to be all zeros, got %v", nonExistant)
		}
	}
}

func BenchmarkCorpus(b *testing.B) {
	query := "yellow fox"
	corp := New()
	for range 100 {
		for _, s := range sampleData {
			corp.IndexDocument(s.text)
		}
	}
	for b.Loop() {
		corp.CreateVector(query)
	}
}

func TestIsNoMatchVector(t *testing.T) {
	corp := New()
	for _, s := range sampleData {
		corp.IndexDocument(s.text)
	}

	tests := []struct {
		name     string
		text     string
		expected bool
	}{
		{
			name:     "non-existent terms",
			text:     "xyzabc123",
			expected: true,
		},
		{
			name:     "single term match",
			text:     "lorem",
			expected: false,
		},
		{
			name:     "multiple term match",
			text:     "lorem ipsum dolor",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vector := corp.CreateVector(tt.text)
			result := IsNoMatchVector(vector)
			if result != tt.expected {
				t.Errorf("IsNoMatchVector(%v) = %v, want %v", vector, result, tt.expected)
			}
		})
	}
}
