// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"testing"

	"github.com/lrstanley/x/term/steep/snapshot"
)

func TestBasics(t *testing.T) {
	t.Parallel()
	snapshot.RequireEqual(t, render(), snapshot.WithANSI(false))
}
