// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"slices"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestFields(t *testing.T) {
	tests := []struct {
		seq  string
		want []field
	}{
		{"\x1b[1;38;2;255;0;10;4:3m", []field{{"1", "bold"}, {"38;2;255;0;10", "foreground #ff000a"}, {"4:3", "underline curly"}}},
		{"\x1b[?25;2026h", []field{{"?25 cursor visible", "set"}, {"?2026 synchronized output", "set"}}},
		{"\x1b]7501;state=working:progress=40:title=YnVpbGQ=\x07", []field{{"command", "7501"}, {"state", "working"}, {"progress", "40"}, {"title", `"build"`}}},
		{"\x1b]133;D;1\x07", []field{{"command", "133"}, {"kind", "D command finished"}, {"exit status", "1"}}},
		{"\x1b]8;id=a;https://x\x1b\\", []field{{"command", "8"}, {"id", "a"}, {"uri", "https://x"}}},
		{"\x1bP+q544e\x1b\\", []field{{"capability", "TN"}}},
		{"\x1b[38:2::255:0:10m", []field{{"38:2::255:0:10", "foreground #ff000a"}}},
		{"\x1b[48:2:255:0:10;1m", []field{{"48:2:255:0:10", "background #ff000a"}, {"1", "bold"}}},
		{"\x1b[58:2:0:255:0:10m", []field{{"58:2:0:255:0:10", "underline color #ff000a"}}},
		{"\x1b[38:5:208;48;5;4;58;2;1;2;3m", []field{{"38:5:208", "foreground indexed 208"}, {"48;5;4", "background indexed 4"}, {"58;2;1;2;3", "underline color #010203"}}},
		{"\x1b[H", nil},
	}
	for _, tt := range tests {
		p := ansi.NewParser()
		seq, _, _, _ := ansi.DecodeSequence([]byte(tt.seq), 0, p)
		if got := fields(seq, p); !slices.Equal(got, tt.want) {
			t.Errorf("fields(%q) = %q, want %q", tt.seq, got, tt.want)
		}
	}
}
