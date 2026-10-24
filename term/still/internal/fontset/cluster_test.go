// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fontset

import (
	"testing"

	"github.com/lrstanley/x/term/still/internal/config"
)

func TestFaceForClusterMarks(t *testing.T) {
	t.Parallel()

	fs := buildTestSet(t, config.Options{}, nil)
	regular := fs.Regular()

	// The primary font covers the base and the mark.
	if got := faceFor(fs, "x\u0301", 0); got != regular {
		t.Error("x + U+0301 is not drawn by the primary face")
	}
	// Composed, the cluster is a single rune the primary font has.
	if got := faceFor(fs, "e\u0301", 0); got != regular {
		t.Error("e + U+0301 is not drawn by the primary face")
	}
	// No font has U+10A0F: the base keeps its face and the mark is skipped by
	// the draw.
	if got := faceFor(fs, "x\U00010a0f", 0); got != regular {
		t.Error("x + uncovered mark is not drawn by the face of x")
	}
	// Variation selectors and joiners do not take part in face selection.
	for _, s := range []string{"x\ufe0f", "x\ufe0e", "x\u200dx"} {
		if got := faceFor(fs, s, 0); got != regular {
			t.Errorf("%q is not drawn by the face of x", s)
		}
	}
}
