// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package codepoints

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Range is an inclusive Unicode scalar value interval [First, Last].
type Range struct {
	First rune
	Last  rune
}

// Compare defines a total order on [Range]: ascending by [Range.First], then
// by [Range.Last]. It returns -1 if a sorts before b, 1 if after, and 0 if a
// and b have the same ordering key.
func Compare(a, b Range) int {
	switch {
	case a.First < b.First:
		return -1
	case a.First > b.First:
		return 1
	case a.Last < b.Last:
		return -1
	case a.Last > b.Last:
		return 1
	default:
		return 0
	}
}

// Ranges is a slice of [Range] values. It implements sort.Interface using
// [Compare] so that sort.Sort orders by ascending First, then ascending Last.
type Ranges []Range

func (p Ranges) Len() int           { return len(p) }
func (p Ranges) Less(i, j int) bool { return Compare(p[i], p[j]) < 0 }
func (p Ranges) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

// ParseRanges splits spec on commas and parses each fragment with [ParseRange].
//
// Errors returned include any produced by [ParseRange]: [EmptyRangeError] for
// an empty fragment (including "", consecutive commas, or a trailing comma);
// [MalformedError] when a fragment’s endpoint fails [ParseSpec]; and
// [ReversedRangeError] when the range endpoints are out of order.
func ParseRanges(spec string) ([]Range, error) {
	out := make([]Range, 0)
	for part := range strings.SplitSeq(spec, ",") {
		first, last, err := ParseRange(part)
		if err != nil {
			return nil, err
		}
		out = append(out, Range{First: first, Last: last})
	}
	return out, nil
}

// ParseRange trims whitespace around spec. If spec contains '-', the substring
// before and after the first hyphen are parsed as codepoints with [ParseSpec];
// otherwise spec is a single literal and Last equals First.
//
// Errors returned:
//   - [EmptyRangeError]: spec is empty after trimming.
//   - [MalformedError]: either substring passed to [ParseSpec] is invalid
//     (including an empty endpoint such as "U+0041-" or "-U+0041").
//   - [ReversedRangeError]: both endpoints parse OK but First > Last.
func ParseRange(spec string) (first, last rune, err error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return 0, 0, &EmptyRangeError{Spec: spec}
	}
	start, end, ranged := strings.Cut(spec, "-")
	first, err = ParseSpec(start)
	if err != nil {
		return 0, 0, err
	}
	if ranged {
		last, err = ParseSpec(end)
		if err != nil {
			return 0, 0, err
		}
	} else {
		last = first
	}
	if first > last {
		return 0, 0, &ReversedRangeError{Spec: spec}
	}
	return first, last, nil
}

// ParseSpec interprets spec as U+ followed by hexadecimal digits (ASCII
// case-insensitive U+ prefix). After trimming, the string must be 6–8 runes
// total so the digits encode one scalar value within unicode.MaxRune.
//
// Errors returned:
//   - [MalformedError]: empty after trim; missing or wrong U+ prefix; wrong
//     length; non-hex digits; overflow from strconv parsing; or value negative
//     or greater than unicode.MaxRune.
func ParseSpec(spec string) (rune, error) {
	spec = strings.TrimSpace(spec)
	if len(spec) < 6 || len(spec) > 8 || !strings.HasPrefix(strings.ToUpper(spec), "U+") {
		return 0, &MalformedError{Spec: spec}
	}
	v, err := strconv.ParseInt(spec[2:], 16, 32)
	if err != nil || v < 0 || v > unicode.MaxRune {
		return 0, &MalformedError{Spec: spec}
	}
	return rune(v), nil
}

// Format returns r in canonical U+ notation using uppercase hexadecimal with at
// least four digits.
func Format(r rune) string {
	return fmt.Sprintf("U+%04X", r)
}
