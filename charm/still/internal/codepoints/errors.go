// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package codepoints

import "fmt"

// EmptyRangeError means a range specification was empty after trimming whitespace.
type EmptyRangeError struct {
	Spec string
}

func (e *EmptyRangeError) Error() string {
	return fmt.Sprintf("codepoints: empty range: %s", e.Spec)
}

// ReversedRangeError means the first codepoint of an explicit range was greater
// than the last.
type ReversedRangeError struct {
	Spec string
}

func (e *ReversedRangeError) Error() string {
	return fmt.Sprintf("codepoints: reversed range: %s", e.Spec)
}

// MalformedError means a codepoint literal did not match the U+XXXXXXXX pattern
// accepted by [ParseSpec].
type MalformedError struct {
	Spec string
}

func (e *MalformedError) Error() string {
	return fmt.Sprintf("codepoints: malformed codepoint: %s", e.Spec)
}
