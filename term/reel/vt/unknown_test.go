// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vt

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestWithUnknownSequence(t *testing.T) {
	t.Parallel()
	// One slot, so the second Open reuses the first terminal.
	rt := NewRuntime(1)
	var got []UnknownSequence
	now := 5 * time.Second
	term, err := rt.Open(context.Background(), 20, 2,
		WithClock(func() time.Duration { return now }),
		WithUnknownSequence(func(seq UnknownSequence) { got = append(got, seq) }),
	)
	if err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("x", UnknownSequenceMaxBytes+10)
	writes := []string{
		"\x1b]7400;status=busy\x07",
		"\x1b]7401;st\x1b\\",
		"\x1b_Xapc\x1b\\",
		"\x1b]2;title\x07", // OSC 2 is implemented, so it is not reported.
		"\x1b]8;;\x07",     // So is OSC 8.
		"\x1b]9999;" + long + "\x07",
		"\x1b]7402;cut\x18", // CAN cancels it.
	}
	for _, w := range writes {
		if _, err = term.Write([]byte(w)); err != nil {
			t.Fatal(err)
		}
	}
	want := []UnknownSequence{
		{Kind: SequenceOSC, Data: "7400;status=busy", At: now},
		{Kind: SequenceOSC, Data: "7401;st", At: now},
		{Kind: SequenceAPC, Data: "Xapc", At: now},
		{Kind: SequenceOSC, Data: ("9999;" + long)[:UnknownSequenceMaxBytes], Truncated: true, At: now},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("unknown sequences:\n got %+v\nwant %+v", summarize(got), summarize(want))
	}
	if title := term.Title(); title != "title" {
		t.Fatalf("title = %q, want %q", title, "title")
	}

	// A full reset, from the program or the host, keeps reporting on.
	got = nil
	if _, err = term.Write([]byte("\x1bc\x1b]7403;ris\x07")); err != nil {
		t.Fatal(err)
	}
	if err = term.Reset(); err != nil {
		t.Fatal(err)
	}
	if _, err = term.Write([]byte("\x1b]7404;reset\x07")); err != nil {
		t.Fatal(err)
	}
	want = []UnknownSequence{
		{Kind: SequenceOSC, Data: "7403;ris", At: now},
		{Kind: SequenceOSC, Data: "7404;reset", At: now},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("after reset:\n got %+v\nwant %+v", got, want)
	}
	if err = term.Close(); err != nil {
		t.Fatal(err)
	}

	// The next checkout without the option reports nothing, to either fn.
	got = nil
	term, err = rt.Open(context.Background(), 20, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	if _, err = term.Write([]byte("\x1b]7400;status=idle\x07")); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("closed terminal's callback got %+v", summarize(got))
	}
}

// summarize shortens long Data so a failure message stays readable.
func summarize(seqs []UnknownSequence) []UnknownSequence {
	out := slices.Clone(seqs)
	for i := range out {
		if len(out[i].Data) > 40 {
			out[i].Data = out[i].Data[:40] + "..."
		}
	}
	return out
}
