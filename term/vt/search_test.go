// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"errors"
	"testing"
)

func TestSearchMatches(t *testing.T) {
	t.Parallel()
	term := openTest(t, 10, 4)
	if _, err := term.Write([]byte("Fizz\r\nBuzz\r\nFIZZ")); err != nil {
		t.Fatal(err)
	}
	search, err := NewSearch(term)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(search.Close)
	status, err := search.Status()
	if err != nil {
		t.Fatal(err)
	}
	if status != SearchComplete {
		t.Fatalf("idle status = %d", status)
	}
	if _, err = search.Needle(); !errors.Is(err, ErrNoValue) {
		t.Fatalf("idle needle error = %v", err)
	}
	if err = search.SetNeedle([]byte("fizz")); err != nil {
		t.Fatal(err)
	}
	if err = search.Run(); err != nil {
		t.Fatal(err)
	}
	count, err := search.MatchCount()
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("matches = %d", count)
	}
	matches, err := search.Matches()
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Fatalf("snapshots = %d", len(matches))
	}
	if matches[0].Start.Y != 2 || matches[1].Start.Y != 0 {
		t.Fatalf("rows = %d,%d", matches[0].Start.Y, matches[1].Start.Y)
	}
	viewport, err := search.ViewportMatches()
	if err != nil {
		t.Fatal(err)
	}
	if len(viewport) != 2 {
		t.Fatalf("viewport matches = %d", len(viewport))
	}
	if err = search.SelectNext(); err != nil {
		t.Fatal(err)
	}
	index, err := search.SelectedIndex()
	if err != nil {
		t.Fatal(err)
	}
	if index != 0 {
		t.Fatalf("selected = %d", index)
	}
	selected, err := search.SelectedMatch()
	if err != nil {
		t.Fatal(err)
	}
	text, err := term.SelectionText(selected)
	if err != nil {
		t.Fatal(err)
	}
	if text != "FIZZ" {
		t.Fatalf("selected text = %q", text)
	}
	if err = search.SelectNext(); err != nil {
		t.Fatal(err)
	}
	index, err = search.SelectedIndex()
	if err != nil {
		t.Fatal(err)
	}
	if index != 1 {
		t.Fatalf("next selected = %d", index)
	}
	if err = search.SelectPrevious(); err != nil {
		t.Fatal(err)
	}
	policy, err := search.ScrollPolicy()
	if err != nil {
		t.Fatal(err)
	}
	if policy != SearchScrollIfNeeded {
		t.Fatalf("policy = %d", policy)
	}
	if err = search.SetScrollPolicy(SearchScrollNone); err != nil {
		t.Fatal(err)
	}
	policy, err = search.ScrollPolicy()
	if err != nil {
		t.Fatal(err)
	}
	if policy != SearchScrollNone {
		t.Fatalf("policy after set = %d", policy)
	}
	if err = search.SetNeedle(nil); err != nil {
		t.Fatal(err)
	}
	if err = search.SelectNext(); !errors.Is(err, ErrNoValue) {
		t.Fatalf("select after clear = %v", err)
	}
}
