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

	"github.com/lrstanley/x/charm/steep/internal/xansi"
)

const envUpdateSnapshots = "UPDATE_SNAPSHOTS"

type options struct {
	// transforms holds user-added byte transforms ([WithTransform]) applied in
	// addition to normalization appended by collectOptions (CRLF, ANSI, spinners,
	// private-use graphemes, ESC escaping — subject to flags below).
	transforms []func([]byte) []byte
	// suffix is sanitized and appended to the snapshot base name ([WithSuffix]).
	suffix string
	// dir is the root directory for snapshot files ([WithDir]); joined with paths
	// derived from test names.
	dir string
	// update selects writing snapshots from "got" values instead of comparing
	// against files on disk ([WithUpdate]; also env-driven by default).
	update bool
	// ansi, when false, strips ANSI styling sequences before compare/write; when true,
	// keeps them ([WithANSI]). Default preserves ANSI.
	ansi bool
	// escapeESC, when true (default), rewrites ESC bytes as the literal substring
	// "\x1b" for stable textual snapshots ([WithEscapeESC]).
	escapeESC bool
	// private, when false, replaces private-use grapheme clusters before compare/write;
	// when true, keeps them ([WithPrivate]). Default keeps them.
	private bool
	// spinners, when false (default), normalizes spinner/running glyphs for stable snapshots;
	// when true, leaves those code points untouched ([WithSpinners]).
	spinners bool
	// indent is the encoder indent width for structured formats such as JSON
	// snapshots ([WithIndent]).
	indent int
}

// collectOptions applies functional options into a single configuration.
func collectOptions(tb testing.TB, opts ...Option) options {
	tb.Helper()

	cfg := options{
		dir:       "testdata",
		ansi:      true,
		escapeESC: true,
		private:   true,
	}

	cfg.update, _ = strconv.ParseBool(os.Getenv("UPDATE_SNAPS")) // To match that of github.com/gkampitakis/go-snaps.
	if !cfg.update {
		cfg.update, _ = strconv.ParseBool(os.Getenv(envUpdateSnapshots))
	}

	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	cfg.transforms = append(cfg.transforms, xansi.NormalizeCRLF[[]byte])

	if !cfg.ansi {
		cfg.transforms = append(cfg.transforms, xansi.StripANSI[[]byte])
	}

	if !cfg.spinners {
		cfg.transforms = append(cfg.transforms, xansi.StripSpinners[[]byte])
	}

	if !cfg.private {
		cfg.transforms = append(cfg.transforms, xansi.StripPrivateUse[[]byte])
	}

	if cfg.escapeESC {
		cfg.transforms = append(cfg.transforms, xansi.EscapeESC[[]byte])
	}

	return cfg
}

// Option configures snapshot normalization before comparison.
type Option func(*options)

// WithTransform adds a transform to apply before comparison.
func WithTransform(fn func([]byte) []byte) Option {
	return func(cfg *options) {
		if fn != nil {
			cfg.transforms = append(cfg.transforms, fn)
		}
	}
}

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

// WithDir sets the directory to store snapshots in. Defaults to "testdata" (in
// the current working directory). Can be relative or absolute.
func WithDir(dir string) Option {
	return func(cfg *options) {
		cfg.dir = dir
	}
}

// WithANSI determines whether ANSI sequences should be kept. When false, ANSI
// sequences are stripped, which will often make it easier to visually compare
// snapshots, but does mean stripping of color and similar information/styling.
func WithANSI(enable bool) Option {
	return func(cfg *options) {
		cfg.ansi = enable
	}
}

// WithPrivate determines whether private use grapheme clusters should be kept,
// or replaced with "?". When false, they will be stripped.
func WithPrivate(enable bool) Option {
	return func(cfg *options) {
		cfg.private = enable
	}
}

// WithSpinners determines whether typical spinner glyphs should be replaced with
// "?". When false, they will be stripped.
func WithSpinners(enable bool) Option {
	return func(cfg *options) {
		cfg.spinners = enable
	}
}

// WithEscapeESC toggles the escape of ESC bytes with "\x1b". When true (the default),
// ESC bytes are escaped with "\x1b".
func WithEscapeESC(enable bool) Option {
	return func(cfg *options) {
		cfg.escapeESC = enable
	}
}

// WithIndent sets the indent level for the snapshot, depending on the snapshot
// format (e.g. JSON).
func WithIndent(indent int) Option {
	return func(cfg *options) {
		cfg.indent = max(0, indent)
	}
}

// normalize converts supported values to a stable byte representation.
func normalize[T ~[]byte | ~string](got T, cfg options) T {
	bts := bytes.Clone([]byte(got))
	for _, transform := range cfg.transforms {
		bts = transform(bts)
	}
	return T(bts)
}
