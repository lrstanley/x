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

func TestNewAnnotationInvalidUTF8(t *testing.T) {
	t.Parallel()
	a, err := NewAnnotation("test.note", map[string]string{"text": "a\xffb"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\"text\":\"a�b\"}"; string(a.Data) != want {
		t.Fatalf("Data = %s, want %s", a.Data, want)
	}
}

func TestEffectAnnotationSchema(t *testing.T) {
	t.Parallel()
	valid := []string{
		`{"kind":"reel.effect","effect":"keys","shown_ms":1000}`,
		`{"kind":"reel.effect","effect":"intro","duration_ms":500}`,
		`{"kind":"reel.effect","effect":"intro","duration_ms":500,"lines":[{"text":"hi","style":{"fg":3,"bg":"#010203","underline_color":"#AABBCC80","underline":"dotted","attrs":["bold","rapid_blink"]}},{"text":""}]}`,
		`{"kind":"reel.effect","effect":"pause"}`,
		`{"kind":"reel.effect","effect":"pause","points":[{"at":"end","duration_ms":0},{"at":"marks","duration_ms":10,"labels":["a"]},{"at":"marks","duration_ms":10},{"at":"match","duration_ms":10,"pattern":"^b"}],"badge":true,"badge_corner":"bottom_left","badge_size_px":8,"badge_color":"#102030"}`,
		`{"kind":"reel.effect","effect":"pause","badge":false,"badge_size_px":0}`,
		`{"kind":"reel.effect","effect":"highlight","region":{"type":"text","text":"x"}}`,
		`{"kind":"reel.effect","effect":"highlight","region":{"type":"any","regions":[{"type":"cells","x0":-1,"y0":0,"x1":5,"y1":2},{"type":"rows","from":1,"to":2},{"type":"regexp","pattern":"a+"},{"type":"env","names":[]},{"type":"any","regions":[]}]},"timing":{"between":{"from":"a","to":"b"},"ease_in_ms":100,"ease_out_ms":0},"dim":0,"outline":{"color":"#ff0000c8","width_px":3},"padding_px":0}`,
		`{"kind":"reel.effect","effect":"highlight","region":{"type":"rows","from":0,"to":1},"timing":{"between":{}},"outline":{}}`,
		`{"kind":"reel.effect","effect":"zoom","region":{"type":"rows","from":0,"to":1},"scale":1.5,"anchor":{"x":0,"y":1}}`,
		`{"kind":"reel.effect","effect":"redact","region":{"type":"env","names":["TOKEN"]},"timing":{"between":{"to":"b"}},"mode":"blur","min_partial":1}`,
		`{"kind":"reel.effect","effect":"redact","region":{"type":"text","text":"x"},"mode":"replace","replacement":"•"}`,
		`{"kind":"reel.effect","effect":"redact_inputs","region":{"type":"text","text":"x"},"min_partial":4}`,
	}
	for _, data := range valid {
		if err := validateAnnotation(data); err != nil {
			t.Errorf("%s: %v", data, err)
		}
	}
	invalid := map[string]string{
		"no effect":                `{"kind":"reel.effect"}`,
		"keys without shown":       `{"kind":"reel.effect","effect":"keys"}`,
		"keys fractional ms":       `{"kind":"reel.effect","effect":"keys","shown_ms":1.5}`,
		"intro without duration":   `{"kind":"reel.effect","effect":"intro"}`,
		"line without text":        `{"kind":"reel.effect","effect":"intro","duration_ms":1,"lines":[{}]}`,
		"palette index":            `{"kind":"reel.effect","effect":"intro","duration_ms":1,"lines":[{"text":"x","style":{"fg":256}}]}`,
		"color name":               `{"kind":"reel.effect","effect":"intro","duration_ms":1,"lines":[{"text":"x","style":{"fg":"red"}}]}`,
		"pause negative":           `{"kind":"reel.effect","effect":"pause","points":[{"at":"end","duration_ms":-1}]}`,
		"match without pattern":    `{"kind":"reel.effect","effect":"pause","points":[{"at":"match","duration_ms":1}]}`,
		"end with labels":          `{"kind":"reel.effect","effect":"pause","points":[{"at":"end","duration_ms":1,"labels":["a"]}]}`,
		"marks with pattern":       `{"kind":"reel.effect","effect":"pause","points":[{"at":"marks","duration_ms":1,"pattern":"a"}]}`,
		"badge size":               `{"kind":"reel.effect","effect":"pause","badge_size_px":4}`,
		"badge color index":        `{"kind":"reel.effect","effect":"pause","badge_color":3}`,
		"highlight without region": `{"kind":"reel.effect","effect":"highlight"}`,
		"region without type":      `{"kind":"reel.effect","effect":"highlight","region":{"text":"x"}}`,
		"cells without y1":         `{"kind":"reel.effect","effect":"highlight","region":{"type":"cells","x0":0,"y0":0,"x1":1}}`,
		"nested region":            `{"kind":"reel.effect","effect":"highlight","region":{"type":"any","regions":[{"type":"rows","from":0}]}}`,
		"dim":                      `{"kind":"reel.effect","effect":"highlight","region":{"type":"text","text":"x"},"dim":2}`,
		"outline color":            `{"kind":"reel.effect","effect":"highlight","region":{"type":"text","text":"x"},"outline":{"color":"#fff"}}`,
		"ease negative":            `{"kind":"reel.effect","effect":"zoom","region":{"type":"text","text":"x"},"timing":{"ease_in_ms":-1}}`,
		"zoom scale":               `{"kind":"reel.effect","effect":"zoom","region":{"type":"text","text":"x"},"scale":0.5}`,
		"zoom anchor":              `{"kind":"reel.effect","effect":"zoom","region":{"type":"text","text":"x"},"anchor":{"x":0.5}}`,
		"redact eases":             `{"kind":"reel.effect","effect":"redact","region":{"type":"text","text":"x"},"timing":{"ease_in_ms":1}}`,
		"replace without char":     `{"kind":"reel.effect","effect":"redact","region":{"type":"text","text":"x"},"mode":"replace"}`,
		"replacement two chars":    `{"kind":"reel.effect","effect":"redact","region":{"type":"text","text":"x"},"mode":"replace","replacement":"ab"}`,
		"replacement with block":   `{"kind":"reel.effect","effect":"redact","region":{"type":"text","text":"x"},"replacement":"*"}`,
		"min partial":              `{"kind":"reel.effect","effect":"redact_inputs","region":{"type":"text","text":"x"},"min_partial":0}`,
	}
	for name, data := range invalid {
		if err := validateAnnotation(data); !errors.Is(err, ErrInvalidAnnotation) {
			t.Errorf("%s: validate = %v, want ErrInvalidAnnotation", name, err)
		}
	}
	// Fields and values the schema does not list are allowed, since newer
	// tools may write them. The effects package refuses to apply them.
	for _, data := range []string{
		`{"kind":"reel.effect","effect":"sparkle"}`,
		`{"kind":"reel.effect","effect":"keys","shown_ms":1,"fade_ms":1}`,
		`{"kind":"reel.effect","effect":"intro","duration_ms":1,"lines":[{"text":"x","style":{"underline":"wavy"}}]}`,
		`{"kind":"reel.effect","effect":"intro","duration_ms":1,"lines":[{"text":"x","style":{"attrs":["loud"]}}]}`,
		`{"kind":"reel.effect","effect":"pause","points":[{"at":"start","duration_ms":1}]}`,
		`{"kind":"reel.effect","effect":"pause","badge_corner":"middle"}`,
		`{"kind":"reel.effect","effect":"highlight","region":{"type":"oval"}}`,
		`{"kind":"reel.effect","effect":"highlight","region":{"type":"text","text":"x","pattern":"y"}}`,
		`{"kind":"reel.effect","effect":"highlight","region":{"type":"env","names":["A"],"values":["secret"]}}`,
		`{"kind":"reel.effect","effect":"highlight","region":{"type":"text","text":"x"},"timing":{"ease_ms":1}}`,
		`{"kind":"reel.effect","effect":"redact","region":{"type":"text","text":"x"},"mode":"melt"}`,
		`{"kind":"reel.effect","effect":"redact_inputs","region":{"type":"text","text":"x"},"timing":{}}`,
	} {
		if err := validateAnnotation(data); err != nil {
			t.Errorf("%s: %v", data, err)
		}
	}
}
