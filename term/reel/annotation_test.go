// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
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
	err = a.Decode(&got)
	if err != nil || got != (note{N: 1}) {
		t.Fatalf("Decode = %+v, %v", got, err)
	}

	empty, err := NewAnnotation("test.empty", nil)
	if err != nil || empty.Data != nil {
		t.Fatalf("NewAnnotation(nil) = %s, %v", empty.Data, err)
	}
	err = empty.Decode(&got)
	if err != nil {
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
		_, err = NewAnnotation(tc.kind, tc.v)
		if err == nil {
			t.Errorf("%s: NewAnnotation succeeded", name)
		}
	}
	_, err = NewAnnotation("Bad", nil)
	if !errors.Is(err, ErrInvalidAnnotation) {
		t.Errorf("bad kind: %v, want ErrInvalidAnnotation", err)
	}
}

func TestAnnotationUnknownKindRoundTrip(t *testing.T) {
	t.Parallel()
	// A kind the schema does not describe keeps every field, whatever it
	// holds, in the order of the file, wherever the kind is.
	data := `{"version":2,"width":4,"height":1}
[0.5,"R","{\"z\":[1,{\"a\":null}], \"kind\":\"future.thing\",\"b\":\"x\"}"]
`
	tl := readCast(t, []byte(data))
	got := slices.Collect(tl.Annotations())
	if len(got) != 1 || got[0].Kind != "future.thing" || string(got[0].Data) != `{"z":[1,{"a":null}],"b":"x"}` {
		t.Fatalf("Annotations = %+v", got)
	}
	want := `[0.500,"R","{\"kind\":\"future.thing\",\"z\":[1,{\"a\":null}],\"b\":\"x\"}"]`
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

// effectSchemaCases are annotations of [EffectKind] the schema allows, and
// those it does not, by name.
var effectSchemaCases = struct {
	valid   []string
	invalid map[string]string
}{
	valid: []string{
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
		// Fields and values the schema does not list are allowed, since
		// newer tools may write them. The effects package refuses to apply
		// them.
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
		// The schema does not describe the effects that conceal, which an
		// asciicast file is never written with.
		`{"kind":"reel.effect","effect":"redact","region":{"type":"text","text":"x"},"mode":"replace"}`,
		`{"kind":"reel.effect","effect":"redact_inputs","region":{"type":"text","text":"x"},"min_partial":0}`,
	},
	invalid: map[string]string{
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
	},
}

func TestEffectAnnotationSchema(t *testing.T) {
	t.Parallel()
	for _, data := range effectSchemaCases.valid {
		if err := validateAnnotation([]byte(data)); err != nil {
			t.Errorf("%s: %v", data, err)
		}
	}
	for name, data := range effectSchemaCases.invalid {
		if err := validateAnnotation([]byte(data)); !errors.Is(err, ErrInvalidAnnotation) {
			t.Errorf("%s: validate = %v, want ErrInvalidAnnotation", name, err)
		}
	}
}

// TestAnnotationSchemaByKind checks that checking an annotation against the
// schema resolved for its kind decides as the whole schema does, and that
// every kind the schema describes is resolved.
func TestAnnotationSchemaByKind(t *testing.T) {
	t.Parallel()
	var s jsonschema.Schema
	if err := json.Unmarshal(AnnotationSchema, &s); err != nil {
		t.Fatal(err)
	}
	whole, err := s.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	ks, err := annotationSchemas()
	if err != nil {
		t.Fatal(err)
	}
	if len(ks.kinds) != len(s.AllOf) || ks.kinds[EffectKind] == nil || ks.kinds[QueryKind] == nil || ks.kinds["steep.result"] == nil {
		t.Fatalf("resolved %d kinds, want %d, with %s, %s, and steep.result", len(ks.kinds), len(s.AllOf), EffectKind, QueryKind)
	}

	samples := slices.Clone(effectSchemaCases.valid)
	for _, data := range effectSchemaCases.invalid {
		samples = append(samples, data)
	}
	samples = append(samples,
		`{"kind":"reel.query","query":"da1","request":"\u001b[c"}`,
		`{"kind":"reel.query","query":"da1"}`,
		`{"kind":"steep.result","test":"T","failed":false,"failures":0,"duration_ms":1}`,
		`{"kind":"steep.result","test":"T"}`,
		`{"kind":"steep.exit","killed":false}`,
		`{"kind":"future.thing","n":1}`,
		`{"kind":"Future"}`,
		`{"kind":1}`,
		`{"n":1}`,
		`[1]`,
	)
	for _, data := range samples {
		var v any
		err = json.Unmarshal([]byte(data), &v)
		if err != nil {
			t.Fatal(err)
		}
		want := whole.Validate(v) == nil
		if got := validateAnnotation([]byte(data)) == nil; got != want {
			t.Errorf("%s: valid %v, want %v", data, got, want)
		}
	}
}

// BenchmarkValidateAnnotation measures a check of an annotation of a kind
// the schema describes.
func BenchmarkValidateAnnotation(b *testing.B) {
	data := []byte(`{"kind":"reel.query","query":"da1","request":"\u001b[c","reply":"\u001b[?64c"}`)
	for b.Loop() {
		if err := validateAnnotation(data); err != nil {
			b.Fatal(err)
		}
	}
}
