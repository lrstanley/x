// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package stem

import (
	"iter"

	"github.com/kljensen/snowball/english"
	"github.com/lrstanley/x/text/corpse"
)

// NewTermFilter returns a term filter that stems the terms using the provided
// language. This is english only. Take a look at the source if you'd like to use
// a different language from the snowball package.
func NewTermFilter() corpse.TermFilter {
	return func(seq iter.Seq[string]) iter.Seq[string] {
		return func(yield func(string) bool) {
			for term := range seq {
				if !yield(english.Stem(term, true)) {
					return
				}
			}
		}
	}
}
