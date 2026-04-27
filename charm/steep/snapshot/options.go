// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package snapshot

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/lrstanley/x/charm/steep/internal/xansi"
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
	return WithTransform(xansi.StripANSI)
}

// WithStripPrivate replaces private use grapheme clusters before
// comparison (e.g. Nerd Font glyphs).
func WithStripPrivate() Option {
	return WithTransform(xansi.StripPrivateUse)
}

// withStripSpinners replaces spinner characters with "?".
func withStripSpinners() Option {
	return WithTransform(xansi.StripSpinners)
}

// withNormalizeCRLF adds Windows line ending normalization to the transform chain.
func withNormalizeCRLF() Option {
	return WithTransform(xansi.NormalizeCRLF)
}

// withEscapeESC writes ESC bytes as the four-character sequence \x1b so ANSI
// sequences stay visible and diff-friendly in snapshot files.
func withEscapeESC() Option {
	return WithTransform(xansi.EscapeESC)
}
