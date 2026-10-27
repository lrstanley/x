// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package codepoints

import (
	"errors"
	"slices"
	"sort"
	"testing"
	"unicode"
)

func TestCompare(t *testing.T) {
	tests := []struct {
		a, b Range
		want int
	}{
		{Range{First: 'A', Last: 'A'}, Range{First: 'B', Last: 'B'}, -1},
		{Range{First: 'B', Last: 'B'}, Range{First: 'A', Last: 'A'}, 1},
		{Range{First: 'A', Last: 'A'}, Range{First: 'A', Last: 'A'}, 0},
		{Range{First: 'A', Last: 'B'}, Range{First: 'A', Last: 'C'}, -1},
		{Range{First: 'A', Last: 'C'}, Range{First: 'A', Last: 'B'}, 1},
	}
	for _, tt := range tests {
		if got := Compare(tt.a, tt.b); got != tt.want {
			t.Errorf("Compare(%+v, %+v) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestRanges_sortInterface(t *testing.T) {
	in := Ranges{
		{First: 'c', Last: 'c'},
		{First: 'a', Last: 'z'},
		{First: 'a', Last: 'm'},
		{First: 'b', Last: 'b'},
	}
	want := Ranges{
		{First: 'a', Last: 'm'},
		{First: 'a', Last: 'z'},
		{First: 'b', Last: 'b'},
		{First: 'c', Last: 'c'},
	}
	cp := slices.Clone(in)
	sort.Sort(cp)
	if !slices.Equal(cp, want) {
		t.Fatalf("sort.Sort: got %+v, want %+v", cp, want)
	}
}

func TestParseSpec_ok(t *testing.T) {
	tests := []struct {
		spec string
		want rune
	}{
		{"U+0041", 'A'},
		{"u+0041", 'A'},
		{"  U+0041  ", 'A'},
		{"U+0020", ' '},
		{"U+10FFFF", unicode.MaxRune},
	}
	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			got, err := ParseSpec(tt.spec)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %q (%U), want %q (%U)", got, got, tt.want, tt.want)
			}
		})
	}
}

func TestParseSpec_errors(t *testing.T) {
	tests := []struct {
		spec string
		want *MalformedError
	}{
		{"", &MalformedError{Spec: ""}},
		{"   ", &MalformedError{Spec: ""}},
		{"0041", &MalformedError{Spec: "0041"}},
		{"x+0041", &MalformedError{Spec: "x+0041"}},
		{"U+41", &MalformedError{Spec: "U+41"}},
		{"U+DEADBEEF", &MalformedError{Spec: "U+DEADBEEF"}},
		{"U+GGGG", &MalformedError{Spec: "U+GGGG"}},
		{"U+110000", &MalformedError{Spec: "U+110000"}},
	}
	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			_, err := ParseSpec(tt.spec)
			var got *MalformedError
			if !errors.As(err, &got) {
				t.Fatalf("want *MalformedError, got %T: %v", err, err)
			}
			if got.Spec != tt.want.Spec {
				t.Fatalf("Spec: got %q, want %q", got.Spec, tt.want.Spec)
			}
		})
	}
}

func TestParseRange_ok(t *testing.T) {
	tests := []struct {
		spec      string
		wantFirst rune
		wantLast  rune
	}{
		{"U+0041", 'A', 'A'},
		{" U+0041 ", 'A', 'A'},
		{"U+0041-U+0045", 'A', 'E'},
		{"u+0041-U+0045", 'A', 'E'},
		{"U+0041-\tU+0045 ", 'A', 'E'},
	}
	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			first, last, err := ParseRange(tt.spec)
			if err != nil {
				t.Fatal(err)
			}
			if first != tt.wantFirst || last != tt.wantLast {
				t.Fatalf("got (%U,%U), want (%U,%U)", first, last, tt.wantFirst, tt.wantLast)
			}
		})
	}
}

func TestParseRange_emptyError(t *testing.T) {
	_, _, err := ParseRange("")
	var got *EmptyRangeError
	if !errors.As(err, &got) {
		t.Fatalf("want *EmptyRangeError, got %T: %v", err, err)
	}
	if got.Spec != "" {
		t.Fatalf("Spec: got %q", got.Spec)
	}

	_, _, err = ParseRange("   ")
	if !errors.As(err, &got) {
		t.Fatalf("want *EmptyRangeError, got %T: %v", err, err)
	}
}

func TestParseRange_reversedError(t *testing.T) {
	_, _, err := ParseRange("U+005A-U+0041")
	var got *ReversedRangeError
	if !errors.As(err, &got) {
		t.Fatalf("want *ReversedRangeError, got %T: %v", err, err)
	}
	if got.Spec != "U+005A-U+0041" {
		t.Fatalf("Spec: got %q", got.Spec)
	}
}

func TestParseRange_parseErrorPropagates(t *testing.T) {
	_, _, err := ParseRange("U+0041-U+ZZZZ")
	var got *MalformedError
	if !errors.As(err, &got) {
		t.Fatalf("want *MalformedError, got %T: %v", err, err)
	}
}

func TestParseRanges_ok(t *testing.T) {
	got, err := ParseRanges("U+0041,U+0043-U+0045 , U+007A ")
	if err != nil {
		t.Fatal(err)
	}
	want := []Range{
		{First: 'A', Last: 'A'},
		{First: 'C', Last: 'E'},
		{First: 'z', Last: 'z'},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseRanges_errors(t *testing.T) {
	t.Run("empty segment", func(t *testing.T) {
		_, err := ParseRanges("U+0041,,U+0042")
		var got *EmptyRangeError
		if !errors.As(err, &got) {
			t.Fatalf("want *EmptyRangeError, got %T: %v", err, err)
		}
	})

	t.Run("only commas", func(t *testing.T) {
		_, err := ParseRanges(",")
		var got *EmptyRangeError
		if !errors.As(err, &got) {
			t.Fatalf("want *EmptyRangeError, got %T: %v", err, err)
		}
	})
}

func TestFormat_roundTripWithParseSpec(t *testing.T) {
	for _, r := range []rune{' ', 'A', 'Я', '🙂', unicode.MaxRune} {
		s := Format(r)
		got, err := ParseSpec(s)
		if err != nil {
			t.Fatalf("ParseSpec(%q): %v", s, err)
		}
		if got != r {
			t.Fatalf("ParseSpec(Format(%U)): got %U", r, got)
		}
	}
}
