// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package xansi

import (
	"bytes"
	"testing"
)

func TestStripANSI(t *testing.T) {
	in := "hello \x1b[31mred\x1b[0m\n"
	want := "hello red\n"
	if got := StripANSI(in); got != want {
		t.Fatalf("string: got %q, want %q", got, want)
	}
	bin := []byte(in)
	bwant := []byte(want)
	if got := StripANSI(bin); !bytes.Equal(got, bwant) {
		t.Fatalf("[]byte: got %q, want %q", got, bwant)
	}
}

func TestStripSpinners(t *testing.T) {
	// "⣾" is the first entry in spinnerReplacements.
	if got := StripSpinners("a⣾b"); got != "a?b" {
		t.Fatalf("string: got %q, want %q", got, "a?b")
	}
	if got := StripSpinners([]byte("a⣾b")); string(got) != "a?b" {
		t.Fatalf("[]byte: got %q, want %q", string(got), "a?b")
	}
}

func TestStripPrivateUse(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "nerd_font_pua",
			in:   "hello 😀 \ue627 #️⃣\n",
			want: "hello 😀 ? #️⃣\n",
		},
		{
			name: "only_ascii",
			in:   "plain",
			want: "plain",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := StripPrivateUse(tc.in); got != tc.want {
				t.Fatalf("string: got %q, want %q", got, tc.want)
			}
			if got := StripPrivateUse([]byte(tc.in)); string(got) != tc.want {
				t.Fatalf("[]byte: got %q, want %q", string(got), tc.want)
			}
		})
	}
}

func TestIsPrivateUse(t *testing.T) {
	tests := []struct {
		r    rune
		want bool
	}{
		{'a', false},
		{'😀', false},
		{0xE000, true},
		{0xF8FF, true},
		{0xDFFF, false},
		{0xF0000, true},
		{0xFFFFD, true},
		{0x100000, true},
		{0x10FFFD, true},
		{0x10FFFE, false},
	}
	for _, tc := range tests {
		if got := IsPrivateUse(tc.r); got != tc.want {
			t.Fatalf("IsPrivateUse(%U) = %v, want %v", tc.r, got, tc.want)
		}
	}
}

func TestInRanges(t *testing.T) {
	ranges := [][2]rune{{'a', 'c'}}
	if !InRanges('b', ranges...) {
		t.Fatal("InRanges: expected 'b' in [a,c]")
	}
	if InRanges('d', ranges...) {
		t.Fatal("InRanges: expected 'd' not in [a,c]")
	}
}

func TestNormalizeCRLF(t *testing.T) {
	if got := NormalizeCRLF("a\r\nb\nc"); got != "a\nb\nc" {
		t.Fatalf("string: got %q, want %q", got, "a\nb\nc")
	}
	if got := NormalizeCRLF([]byte("a\r\nb\nc")); string(got) != "a\nb\nc" {
		t.Fatalf("[]byte: got %q, want %q", string(got), "a\nb\nc")
	}
}

func TestEscapeESC(t *testing.T) {
	if got := EscapeESC("pre\x1b[0mpost"); got != `pre\x1b[0mpost` {
		t.Fatalf("string: got %q, want %q", got, `pre\x1b[0mpost`)
	}
	if got := EscapeESC([]byte("pre\x1b[0mpost")); string(got) != `pre\x1b[0mpost` {
		t.Fatalf("[]byte: got %q, want %q", string(got), `pre\x1b[0mpost`)
	}
}
