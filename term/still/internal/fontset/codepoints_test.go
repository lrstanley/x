// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fontset_test

import (
	"testing"

	"github.com/lrstanley/x/term/still/fonts"
	"github.com/lrstanley/x/term/still/internal/config"
	"github.com/lrstanley/x/term/still/internal/fontset"
)

func TestBuildCodepointMapOverlap(t *testing.T) {
	t.Parallel()

	_, err := fontset.Build(config.Options{
		CodepointMapSet: true,
		CodepointMap: map[string]fonts.FontFamily{
			"U+E000-U+E010": mustDefaultFamily(t),
			"U+E010-U+E020": mustDefaultSymbolFamily(t),
		},
		Scale: config.DefaultScale,
	})
	if err == nil {
		t.Fatal("Build() error = nil, want overlap error")
	}
}

func TestBuildCodepointMapMalformed(t *testing.T) {
	t.Parallel()

	_, err := fontset.Build(config.Options{
		CodepointMapSet: true,
		CodepointMap: map[string]fonts.FontFamily{
			"E000": mustDefaultFamily(t),
		},
		Scale: config.DefaultScale,
	})
	if err == nil {
		t.Fatal("Build() error = nil, want malformed error")
	}
}

func TestBuildCodepointMapNilRegular(t *testing.T) {
	t.Parallel()

	_, err := fontset.Build(config.Options{
		CodepointMapSet: true,
		CodepointMap: map[string]fonts.FontFamily{
			"U+E000": {},
		},
		Scale: config.DefaultScale,
	})
	if err == nil {
		t.Fatal("Build() error = nil, want error for a codepoint family with no regular face")
	}
}
