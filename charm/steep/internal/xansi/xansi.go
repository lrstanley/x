// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package xansi normalizes terminal output for stable snapshots and assertions:
// stripping ANSI, spinner glyphs, and private-use code points, normalizing
// newlines, and escaping ESC for readable diffs.
package xansi

import (
	"bytes"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

// All of the casting in this file isn't very performant or elegant, but this
// package is used only in the scope of tests.

// StripANSI removes ANSI escape sequences and control sequences from input,
// preserving plain text. T must be string or []byte; other types panic.
func StripANSI[T string | []byte](input T) T {
	switch v := any(input).(type) {
	case string:
		return T(ansi.Strip(v))
	case []byte:
		return T(ansi.Strip(string(v)))
	default:
		panic("unsupported type")
	}
}

var spinnerReplacements = [...]string{
	"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷", "⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏",
	"⢄", "⢂", "⢁", "⡁", "⡈", "⡐", "⡠",
	"█", "▓", "▒", "░",
	"∙", "●",
	"🌍", "🌎", "🌏",
	"🌑", "🌒", "🌓", "🌔", "🌕", "🌖", "🌗", "🌘",
	"🙈", "🙉", "🙊",
	"▱", "▰",
	"☱", "☲", "☴",
}

// StripSpinners replaces known spinner and progress glyphs with a single '?'
// so animated or frame-dependent output compares consistently in tests.
func StripSpinners[T string | []byte](input T) T {
	switch v := any(input).(type) {
	case string:
		for _, replacement := range &spinnerReplacements {
			v = strings.ReplaceAll(v, replacement, "?")
		}
		return T(v)
	case []byte:
		for _, replacement := range &spinnerReplacements {
			v = bytes.ReplaceAll(v, []byte(replacement), []byte("?"))
		}
		return T(v)
	default:
		panic("unsupported type")
	}
}

// StripPrivateUse walks grapheme clusters and replaces any cluster containing a
// Unicode private-use character with '?'. T must be string or []byte.
func StripPrivateUse[T string | []byte](input T) T {
	var gr *uniseg.Graphemes
	switch v := any(input).(type) {
	case string:
		gr = uniseg.NewGraphemes(v)
	case []byte:
		gr = uniseg.NewGraphemes(string(v))
	default:
		panic("unsupported type")
	}

	var out strings.Builder

	for gr.Next() {
		cluster := gr.Str()
		containsPrivate := false
		for _, r := range cluster {
			if IsPrivateUse(r) {
				containsPrivate = true
				break
			}
		}
		if containsPrivate {
			out.WriteRune('?')
			continue
		}
		out.WriteString(cluster)
	}

	return T(out.String())
}

// IsPrivateUse reports whether r lies in a Unicode private-use area (BMP
// U+E000–U+F8FF or supplementary planes U+F0000–U+FFFFD / U+100000–U+10FFFD).
func IsPrivateUse(r rune) bool {
	return InRanges(r,
		[2]rune{0xE000, 0xF8FF},
		[2]rune{0xF0000, 0xFFFFD},
		[2]rune{0x100000, 0x10FFFD},
	)
}

// InRanges reports whether r is within any of the inclusive [low, high] pairs.
func InRanges(r rune, ranges ...[2]rune) bool {
	for _, rng := range ranges {
		if rng[0] <= r && r <= rng[1] {
			return true
		}
	}
	return false
}

// NormalizeCRLF converts Windows line endings (CRLF) to LF.
func NormalizeCRLF[T string | []byte](input T) T {
	switch v := any(input).(type) {
	case string:
		return T(strings.ReplaceAll(v, "\r\n", "\n"))
	case []byte:
		return T(bytes.ReplaceAll(v, []byte("\r\n"), []byte("\n")))
	default:
		panic("unsupported type")
	}
}

// EscapeESC replaces the ASCII ESC byte (0x1b) with the four-character
// sequence `\x1b` so control bytes appear literally in snapshots and diffs.
func EscapeESC[T string | []byte](input T) T {
	switch v := any(input).(type) {
	case string:
		return T(strings.ReplaceAll(v, "\x1b", `\x1b`))
	case []byte:
		return T(bytes.ReplaceAll(v, []byte{0x1b}, []byte(`\x1b`)))
	default:
		panic("unsupported type")
	}
}
