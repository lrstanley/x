// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package snapshot

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/lrstanley/x/charm/steep/internal/xansi"
)

const envUpdateSnapshots = "UPDATE_SNAPSHOTS"

type options struct {
	suffix               string
	update               bool
	transforms           []func([]byte) []byte
	disableEscapeESC     bool
	disableStripSpinners bool
}

// collectOptions applies functional options into a single configuration.
func collectOptions(tb testing.TB, opts ...Option) options {
	tb.Helper()

	var cfg options

	cfg.update, _ = strconv.ParseBool(os.Getenv("UPDATE_SNAPS")) // To match that of github.com/gkampitakis/go-snaps.
	if !cfg.update {
		cfg.update, _ = strconv.ParseBool(os.Getenv(envUpdateSnapshots))
	}

	if cfg.update {
		tb.Log("updating of snapshots is enabled through env vars")
	}

	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	cfg.transforms = append(cfg.transforms, xansi.NormalizeCRLF[[]byte])

	if !cfg.disableStripSpinners {
		cfg.transforms = append(cfg.transforms, xansi.StripSpinners[[]byte])
	}

	if !cfg.disableEscapeESC {
		cfg.transforms = append(cfg.transforms, xansi.EscapeESC[[]byte])
	}

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

// WithEnableSpinners prevents spinner characters from being replaced with "?",
// which is the default behavior.
func WithEnableSpinners() Option {
	return func(cfg *options) {
		cfg.disableStripSpinners = false
	}
}

// WithESC prevents ESC bytes from being escaped with "\x1b", which is the default
// behavior.
func WithESC() Option {
	return func(cfg *options) {
		cfg.disableEscapeESC = false
	}
}

// normalize converts supported values to a stable byte representation.
func normalize[T ~[]byte | ~string](got T, cfg options) []byte {
	value := reflect.ValueOf(got)
	var bts []byte
	if value.Kind() == reflect.String {
		bts = []byte(value.String())
	} else if value.Kind() == reflect.Slice {
		bts = bytes.Clone(value.Bytes())
	} else {
		bts = fmt.Append(nil, got)
	}

	for _, transform := range cfg.transforms {
		bts = transform(bts)
	}

	return bts
}
