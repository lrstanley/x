// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package corpse

import (
	"iter"
	"maps"
	"sync"

	"github.com/chewxy/math32"
	"github.com/lrstanley/x/text/corpse/internal/utils"
)

// Corpus stores term frequencies across all documents.
type Corpus struct {
	maxVectorSize int
	tokenizer     Tokenizer
	termFilters   []TermFilter
	pruneHooks    []PruneHook

	mu        sync.RWMutex
	termFreq  map[string]int           // How many times a term appears in ALL documents.
	termIndex *utils.SortedSet[string] // Set used for consistent vector positions.
	documents int                      // How many documents have been indexed.
	hasPruned bool

	seenTermPool utils.Pool[map[string]struct{}]
	termFreqPool utils.Pool[map[string]int]
}

// New creates a new corpus with the given options.
func New(options ...Option) *Corpus {
	c := &Corpus{
		maxVectorSize: 256,
		tokenizer:     DefaultTokenizer,
		termFreq:      make(map[string]int),
		termIndex:     &utils.SortedSet[string]{},
		seenTermPool: utils.Pool[map[string]struct{}]{
			New: func() map[string]struct{} { return make(map[string]struct{}) },
			Prepare: func(v map[string]struct{}) map[string]struct{} {
				for k := range v {
					delete(v, k)
				}
				return v
			},
		},
		termFreqPool: utils.Pool[map[string]int]{
			New: func() map[string]int { return make(map[string]int) },
			Prepare: func(v map[string]int) map[string]int {
				for k := range v {
					delete(v, k)
				}
				return v
			},
		},
	}
	for _, option := range options {
		option(c)
	}
	return c
}

// Reset resets the corpus to its initial state.
func (c *Corpus) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.termFreq = make(map[string]int)
	c.termIndex.Clear()
	c.documents = 0
}

// Prune runs all prune hooks, removing terms of less importance from the corpus.
// This is automatically ran by [Corpus.CreateVector] if there are any new documents
// that have been indexed since the last prune. Run it manually if you don't plan to
// invoke [Corpus.CreateVector] immediately after indexing all documents. Do not
// run this until you have indexed all documents.
//
// This is concurrent-safe.
func (c *Corpus) Prune() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.hasPruned || c.documents == 0 || len(c.pruneHooks) == 0 {
		return
	}

	snapshot := maps.Clone(c.termFreq)

	for _, hook := range c.pruneHooks {
		for _, term := range hook(c.documents, snapshot) {
			delete(c.termFreq, term)
			c.termIndex.Remove(term)
		}
	}

	c.hasPruned = true
}

// GetUsedCapacity returns the percentage of the corpus capacity that is used.
// You can use this to determine if you are getting close to the max vector size.
// If you do go above capacity, all vectors will be calculated with the first X
// terms (sorted), where X is the max vector size, and you will lose corpus
// information. Make sure to call [Corpus.Prune] before checking this.
func (c *Corpus) GetUsedCapacity() (percent int) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	percent = int(float32(len(c.termFreq)) / float32(c.maxVectorSize) * 100)
	return percent
}

// IndexDocument indexes a document, calculating occurrences of each term. Note that
// you should call this for ALL documents before creating vectors for your documents
// (or search queries).
//
// This is concurrent-safe.
func (c *Corpus) IndexDocument(text string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	seenTerms := c.seenTermPool.Get()
	defer c.seenTermPool.Put(seenTerms)

	for term := range c.tokenize(text) {
		if _, ok := seenTerms[term]; !ok {
			c.termFreq[term]++
			seenTerms[term] = struct{}{}
			c.termIndex.Add(term)
		}
	}
	c.documents++
	c.hasPruned = false
}

// GetTermFrequency returns a snapshot of the term frequencies. Note that
// because [CreateVector] calls [Corpus.Prune] before creating vectors, if you
// invoke this before [CreateVector], you will receive terms that might not have
// been pruned yet by [PruneHook]s. Call [Corpus.Prune] manually before this
// function first in that case.
func (c *Corpus) GetTermFrequency() map[string]int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return maps.Clone(c.termFreq)
}

// GetDocumentCount returns the number of documents that have been indexed.
func (c *Corpus) GetDocumentCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.documents
}

// CreateVector creates a TF-IDF vector for the given text. Note that for documents,
// before generating a vector and adding it to a graph, ALL documents must be indexed
// first. Note that the returned vector will not be padded. See [CreatePaddedVector]
// if you need a constant-sized vector.
//
// This will automatically call [Corpus.Prune] if there are any new documents that
// have been indexed since the last prune.
//
// This is concurrent-safe.
func (c *Corpus) CreateVector(text string) []float32 {
	c.Prune()

	c.mu.RLock()
	defer c.mu.RUnlock()

	// Count terms in this document.
	termFreq := c.termFreqPool.Get()
	defer c.termFreqPool.Put(termFreq)

	totalTerms := 0

	for term := range c.tokenize(text) {
		termFreq[term]++
		totalTerms++
	}

	// Create TF-IDF vector.
	//
	// This is not 1-for-1 how TF-IDF is normally calculated, but there are a few good reasons:
	// 1. We want to avoid extreme values -- add 1 to the value of IDF to avoid completely ignoring
	//    terms that occur in all documents.
	// 2. Some implementations use IDF smoothing, which prevents division by zero. Don't really need
	//    that here.
	//
	// This follows patterns by Python libraries like scikit-learn.
	vector := make([]float32, min(len(c.termIndex.All()), c.maxVectorSize))
	for i, term := range c.termIndex.All()[:len(vector)] {
		tf := float32(termFreq[term]) / float32(totalTerms)
		idf := math32.Log(float32(c.documents)/float32(c.termFreq[term])) + 1
		vector[i] = tf * idf
	}

	// Normalize vector.
	var magnitude float32
	for _, val := range vector {
		magnitude += val * val
	}
	magnitude = math32.Sqrt(magnitude)
	if magnitude > 0 {
		for i := range vector {
			vector[i] /= magnitude
		}
	}

	return vector
}

// CreatePaddedVector creates a vector with the maximum potential vector size,
// padding with zeros if the vector is smaller. Not needed unless the graph you
// use to compare vectors does not support sparse vectors, as it will use more
// memory.
//
// This is concurrent-safe.
func (c *Corpus) CreatePaddedVector(text string) []float32 {
	vector := c.CreateVector(text)
	if len(vector) < c.maxVectorSize {
		vector = append(vector, make([]float32, c.maxVectorSize-len(vector))...)
	}
	return vector
}

// tokenize is a helper function that applies the term filters (if any) to the
// tokenizer iterator.
func (c *Corpus) tokenize(text string) iter.Seq[string] {
	seq := c.tokenizer(text)
	for _, filter := range c.termFilters {
		seq = filter(seq)
	}
	return seq
}

// IsNoMatchVector returns true if the vector didn't match any terms.
func IsNoMatchVector(vector []float32) bool {
	for _, val := range vector {
		if val != 0.0 {
			return false
		}
	}
	return true
}

// VectorSubCount returns the number of non-zero values in the vector.
func VectorSubCount(vector []float32) (count int) {
	for _, val := range vector {
		if val != 0.0 {
			count++
		}
	}
	return count
}
