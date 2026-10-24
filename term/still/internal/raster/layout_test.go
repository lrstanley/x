// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package raster_test

import (
	"testing"

	"github.com/lrstanley/x/term/still/internal/raster"
)

func TestIsSpanIcon(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"font awesome", "\uf059", true},
		{"codicon", "\uea61", true},
		{"plane 15 private use", "\U000f0001", true},
		{"octicons heart", "\u2665", true},
		{"other symbol block", "\u2699", false},
		{"powerline branch", "\ue0a0", false},
		{"powerline arrow", "\ue0b0", false},
		{"trigram", "\u2630", false},
		{"heavy angle bracket", "\u276c", false},
		{"power symbol", "\u23fb", false},
		{"unassigned private use", "\ue100", false},
		{"letter", "a", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := raster.IsSpanIcon(tt.content); got != tt.want {
				t.Fatalf("IsSpanIcon(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

func TestIconRoom(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		prev, next string
		want       bool
	}{
		{"space after", "", " ", true},
		{"empty cell after", "", "", true},
		{"en space after", "", "\u2002", true},
		{"after text", "a", " ", true},
		{"after powerline", "\ue0b0", " ", true},
		{"letter after", "", "x", false},
		{"icon after", "", "\uf084", false},
		{"after icon", "\uf084", " ", false},
		{"after other symbol", "\u2699", " ", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := raster.IconRoom(tt.prev, tt.next); got != tt.want {
				t.Fatalf("IconRoom(%q, %q) = %v, want %v", tt.prev, tt.next, got, tt.want)
			}
		})
	}
}
