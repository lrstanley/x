// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"bytes"
	"context"
	"image/color"
	"slices"
	"strings"
	"testing"
	"time"
)

// sessionQueries returns the queries s recorded, oldest first.
func sessionQueries(t *testing.T, s *Session) []Query {
	t.Helper()
	var out []Query
	for a := range s.Timeline().Annotations() {
		if a.Kind != QueryKind {
			continue
		}
		var q Query
		if err := a.Decode(&q); err != nil {
			t.Fatal(err)
		}
		out = append(out, q)
	}
	return out
}

// readReplies returns what the program can read from s within a moment.
func readReplies(s *Session) string {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	var got []byte
	buf := make([]byte, 1024)
	for {
		n, err := s.readContext(ctx, buf)
		got = append(got, buf[:n]...)
		if err != nil {
			return string(got)
		}
	}
}

// TestSessionQueries feeds the terminal each query reel knows, and checks
// what it recorded against what the terminal answered.
func TestSessionQueries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		request string
		want    Query
	}{
		{"\x05", Query{Query: "enq", Reply: "\x06"}},
		{"\x1b[c", Query{Query: "da1", Reply: "\x1b[?64c"}},
		{"\x1b[0c", Query{Query: "da1", Reply: "\x1b[?64c"}},
		{"\x1b[>c", Query{Query: "da2", Reply: "\x1b[>0;0;0c"}},
		{"\x1b[=c", Query{Query: "da3", Reply: "\x1bP!|00000000\x1b\\"}},
		{"\x1b[>q", Query{Query: "xtversion", Reply: "\x1bP>|reel\x1b\\"}},
		{"\x1b[5n", Query{Query: "dsr", Param: "5", Reply: "\x1b[0n"}},
		{"\x1b[?15n", Query{Query: "dsr", Param: "?15"}},
		{"\x1b[6n", Query{Query: "cpr", Param: "6", Reply: "\x1b[1;3R"}},
		{"\x1b[?6n", Query{Query: "cpr", Param: "?6", Reply: "\x1b[?1;3;1R"}},
		{"\x1b[?996n", Query{Query: "color_scheme", Reply: "\x1b[?997;1n"}},
		{"\x1b[?2026$p", Query{Query: "decrqm", Param: "?2026", Reply: "\x1b[?2026;2$y"}},
		{"\x1b[4$p", Query{Query: "decrqm", Param: "4", Reply: "\x1b[4;2$y"}},
		{"\x1b]10;?\x07", Query{Query: "osc_color", Param: "10", Reply: "\x1b]10;rgb:eeee/eeee/eeee\x07"}},
		{"\x1b]11;?\x1b\\", Query{Query: "osc_color", Param: "11", Reply: "\x1b]11;rgb:1111/1111/1111\x1b\\"}},
		{"\x1b]12;?\x07", Query{Query: "osc_color", Param: "12", Reply: "\x1b]12;rgb:eeee/eeee/eeee\x07"}},
		{"\x1b]4;1;?;2;?\x07", Query{Query: "osc_color", Param: "4", Reply: "\x1b]4;1;rgb:cccc/6666/6666\x07\x1b]4;2;rgb:b5b5/bdbd/6868\x07"}},
		{"\x1b]52;c;?\x07", Query{Query: "osc52", Param: "c", Reply: "\x1b]52;c;Y2xpcA==\x07"}},
		{"\x1b[?u", Query{Query: "kitty_keyboard", Reply: "\x1b[?0u"}},
		{"\x1bP$qm\x1b\\", Query{Query: "decrqss", Param: "m", Reply: "\x1bP1$r0m\x1b\\"}},
		{"\x1bP+q544e;636f6c73\x1b\\", Query{Query: "xtgettcap", Param: "TN;cols", Reply: "\x1bP1+r636F6C73=3830\x1b\\"}},
		{"\x1b[14t", Query{Query: "xtwinops", Param: "14", Reply: "\x1b[4;40;100t"}},
		{"\x1b[18t", Query{Query: "xtwinops", Param: "18", Reply: "\x1b[8;2;10t"}},
		{"\x1b[1;1;1;1;2;2*y", Query{Query: "decrqcra", Reply: "\x1bP1!~FF3D\x1b\\"}},
	} {
		t.Run(strings.Trim(strings.ReplaceAll(tc.request, "\x1b", "ESC"), "\x07\x05"), func(t *testing.T) {
			t.Parallel()
			s := openTest(t, 10, 2)
			s.Terminal().SetClipboard("clip")
			if err := s.Terminal().SetChecksumReport(true); err != nil {
				t.Fatal(err)
			}
			// The dynamic colors are only reported once some are set.
			if err := s.SetDefaultForeground(color.NRGBA{R: 0xee, G: 0xee, B: 0xee, A: 0xff}); err != nil {
				t.Fatal(err)
			}
			if err := s.SetDefaultBackground(color.NRGBA{R: 0x11, G: 0x11, B: 0x11, A: 0xff}); err != nil {
				t.Fatal(err)
			}
			if err := s.SetDefaultCursorColor(color.NRGBA{R: 0xee, G: 0xee, B: 0xee, A: 0xff}); err != nil {
				t.Fatal(err)
			}
			write(t, s, "ab"+tc.request+"\x1b[1m")

			want := tc.want
			want.Request = tc.request
			got := sessionQueries(t, s)
			if len(got) != 1 || got[0] != want {
				t.Fatalf("queries = %#v\nwant %#v", got, want)
			}
			// The program still reads the reply.
			if replies := readReplies(s); replies != want.Reply {
				t.Fatalf("program read %q, want %q", replies, want.Reply)
			}
		})
	}
}

func TestSessionQueriesMatch(t *testing.T) {
	t.Parallel()
	s := openTest(t, 10, 2)
	// A startup burst, as Bubble Tea writes: an unanswered query between
	// answered ones, and a mode set that is not a query.
	write(t, s, "\x1b[?2026$p\x1b]11;?\x07\x1b[?1049h\x1b[?u\x1b[c")
	got := sessionQueries(t, s)
	want := []Query{
		{Query: "decrqm", Param: "?2026", Request: "\x1b[?2026$p", Reply: "\x1b[?2026;2$y"},
		{Query: "osc_color", Param: "11", Request: "\x1b]11;?\x07"},
		{Query: "kitty_keyboard", Request: "\x1b[?u", Reply: "\x1b[?0u"},
		{Query: "da1", Request: "\x1b[c", Reply: "\x1b[?64c"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("queries = %#v\nwant %#v", got, want)
	}

	// Each query's annotation follows the output event that held it.
	events := slices.Collect(s.Timeline().Events())
	if _, ok := events[0].(Output); !ok || len(events) != 5 {
		t.Fatalf("events = %+v, want the output then 4 annotations", events)
	}

	// A query split across writes is answered by the second, which records
	// the reply alone.
	write(t, s, "\x1b[>")
	write(t, s, "q")
	got = sessionQueries(t, s)[len(want):]
	if len(got) != 1 || got[0] != (Query{Query: "xtversion", Reply: "\x1bP>|reel\x1b\\"}) {
		t.Fatalf("split query = %#v", got)
	}
}

func TestSessionQueriesTruncated(t *testing.T) {
	t.Parallel()
	s := openTest(t, 10, 2)
	s.Terminal().SetClipboard(strings.Repeat("x", 8<<10))
	write(t, s, "\x1b]52;c;?\x07")
	got := sessionQueries(t, s)
	if len(got) != 1 || !got[0].Truncated || len(got[0].Reply) != annotationTextMax {
		t.Fatalf("queries = %d, truncated %v, reply %d bytes", len(got), len(got) > 0 && got[0].Truncated, len(got[0].Reply))
	}
}

func TestSessionWithoutQueries(t *testing.T) {
	t.Parallel()
	s := openTest(t, 10, 2, WithoutQueries())
	write(t, s, "\x1b[c")
	if got := sessionQueries(t, s); len(got) != 0 {
		t.Fatalf("queries = %#v, want none", got)
	}
	if replies := readReplies(s); replies != "\x1b[?64c" {
		t.Fatalf("program read %q", replies)
	}
}

func TestQueryCast(t *testing.T) {
	t.Parallel()
	s := openTest(t, 10, 2)
	write(t, s, "hi\x1b[c")
	cast := writeCast(t, s.Timeline(), CastV2)
	if want := `"R","{\"kind\":\"reel.query\",\"query\":\"da1\",\"request\":\"\\u001b[c\",\"reply\":\"\\u001b[?64c\"}"]`; !bytes.Contains(cast, []byte(want)) {
		t.Fatalf("cast:\n%s\nwant %s", cast, want)
	}
	tl := readCast(t, cast)
	if got := slices.Collect(tl.Annotations()); len(got) != 1 || got[0].Kind != QueryKind {
		t.Fatalf("read back %+v", got)
	}
}

func TestQuerySchema(t *testing.T) {
	t.Parallel()
	valid := []any{
		Query{Query: "da1", Request: "\x1b[c", Reply: "\x1b[?64c"},
		Query{Query: "osc_color", Param: "11", Request: "\x1b]11;?\x07"},
		Query{Query: "unknown", Reply: "\x1b[?1;2x", Truncated: true},
	}
	for _, v := range valid {
		if _, err := NewAnnotation(QueryKind, v); err != nil {
			t.Errorf("%+v: %v", v, err)
		}
	}
	for name, v := range map[string]any{
		"no query":       map[string]any{"request": "\x1b[c"},
		"bad query":      map[string]any{"query": "da9", "request": "\x1b[c"},
		"neither":        map[string]any{"query": "da1"},
		"unknown field":  map[string]any{"query": "da1", "request": "\x1b[c", "extra": 1},
		"truncated off":  map[string]any{"query": "da1", "request": "\x1b[c", "truncated": false},
		"request number": map[string]any{"query": "da1", "request": 1},
	} {
		if _, err := NewAnnotation(QueryKind, v); err == nil {
			t.Errorf("%s: NewAnnotation succeeded", name)
		}
	}
}

func TestScanSeqs(t *testing.T) {
	t.Parallel()
	var got []string
	scanSeqs([]byte("a\x1b[1;2Hb\x1b]0;t\x07\x1b]2;cut\x1b[c\x1bP$qm\x1b\\\x1b7\x05\x1b[?2"), func(s ctlSeq) {
		got = append(got, string(s.raw))
	})
	// A cursor move ends in no query's or reply's final, so it is skipped.
	want := []string{"\x1b]0;t\x07", "\x1b[c", "\x1bP$qm\x1b\\", "\x05"}
	if !slices.Equal(got, want) {
		t.Fatalf("scanSeqs = %q, want %q", got, want)
	}
}
