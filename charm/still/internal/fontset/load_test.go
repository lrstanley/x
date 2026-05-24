// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fontset_test

import (
	"testing"

	"github.com/lrstanley/x/charm/still/fonts"
	"github.com/lrstanley/x/charm/still/internal/config"
	"github.com/lrstanley/x/charm/still/internal/fontset"
	"github.com/lrstanley/x/charm/still/internal/testutil"
)

func TestBuildNilRegularFont(t *testing.T) {
	t.Parallel()

	_, err := fontset.Build(config.Options{
		FontFamilySet: true,
		FontFamily:    fonts.FontFamily{},
		FontSize:      config.DefaultFontSize,
		DPI:           config.DefaultDPI,
	})
	if err == nil {
		t.Fatal("Build() error = nil, want error")
	}
}

func TestSyntheticStyleFallback(t *testing.T) {
	t.Parallel()

	defaultFamily := mustDefaultFamily(t)
	fs, err := fontset.Build(config.Options{
		FontFamilySet: true,
		FontFamily:    fonts.FontFamily{Regular: defaultFamily.Regular},
		FontSize:      config.DefaultFontSize,
		DPI:           config.DefaultDPI,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	t.Cleanup(func() { _ = fs.Close() })

	testutil.AssertSyntheticFaceDiff(t, fs.Regular(), fs.Bold(), 'H')
	testutil.AssertSyntheticFaceDiff(t, fs.Regular(), fs.Italic(), 'H')
	testutil.AssertSyntheticFaceDiff(t, fs.Regular(), fs.BoldItalic(), 'H')
}
