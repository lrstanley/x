// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package corpse

import (
	"iter"
	"strings"
	"unicode"
)

type Option func(*Corpus)

// WithMaxVectorSize sets the maximum potential vector size.
func WithMaxVectorSize(size int) Option {
	return func(c *Corpus) {
		c.maxVectorSize = size
	}
}

type Tokenizer func(text string) iter.Seq[string]

func WithTokenizer(tokenizer Tokenizer) Option {
	return func(c *Corpus) {
		c.tokenizer = tokenizer
	}
}

func DefaultTokenizer(text string) iter.Seq[string] {
	return func(yield func(string) bool) {
		var token strings.Builder
		for _, r := range strings.ToLower(text) {
			if unicode.IsLetter(r) || unicode.IsNumber(r) {
				token.WriteRune(r)
			} else if token.Len() > 0 {
				if !yield(token.String()) {
					return
				}
				token.Reset()
			}
		}
		if token.Len() > 0 {
			yield(token.String())
		}
	}
}

type TermFilter func(iter.Seq[string]) iter.Seq[string]

// TermFilterFunc is a helper function that creates a TermFilter from a function
// that transforms a single term. If the filter returns an empty string, the term
// is skipped.
func TermFilterFunc(filter func(string) string) TermFilter {
	return func(seq iter.Seq[string]) iter.Seq[string] {
		var v string
		return func(yield func(string) bool) {
			for term := range seq {
				v = filter(term)
				if v != "" && !yield(v) {
					return
				}
			}
		}
	}
}

// WithTermFilters allows adding filters to the tokenizer iterator. For example to add:
// - stopword removal
// - lemmatization
// - stemming
//
// Order of operations: tokenizer -> filter (1st call) -> filter (2nd call) -> ... -> filter (n-th call)
func WithTermFilters(filters ...TermFilter) Option {
	return func(c *Corpus) {
		c.termFilters = filters
	}
}

// StopTermFilter removes stop words from the tokenizer iterator (i.e. ignores them).
func StopTermFilter(words []string) TermFilter {
	wordMap := make(map[string]struct{}, len(words))
	for _, word := range words {
		wordMap[word] = struct{}{}
	}
	return func(seq iter.Seq[string]) iter.Seq[string] {
		return func(yield func(string) bool) {
			for term := range seq {
				if _, ok := wordMap[term]; !ok {
					if !yield(term) {
						return
					}
				}
			}
		}
	}
}

// WithMinLenTermFilter removes terms that are shorter than the given length.
func WithMinLenTermFilter(minLen int) TermFilter {
	return func(seq iter.Seq[string]) iter.Seq[string] {
		return func(yield func(string) bool) {
			for term := range seq {
				if len(term) >= minLen && !yield(term) {
					return
				}
			}
		}
	}
}

// WithMaxLenTermFilter removes terms that are longer than the given length.
func WithMaxLenTermFilter(maxLen int) TermFilter {
	return func(seq iter.Seq[string]) iter.Seq[string] {
		return func(yield func(string) bool) {
			for term := range seq {
				if len(term) <= maxLen && !yield(term) {
					return
				}
			}
		}
	}
}

type PruneHook func(documents int, termFreq map[string]int) (toRemove []string)

// WithPruneHooks allows adding hooks, which are ran before vectorization, that remove
// terms from the corpus. This can be used to remove terms that are either in too
// few documents, or too many documents, to reduce the sizze of the corpus.
func WithPruneHooks(hooks ...PruneHook) Option {
	return func(c *Corpus) {
		c.pruneHooks = hooks
	}
}

// PruneLessThan is a [PruneHook] that removes terms that appear in less than the
// given number of documents. Keep in mind that if you happen to have very few
// documents, this may remove all terms.
func PruneLessThan(count int) PruneHook {
	return func(_ int, termFreq map[string]int) (toRemove []string) {
		for term, freq := range termFreq {
			if freq < count {
				toRemove = append(toRemove, term)
			}
		}
		return
	}
}

// PruneLessThanPercent is a [PruneHook] that removes terms that appear in less than
// the given percentage of documents. Keep in mind that if you happen to have very few
// documents, this may remove all terms.
func PruneLessThanPercent(percent int) PruneHook {
	return func(documents int, termFreq map[string]int) (toRemove []string) {
		for term, freq := range termFreq {
			if freq < documents*percent/100 {
				toRemove = append(toRemove, term)
			}
		}
		return
	}
}

// PruneMoreThan is a [PruneHook] that removes terms that appear in more than the
// given number of documents. Keep in mind that if you happen to have very few
// documents, this may remove all terms.
func PruneMoreThan(count int) PruneHook {
	return func(_ int, termFreq map[string]int) (toRemove []string) {
		for term, freq := range termFreq {
			if freq > count {
				toRemove = append(toRemove, term)
			}
		}
		return
	}
}

// PruneMoreThanPercent is a [PruneHook] that removes terms that appear in more than
// the given percentage of documents. Keep in mind that if you happen to have very few
// documents, this may remove all terms.
func PruneMoreThanPercent(percent int) PruneHook {
	return func(documents int, termFreq map[string]int) (toRemove []string) {
		for term, freq := range termFreq {
			if freq > documents*percent/100 {
				toRemove = append(toRemove, term)
			}
		}
		return
	}
}
