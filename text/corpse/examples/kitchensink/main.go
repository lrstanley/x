// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/coder/hnsw"
	"github.com/lrstanley/x/text/corpse"
	"github.com/lrstanley/x/text/lemm"
	"github.com/lrstanley/x/text/stem"
)

var documents = map[string]string{
	"brown-fox":     "The quick brown fox jumps over the lazy dog.",
	"yellow-fox":    "The slow yellow fox jumps over the fast cat.",
	"foo-bar":       "Foo bar@baz",
	"walking-store": "I was walking to the store. Alphabetically, working, testing, and so on.",
	"lorem-ipsum":   "Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.",
}

func main() {
	// The vector size will heavily depend on the corpus size and the number of
	// unique terms in the corpus. This example actually doesn't need more than 25,
	// however, you'd likely set this higher. OpenAI uses 1536 for
	// text-embedding-3-small, and 3072 for text-embedding-3-large.
	vectorSize := 25

	// Initialize a corpus. This keeps track of the terms found across all documents,
	// and how frequently they appear across all documents.
	corp := corpse.New(
		corpse.WithMaxVectorSize(vectorSize),

		// Term filters help simplify each document into a smaller set of terms.
		corpse.WithTermFilters(
			// Example of using a term filter to lemmatize terms. This is a common
			// approach to reduce the number of unique terms in the corpus. It's a
			// predefined database of words and their simplified meaning.
			//
			// Example: "agreed" -> "agree", "first" -> "1", "alphabetically" -> "alphabet".
			lemm.NewTermFilter(),

			// Example of using a term filter to "stem" terms. This is a common
			// approach to reduce the number of unique terms in the corpus. Note
			// that this is similar to lemmatization, but it's generic and essentially
			// "simplifies" the word to its root form. It is more generic than
			// lemmatization, and works with a much larger corpus, but may not be
			// as accurate.
			//
			// Example: "running" -> "run", "organization" -> "organiz".
			stem.NewTermFilter(),

			// Example of using a term filter to remove stop words. Stop words are
			// words that you know will likely appear in most documents, and you
			// want to explicitly remove them from the corpus. The stop words you
			// use will likely depend on the context of your corpus. E.g. JSON files,
			// YAML files, specific data structures, plain english text, etc.
			corpse.StopTermFilter([]string{
				"the", "and", "is", "in", // etc.
			}),
		),

		// Prune hooks are invoked after the entire corpus has been indexed, and
		// help to remove any terms that are either super common, or super rare.
		// E.g. "the" is super common, and a mis-spelling of "apple" like "appel"
		// would be super rare. You'll want to tune these thresholds to your use
		// case so you don't accidentally remove terms that are important to your
		// use case.
		corpse.WithPruneHooks(
			// Prune terms that appear in more than 85% of documents.
			corpse.PruneMoreThanPercent(85),

			// Prune terms that appear in only 1 document. Not enabled here because
			// our corpus is so small.
			// corpse.PruneLessThan(2),
		),
	)

	// Index all documents. All documents must be indexed before creating vectors
	// and searching the graph.
	for _, doc := range documents {
		corp.IndexDocument(doc)
	}

	// If you want to see what terms make up the corpus, you can use this:
	var terms []string
	for term, freq := range corp.GetTermFrequency() {
		terms = append(terms, fmt.Sprintf("%s(%d)", term, freq))
	}
	sort.Strings(terms)
	fmt.Printf("terms: %s\n", strings.Join(terms, ", "))

	// Use an in-memory HNSW (Hierarchical Navigable Small Worlds) database, a
	// nearest neighbor search algorithm. Note that this library also supports
	// exporting to, and importing from, disk, if you want to persist the graph
	// across restarts. Though, you would also need to persist the corpus as well,
	// as it's required to produce vectors for searching the graph.
	graph := hnsw.NewGraph[string]()
	graph.M = vectorSize // Make sure this is always the same as the vector size.

	// Create vectors for each document.
	for id, doc := range documents {
		graph.Add(hnsw.MakeNode(id, corp.CreateVector(doc)))
	}

	// Search the graph.
	qvector := corp.CreateVector("yellow fox")
	results := graph.Search(qvector, 2) // 2 results.

	for _, result := range results {
		fmt.Printf("content[%s]: %s\n", result.Key, documents[result.Key])
	}
}
