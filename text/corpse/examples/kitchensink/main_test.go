// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"maps"
	"os"
	"slices"
	"sort"
	"testing"

	"github.com/coder/hnsw"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/lrstanley/x/text/corpse"
)

func TestMain(m *testing.M) {
	v := m.Run()
	snaps.Clean(m)
	os.Exit(v)
}

func TestCorpus(t *testing.T) {
	graph := hnsw.NewGraph[string]()
	graph.M = 25

	corp := corpse.New()
	for _, doc := range documents {
		corp.IndexDocument(doc)
	}
	snaps.MatchSnapshot(t, corp.GetTermFrequency())

	sortedKeys := slices.Collect(maps.Keys(documents))
	sort.Strings(sortedKeys)

	for _, key := range sortedKeys {
		vector := corp.CreateVector(documents[key])
		snaps.MatchSnapshot(t, vector)
		graph.Add(hnsw.MakeNode(key, vector))
	}

	cases := []struct {
		query    string
		expected []string
	}{
		{
			query:    "yellow fox",
			expected: []string{"yellow-fox", "brown-fox"},
		},
		{
			query:    "brown fox",
			expected: []string{"brown-fox", "yellow-fox"},
		},
		{
			query:    "foo bar",
			expected: []string{"foo-bar"},
		},
	}

	for _, tt := range cases {
		t.Run(tt.query, func(t *testing.T) {
			vector := corp.CreateVector(tt.query)
			snaps.MatchSnapshot(t, vector)

			results := graph.Search(vector, len(tt.expected))
			snaps.MatchSnapshot(t, results)
		})
	}
}
