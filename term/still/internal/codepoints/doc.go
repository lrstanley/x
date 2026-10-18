// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package codepoints parses Unicode codepoint literals written in U+ notation
// (for example U+0041 for 'A'). [ParseSpec] accepts a single literal; [ParseRanges]
// splits on commas and parses each fragment with [ParseRange] (a single codepoint
// or U+XXXX-U+YYYY inclusive range). Values must lie within unicode.MaxRune.
package codepoints
