// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"testing"
)

func TestSplitInput(t *testing.T) {
	tests := []struct{ in, complete, tail string }{
		{"abc", "abc", ""},
		{"a\x1b[", "a", "\x1b["},
		{"a\x1b[1;5", "a", "\x1b[1;5"},
		{"a\x1b[A", "a\x1b[A", ""},
		{"a\x1bO", "a", "\x1bO"},
		{"a\x1b", "a\x1b", ""},
		{"x\x1b[200~paste", "x", "\x1b[200~paste"},
		{"x\x1b[200~paste\x1b[201~", "x\x1b[200~paste\x1b[201~", ""},
		{"é"[:1], "", "é"[:1]},
		{"a世"[:3], "a", "世"[:2]},
	}
	for _, tt := range tests {
		complete, tail := splitInput([]byte(tt.in))
		if string(complete) != tt.complete || string(tail) != tt.tail {
			t.Errorf("splitInput(%q) = %q, %q; want %q, %q", tt.in, complete, tail, tt.complete, tt.tail)
		}
	}
}
