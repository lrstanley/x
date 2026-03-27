// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package lemm provides English lemmatization as a term filter for text pipelines
// built on iterators of strings.
package lemm

import (
	"iter"
	"sync"

	"github.com/aaaton/golem/v4"
	"github.com/aaaton/golem/v4/dicts/en"
)

var (
	lemmatizer *golem.Lemmatizer
	initOnce   sync.Once
)

func NewTermFilter() func(iter.Seq[string]) iter.Seq[string] {
	initOnce.Do(func() {
		var err error
		lemmatizer, err = golem.New(en.New())
		if err != nil {
			panic(err)
		}
	})

	return func(seq iter.Seq[string]) iter.Seq[string] {
		return func(yield func(string) bool) {
			for term := range seq {
				if !yield(lemmatizer.Lemma(term)) {
					return
				}
			}
		}
	}
}
