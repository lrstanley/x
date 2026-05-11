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
