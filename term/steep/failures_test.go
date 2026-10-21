// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"slices"
	"strings"
	"testing"
)

func TestFailureTBRecords(t *testing.T) {
	t.Parallel()
	f := &failureTB{TB: &softTB{TB: t}}
	f.Error("want", 3, "rows")
	f.Errorf("timeout after %s", "2s")
	want := []string{"want 3 rows", "timeout after 2s"}
	if got := f.messages(); !slices.Equal(got, want) {
		t.Fatalf("messages = %q, want %q", got, want)
	}
}

func TestExportFailureLines(t *testing.T) {
	t.Parallel()
	texts := func(lines []CardLine) []string {
		out := make([]string, len(lines))
		for i, line := range lines {
			out[i] = line.Text
		}
		return out
	}
	cases := []struct {
		name       string
		failures   []string
		cols, rows int
		want       []string
	}{
		{
			name: "no failures",
			cols: 40, rows: 24,
		},
		{
			name:     "summary lines only, as one block",
			failures: []string{"\x1b[31mwant Tokyo\x1b[0m\noutput:\nscreen dump", "timeout\twaiting"},
			cols:     40, rows: 24,
			want: []string{"", "• want Tokyo     ", "• timeout waiting"},
		},
		{
			name:     "long summaries wrap under their bullet",
			failures: []string{"expected output to contain the city"},
			cols:     24, rows: 24,
			want: []string{"", "• expected output to", "  contain the city  "},
		},
		{
			name:     "count what does not fit",
			failures: []string{"one", "two", "three", "four"},
			cols:     40, rows: 8,
			want: []string{"", "• one", "• two", "+2 more"},
		},
		{
			name:     "no room",
			failures: []string{"one"},
			cols:     40, rows: 5,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ec := ExportContext{
				h:   &Harness{failures: &failureTB{msgs: tc.failures}},
				src: frameSource{cols: tc.cols, rows: tc.rows},
			}
			got := texts(ec.failureLines())
			if !slices.Equal(got, tc.want) {
				t.Fatalf("lines =\n%s\nwant\n%s", strings.Join(got, "|\n"), strings.Join(tc.want, "|\n"))
			}
		})
	}
}
