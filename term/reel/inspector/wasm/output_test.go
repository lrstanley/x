// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"testing"

	"github.com/lrstanley/x/term/reel"
)

func TestOutputBoundaries(t *testing.T) {
	x := &inspector{events: []reel.Event{
		reel.Output{Data: []byte("one")},
		reel.Output{Data: []byte("two")},
		reel.Output{Data: []byte("\x1b[38:2::255:")},
		reel.Output{Data: []byte("0:10mred")},
	}}
	x.tokenize()
	for i, want := range []string{"one", "two"} {
		if got := x.tokens[i]; len(got) != 1 || got[0].Raw != want || got[0].Offset != 0 {
			t.Errorf("event %d tokens = %+v, want text %q", i, got, want)
		}
	}
	seq := x.tokenFields(2)
	if len(seq) != 1 || !seq[0].Continues || seq[0].Tag != "SGR" || len(seq[0].Fields) != 1 || seq[0].Fields[0].Value != "foreground #ff000a" {
		t.Errorf("split SGR = %+v", seq)
	}
	if got := x.tokens[3]; len(got) != 1 || got[0].Raw != "red" || got[0].Offset != 5 {
		t.Errorf("text after split SGR = %+v", got)
	}
}
