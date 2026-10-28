// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"encoding/json/jsontext"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestNewAnnotation(t *testing.T) {
	t.Parallel()
	type note struct {
		N    int    `json:"n"`
		Text string `json:"text,omitzero"`
	}
	a, err := NewAnnotation("test.note", note{N: 1})
	if err != nil {
		t.Fatal(err)
	}
	if a.Kind != "test.note" || string(a.Data) != `{"n":1}` {
		t.Fatalf("NewAnnotation = %q %s", a.Kind, a.Data)
	}
	var got note
	if err := a.Decode(&got); err != nil || got != (note{N: 1}) {
		t.Fatalf("Decode = %+v, %v", got, err)
	}

	empty, err := NewAnnotation("test.empty", nil)
	if err != nil || empty.Data != nil {
		t.Fatalf("NewAnnotation(nil) = %s, %v", empty.Data, err)
	}
	if err := empty.Decode(&got); err != nil {
		t.Fatalf("Decode of no fields: %v", err)
	}

	for name, tc := range map[string]struct {
		kind string
		v    any
	}{
		"kind upper case":   {"Test.note", nil},
		"kind one word":     {"note", nil},
		"kind empty":        {"", nil},
		"data not object":   {"test.note", []int{1}},
		"data has kind":     {"test.note", map[string]string{"kind": "other.kind"}},
		"data not json":     {"test.note", func() {}},
		"kind trailing dot": {"test.note.", nil},
	} {
		if _, err := NewAnnotation(tc.kind, tc.v); err == nil {
			t.Errorf("%s: NewAnnotation succeeded", name)
		}
	}
	if _, err := NewAnnotation("Bad", nil); !errors.Is(err, ErrInvalidAnnotation) {
		t.Errorf("bad kind: %v, want ErrInvalidAnnotation", err)
	}
}

func TestAnnotationUnknownKindRoundTrip(t *testing.T) {
	t.Parallel()
	// A kind the schema does not describe keeps every field, whatever it
	// holds.
	data := `{"version":2,"width":4,"height":1}
[0.5,"R","{\"kind\":\"future.thing\",\"z\":[1,{\"a\":null}],\"b\":\"x\"}"]
`
	tl := readCast(t, []byte(data))
	got := slices.Collect(tl.Annotations())
	if len(got) != 1 || got[0].Kind != "future.thing" || string(got[0].Data) != `{"b":"x","z":[1,{"a":null}]}` {
		t.Fatalf("Annotations = %+v", got)
	}
	want := `[0.500,"R","{\"kind\":\"future.thing\",\"b\":\"x\",\"z\":[1,{\"a\":null}]}"]`
	if out := string(writeCast(t, tl, CastV2)); !strings.Contains(out, want) {
		t.Fatalf("written:\n%s\nwant line %s", out, want)
	}
}

func TestReadTimelineInvalidAnnotation(t *testing.T) {
	t.Parallel()
	for name, line := range map[string]string{
		"not json":       `[0,"R","nope"]`,
		"not object":     `[0,"R","[1]"]`,
		"no kind":        `[0,"R","{\"n\":1}"]`,
		"kind not text":  `[0,"R","{\"kind\":1}"]`,
		"bad kind":       `[0,"R","{\"kind\":\"Nope\"}"]`,
		"duplicate kind": `[0,"R","{\"kind\":\"a.b\",\"kind\":\"a.c\"}"]`,
	} {
		_, err := ReadTimeline(strings.NewReader(`{"version":2,"width":4,"height":1}` + "\n" + line + "\n"))
		if err == nil || !strings.Contains(err.Error(), "line 2") {
			t.Errorf("%s: ReadTimeline = %v, want an error naming line 2", name, err)
		}
	}
}

func TestSessionAnnotate(t *testing.T) {
	t.Parallel()
	s := openTest(t, 4, 1)
	if err := s.Annotate(Annotation{Kind: "test.note", Data: jsontext.Value(`{"n":2}`)}); err != nil {
		t.Fatal(err)
	}
	if err := s.Annotate(Annotation{Kind: "test.note", Data: jsontext.Value(`[2]`)}); err == nil {
		t.Fatal("Annotate with array data succeeded")
	}
	if err := s.Annotate(Annotation{Kind: "x"}); !errors.Is(err, ErrInvalidAnnotation) {
		t.Fatalf("Annotate with bad kind = %v", err)
	}
	got := slices.Collect(s.Timeline().Annotations())
	if len(got) != 1 || string(got[0].Data) != `{"n":2}` {
		t.Fatalf("Annotations = %+v", got)
	}
}
