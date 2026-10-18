// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package codepoints

import "testing"

func TestErrorStrings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  error
		want string
	}{
		{&EmptyRangeError{Spec: "x"}, "codepoints: empty range: x"},
		{&ReversedRangeError{Spec: "a-b"}, "codepoints: reversed range: a-b"},
		{&MalformedError{Spec: "bad"}, "codepoints: malformed codepoint: bad"},
	}
	for _, tt := range tests {
		if got := tt.err.Error(); got != tt.want {
			t.Fatalf("Error() = %q, want %q", got, tt.want)
		}
	}
}
