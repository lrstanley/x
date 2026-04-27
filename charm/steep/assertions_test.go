// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import "testing"

type ansiLiteralView string

func (v ansiLiteralView) View() string { return string(v) }

func TestAssertStringWithStripANSI(t *testing.T) {
	t.Helper()
	const out = "plain \x1b[31mred\x1b[0m"
	if !AssertString(t, ansiLiteralView(out), "plain red", WithStripANSI()) {
		t.Fatal("expected strip-then-contains to match")
	}
}
