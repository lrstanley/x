// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"sort"

	"github.com/lrstanley/x/term/reel"
)

// tokenize splits all output, concatenated, into tokens, so a sequence
// split across events is read whole, as the terminal reads it.
func (x *inspector) tokenize() {
	var stream []byte
	var starts, owners []int
	for i, ev := range x.events {
		if o, ok := ev.(reel.Output); ok {
			starts, owners = append(starts, len(stream)), append(owners, i)
			stream = append(stream, o.Data...)
		}
	}
	x.stream, x.starts = stream, make(map[int]int, len(owners))
	for j, i := range owners {
		x.starts[i] = starts[j]
	}
	x.tokens = make(map[int][]token, len(owners))
	tokenize(stream, func(at int, tok token) {
		end := at + tok.Len
		for at < end {
			j := sort.Search(len(starts), func(j int) bool { return starts[j] > at }) - 1
			next := end
			if j+1 < len(starts) {
				next = min(next, starts[j+1])
			}
			part := tok
			part.Offset = at - starts[j]
			if tok.Group == groupText {
				part.Len, part.Raw = next-at, escape(stream[at:next])
			} else {
				part.Continues = next < end
				next = end
			}
			x.tokens[owners[j]] = append(x.tokens[owners[j]], part)
			at = next
		}
	})
}
