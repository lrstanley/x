// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package lemm

import (
	"iter"
	"sync"

	"github.com/aaaton/golem/v4"
	"github.com/aaaton/golem/v4/dicts/en"
	"github.com/lrstanley/x/text/corpse"
)

var (
	lemmatizer *golem.Lemmatizer
	initOnce   sync.Once
)

func NewTermFilter() corpse.TermFilter {
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
