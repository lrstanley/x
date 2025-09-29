# Corpse: Crude text embedding using TF-IDF algo, in pure Go

## :sparkles: Features

- **Text Vectorization**: Convert text documents into numerical vectors using TF-IDF (Term Frequency-Inverse Document Frequency).
- **Term Processing**:
  - Built-in tokenization for text processing.
  - Support for extensible term filtering (stop words, lemmatization, stemming, etc).
  - Configurable term pruning to remove common or rare terms (minimum and maximum document frequency).
- **Vector Management**:
  - Configurable vector size limits.
  - Automatic term frequency tracking.
  - Efficient memory usage with object pooling.
- **Search Capabilities**:
  - Simple integration with HNSW (Hierarchical Navigable Small Worlds) for fast similarity search, and
    other search algorithms.

## :warning: Limitations

- Designed for addition-only vectorization. If you want to remove or update documents, you'll need to
  re-index the entire corpus. This does mean reduced memory usage, however.
- Designed primarily for in-memory vectorization. If you need clustering, or advanced features, use
  a proper vector database (and something like LLM-based embedding).
- I'm not an expert in text embedding, so there may be better ways to do this.

---

## :gear: Usage

```console
$ go get github.com/lrstanley/x/text/corpse

# if you want lemmatization or stemming. note that these support english only.
# see the source if you want to use a different language.
$ go get github.com/lrstanley/x/text/lemm
$ go get github.com/lrstanley/x/text/stem
```

```go
package main

import (
    "fmt"
    "github.com/coder/hnsw"
    "github.com/lrstanley/x/text/corpse"
    "github.com/lrstanley/x/text/lemm"
    "github.com/lrstanley/x/text/stem"
)

func main() {
    vectorSize := 25

    // Initialize a corpus with custom options.
    corp := corpse.New(
        corpse.WithMaxVectorSize(vectorSize),
        corpse.WithTermFilters(
            lemm.NewTermFilter(), // Add lemmatization.
            stem.NewTermFilter(), // Add stemming.
            corpse.StopTermFilter([]string{ // Remove common stop words.
                "the", "and", "is", "in", "etc...",
            }),
        ),
        corpse.WithPruneHooks(
            // Remove terms that appear in more than 85% of documents.
            corpse.PruneMoreThanPercent(85),
        ),
    )

    // Index your documents
    documents := map[string]string{
        "brown-fox":     "The quick brown fox jumps over the lazy dog.",
        "yellow-fox":    "The slow yellow fox jumps over the fast cat.",
        "foo-bar":       "Foo bar@baz",
        "walking-store": "I was walking to the store. Alphabetically, working, testing, and so on.",
        "lorem-ipsum":   "Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.",
    }

    for _, doc := range documents {
        corp.IndexDocument(doc)
    }

    // Create a search graph.
    graph := hnsw.NewGraph[string]()
    graph.M = vectorSize // Should match your vector size.

    // Add documents to the graph.
    for id, doc := range documents {
        graph.Add(hnsw.MakeNode(id, corp.CreateVector(doc)))
    }

    // Search for similar documents.
    query := "yellow fox"
    results := graph.Search(corp.CreateVector(query), 2)

    for _, result := range results {
        fmt.Println(result.Key)
    }
}
```

For more advanced examples, check out the [examples directory](examples/).

## :books: References

- [Term Frequency-Inverse Document Frequency (TF-IDF)](https://www.geeksforgeeks.org/understanding-tf-idf-term-frequency-inverse-document-frequency/)
  - https://scikit-learn.org/stable/modules/generated/sklearn.feature_extraction.text.TfidfTransformer.html
- [Hierarchical Navigable Small Worlds (HNSW)](https://github.com/coder/hnsw)
