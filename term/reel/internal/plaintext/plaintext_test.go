// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package plaintext

import (
	"strings"
	"testing"
	"unicode"
	"unsafe"
)

// maxRunes is the limit the tests read text with.
const maxRunes = 512

func TestLine(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"plain title", "plain title"},
		{"漢字 ✔ ok", "漢字 ✔ ok"},
		{"\u202egnp.exe\u202c", "gnp.exe"},                         // RLO, PDF
		{"a\u202ab\u202bc\u202dd", "abcd"},                         // LRE, RLE, LRO
		{"\u2066invoice\u2069\u2067x\u2068y", "invoicexy"},         // isolates
		{"zero\u200bwidth\u200c\u200d\u200e\u200f!", "zerowidth!"}, // ZWSP, ZWNJ, ZWJ, LRM, RLM
		{"word\u2060joiner\u2061\u2062\u2063\u2064", "wordjoiner"}, // word joiner, invisible operators
		{"\ufeffbom\u00adsoft\u061calm\u180e", "bomsoftalm"},       // BOM, soft hyphen, ALM, MVS
		{"tag\U000e0041\U000e007f", "tag"},                         // tag characters
		{"cgj\u034fhf\u115f\u1160\u3164\uffa0", "cgjhf"},           // CGJ, Hangul fillers
		{"kv\u17b4\u17b5x\u2065y\ufff0z\U000e0fff", "kvxyz"},       // Khmer vowels, reserved ignorables
		{"a\x00b\x07c\x1bd\x7fe\u0085f\u009bg", "a b c d e f g"},   // C0, DEL, C1
		{"\x1b[31mred\x1b[0m", "[31mred [0m"},                      // escapes are not interpreted
		{"line\nbreak\r\nand\ttab\u2028sep\u2029para", "line break and tab sep para"},
		{"  lead  and   trail  ", "lead and trail"},
		{"trail ", "trail"},
		{"\u00a0nbsp\u3000ideographic\u2003em", "nbsp ideographic em"},
		{"a \u200b b", "a b"},
		{"a\u200b b", "a b"},
		{"bad\xffutf8\xc3", "badutf8"},
		{"non\ufffechar\U0001ffff\ufdd0", "nonchar"},
		{"<b>markup</b> &amp;", "<b>markup</b> &amp;"},
		{"\u202e\u200b\x00 \t", ""},
		{strings.Repeat("ab ", maxRunes), strings.TrimSpace(strings.Repeat("ab ", maxRunes)[:maxRunes])},
		{strings.Repeat("\u200b", 2*maxRunes) + "x", ""},
	} {
		got := Line(tc.in, maxRunes)
		if got != tc.want {
			t.Errorf("Line(%q) = %q, want %q", tc.in, got, tc.want)
		}
		for _, r := range got {
			if unicode.Is(unicode.Cf, r) || unicode.IsControl(r) || (r != ' ' && unicode.IsSpace(r)) {
				t.Errorf("Line(%q) kept %U", tc.in, r)
			}
		}
		if strings.Contains(got, "  ") || strings.TrimSpace(got) != got {
			t.Errorf("Line(%q) = %q has stray spaces", tc.in, got)
		}
		if got == tc.in && got != "" && unsafe.StringData(got) != unsafe.StringData(tc.in) {
			t.Errorf("Line(%q) copied text it returns as is", tc.in)
		}
	}
}
