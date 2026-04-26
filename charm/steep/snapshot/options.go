// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package snapshot

import (
	"bytes"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

type options struct {
	suffix     string
	update     bool
	transforms []func([]byte) []byte
}

// collectOptions applies functional options into a single configuration.
func collectOptions(tb testing.TB, opts ...Option) options {
	tb.Helper()

	var cfg options

	cfg.update, _ = strconv.ParseBool(os.Getenv("UPDATE_SNAPS")) // To match that of github.com/gkampitakis/go-snaps.
	if !cfg.update {
		cfg.update, _ = strconv.ParseBool(os.Getenv(envUpdateSnapshots))
	}

	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	withNormalizeCRLF()(&cfg)
	withStripSpinners()(&cfg)
	withEscapeESC()(&cfg)

	return cfg
}

// Option configures snapshot normalization before comparison.
type Option func(*options)

// WithUpdate sets whether snapshots should be updated. Defaults to the
// UPDATE_SNAPSHOTS environment variable (and UPDATE_SNAPS for compatibility with
// those migrating from github.com/gkampitakis/go-snaps).
func WithUpdate(update bool) Option {
	return func(cfg *options) {
		cfg.update = update
	}
}

// WithSuffix appends name as a suffix to the generated test snapshot name.
func WithSuffix(name string) Option {
	return func(cfg *options) {
		cfg.suffix = strings.TrimPrefix(name, "-")
	}
}

// WithTransform adds a transform to apply before comparison.
func WithTransform(fn func([]byte) []byte) Option {
	return func(cfg *options) {
		if fn != nil {
			cfg.transforms = append(cfg.transforms, fn)
		}
	}
}

// WithStripANSI strips ANSI sequences before comparison.
func WithStripANSI() Option {
	return WithTransform(func(bts []byte) []byte {
		return []byte(ansi.Strip(string(bts)))
	})
}

// WithStripPrivate replaces private use grapheme clusters before
// comparison (e.g. Nerd Font glyphs).
func WithStripPrivate() Option {
	return WithTransform(stripPrivateUse)
}

func stripPrivateUse(bts []byte) []byte {
	gr := uniseg.NewGraphemes(string(bts))
	var out []byte

	for gr.Next() {
		cluster := gr.Str()

		containsPrivate := false
		for _, r := range cluster {
			if isPrivateUse(r) {
				containsPrivate = true
				break
			}
		}
		if containsPrivate {
			out = append(out, '?')
			continue
		}
		out = append(out, cluster...)
	}

	return out
}

func isPrivateUse(r rune) bool {
	return inRanges(r,
		[2]rune{0xE000, 0xF8FF},
		[2]rune{0xF0000, 0xFFFFD},
		[2]rune{0x100000, 0x10FFFD},
	)
}

func inRanges(r rune, ranges ...[2]rune) bool {
	for _, rng := range ranges {
		if rng[0] <= r && r <= rng[1] {
			return true
		}
	}
	return false
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

// withStripSpinners replaces spinner characters with "?".
func withStripSpinners() Option {
	return WithTransform(func(bts []byte) []byte {
		for _, replacement := range &spinnerReplacements {
			bts = bytes.ReplaceAll(bts, []byte(replacement), []byte("?"))
		}
		return bts
	})
}

// withNormalizeCRLF adds Windows line ending normalization to the transform chain.
func withNormalizeCRLF() Option {
	return WithTransform(normalizeCRLF)
}

func normalizeCRLF(bts []byte) []byte {
	return bytes.ReplaceAll(bts, []byte("\r\n"), []byte("\n"))
}

// withEscapeESC writes ESC bytes as the four-character sequence \x1b so ANSI
// sequences stay visible and diff-friendly in snapshot files.
func withEscapeESC() Option {
	return WithTransform(escapeESC)
}

func escapeESC(bts []byte) []byte {
	return bytes.ReplaceAll(bts, []byte{0x1b}, []byte(`\x1b`))
}
