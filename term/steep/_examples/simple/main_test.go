// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"testing"

	"github.com/lrstanley/x/term/steep"
	"github.com/lrstanley/x/term/steep/snapshot"
)

func TestModel(t *testing.T) {
	steep.NewHarness(t, newRankingModel()).
		Wait("Tokyo").
		AssertSnapshot(snapshot.WithANSI(false)).
		AssertJSON()
}
