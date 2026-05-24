// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fontset_test

import (
	"testing"

	"github.com/lrstanley/x/charm/still/fonts"
	"github.com/lrstanley/x/charm/still/internal/config"
	"github.com/lrstanley/x/charm/still/internal/fontset"
)

func TestBuildCodepointMapOverlap(t *testing.T) {
	t.Parallel()

	_, err := fontset.Build(config.Options{
		CodepointMapSet: true,
		CodepointMap: map[string]fonts.FontFamily{
			"U+E000-U+E010": mustDefaultFamily(t),
			"U+E010-U+E020": mustDefaultSymbolFamily(t),
		},
		FontSize: config.DefaultFontSize,
		DPI:      config.DefaultDPI,
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
		FontSize: config.DefaultFontSize,
		DPI:      config.DefaultDPI,
	})
	if err == nil {
		t.Fatal("Build() error = nil, want malformed error")
	}
}
