// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"testing"

	"github.com/lrstanley/x/charm/steep"
	"github.com/lrstanley/x/charm/steep/snapshot"
)

func TestModel(t *testing.T) {
	steep.NewHarness(t, newRankingModel()).
		WaitString("Tokyo").
		AssertSnapshot(snapshot.WithANSI(false)).
		AssertJSON()
}
