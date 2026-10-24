// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package draw //nolint:revive // name mirrors image/draw responsibilities

import "testing"

func TestClusterMarks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cluster string
		want    string
	}{
		{"empty", "", ""},
		{"ascii", "a", ""},
		{"wide", "世", ""},
		{"acute", "e\u0301", "\u0301"},
		{"stacked", "e\u0301\u0323", "\u0301\u0323"},
		{"enclosing keycap", "1\ufe0f\u20e3", "\u20e3"},
		{"text selector", "❤\ufe0e", ""},
		{"emoji selector", "❤\ufe0f", ""},
		{"ideographic selector", "⼀\U000e0100", ""},
		{"zwj sequence", "\U0001f468\u200d\U0001f469\u200d\U0001f467", ""},
		{"mark after selector", "x\ufe0f\u0301", "\u0301"},
		{"spacing mark", "का", ""},
		{"mongolian selector", "\u1820\u180b", ""},
		{"selector between marks", "x\u0301\ufe0f\u0323", "\u0301\u0323"},
		{"capped", "x\u0301\u0302\u0303\u0304\u0305\u0306", "\u0301\u0302\u0303\u0304"},
		{"capped after selector", "x\ufe0f\u0301\u0302\u0303\u0304\u0305", "\u0301\u0302\u0303\u0304"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := clusterMarks(tt.cluster); got != tt.want {
				t.Fatalf("clusterMarks(%q) = %q, want %q", tt.cluster, got, tt.want)
			}
		})
	}
}

func TestNormalizeCluster(t *testing.T) {
	t.Parallel()

	tests := []struct {
		cluster string
		want    string
	}{
		{"", ""},
		{"a", "a"},
		{"e\u0301", "é"},
		{"x\u0301", "x\u0301"},
		{"\U0001f468\u200d\U0001f469", "\U0001f468\u200d\U0001f469"},
	}
	for _, tt := range tests {
		if got := normalizeCluster(tt.cluster); got != tt.want {
			t.Errorf("normalizeCluster(%q) = %q, want %q", tt.cluster, got, tt.want)
		}
	}
}

func TestNewCluster(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    Cluster
		wantOK  bool
	}{
		{"empty cell is a space", "", Cluster{Text: " ", Base: ' '}, true},
		{"ascii", "a", Cluster{Text: "a", Base: 'a'}, true},
		{"composed", "e\u0301", Cluster{Text: "\u00e9", Base: '\u00e9'}, true},
		{"no precomposed form", "x\u0301", Cluster{Text: "x\u0301", Base: 'x', Marks: "\u0301"}, true},
		{"selector is not a mark", "a\ufe0f", Cluster{Text: "a\ufe0f", Base: 'a'}, true},
		{"replacement character", "\ufffd", Cluster{Text: "\ufffd", Base: '\ufffd'}, true},
		{"invalid utf-8", "\xff", Cluster{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := NewCluster(tt.content)
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("NewCluster(%q) = %+v, %v, want %+v, %v", tt.content, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestSingleRuneClusterDoesNotAllocate(t *testing.T) {
	for _, s := range []string{"a", "\u4e16", "\ue0b0", ""} {
		if n := testing.AllocsPerRun(100, func() {
			_, _ = NewCluster(s)
		}); n != 0 {
			t.Errorf("%q: %v allocs, want 0", s, n)
		}
	}
}
