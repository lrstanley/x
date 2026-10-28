// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	_ "embed"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/google/jsonschema-go/jsonschema"
)

// AnnotationSchema is the JSON Schema of the data of an [Annotation] in an
// asciicast file: the JSON object with its kind and fields. It is the file
// annotation.schema.json in this package's directory, which tools that
// write annotations of their own can validate against. For the kinds it
// describes, it checks the fields it lists, and allows fields and values it
// does not, so a file written by a newer tool reads back whole.
//
//go:embed annotation.schema.json
var AnnotationSchema []byte

// AnnotationTextMax is the most bytes of text reel and steep keep in a
// field of an annotation they record, such as a terminal reply or the text
// a test waited for. [ClipText] cuts longer text.
const AnnotationTextMax = 4 << 10

// ClipText returns s as valid UTF-8, cut at a character boundary to at most
// [AnnotationTextMax] bytes, and sets *truncated when it was cut, for a
// field of an annotation to record s in. Invalid UTF-8 is replaced with
// U+FFFD, since an annotation is JSON.
func ClipText(s string, truncated *bool) string {
	s = strings.ToValidUTF8(s, "�")
	if len(s) <= AnnotationTextMax {
		return s
	}
	*truncated = true
	n := AnnotationTextMax
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// annotationCode is the asciicast event code of an [Annotation].
const annotationCode = "R"

// Annotation is reel's own event, which records something about the session
// that is not part of what the terminal showed, such as an effect to apply
// to its recordings, or what a test did. A replay ignores it, but its time
// counts toward the timeline's duration, as a [Mark]'s does.
//
// In an asciicast file it is an event with code "R", whose data is a JSON
// object of its Kind and its fields, as [AnnotationSchema] describes. Other
// readers ignore it.
type Annotation struct {
	event
	// Kind names what the annotation records, as dot-separated lower case
	// words, such as "reel.effect". Kinds the schema does not describe are
	// kept as they are, so a file written by a newer reel reads back
	// whole.
	Kind string
	// Data is the annotation's fields, a JSON object without the kind, or
	// nil for none. It must not be modified.
	Data jsontext.Value
}

// NewAnnotation returns an annotation of kind with the fields of v, which
// must marshal to a JSON object without a "kind" member, or be nil. Strings
// that are not UTF-8 are written with U+FFFD in place of their invalid
// bytes. The annotation must be valid as [AnnotationSchema] describes its
// kind.
func NewAnnotation(kind string, v any) (Annotation, error) {
	a, err := newAnnotation(kind, v)
	if err != nil {
		return a, err
	}
	return a, a.validate()
}

// newAnnotation is [NewAnnotation], without checking the annotation against
// [AnnotationSchema], for annotations reel builds that it always allows.
func newAnnotation(kind string, v any) (Annotation, error) {
	a := Annotation{Kind: kind}
	if v != nil {
		data, err := json.Marshal(v, json.Deterministic(true), jsontext.AllowInvalidUTF8(true))
		if err != nil {
			return a, fmt.Errorf("reel: annotation %q: %w", kind, err)
		}
		a.Data = data
	}
	return a, nil
}

// Decode unmarshals the annotation's fields into v, as [json.Unmarshal]
// does. An annotation with no fields decodes as an empty object.
func (e Annotation) Decode(v any) error {
	data := e.Data
	if len(data) == 0 {
		data = jsontext.Value("{}")
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("reel: annotation %q: %w", e.Kind, err)
	}
	return nil
}

// annotationObject is the JSON object of an [Annotation] in an asciicast
// file: its kind, then its fields.
type annotationObject struct {
	Kind string         `json:"kind"`
	Data jsontext.Value `json:",embed"`
}

// encode returns the annotation as the JSON object of an asciicast event's
// data, without checking it against [AnnotationSchema]. Data that is not a
// JSON object, or that has a "kind" member, is an error.
func (e Annotation) encode() ([]byte, error) {
	data, err := json.Marshal(annotationObject{Kind: e.Kind, Data: e.Data})
	if err != nil {
		return nil, fmt.Errorf("reel: annotation %q: %w", e.Kind, err)
	}
	return data, nil
}

// validate checks the annotation against [AnnotationSchema].
func (e Annotation) validate() error {
	data, err := e.encode()
	if err != nil {
		return err
	}
	return validateAnnotation(data)
}

// parseAnnotation parses the data of an asciicast annotation event, after
// checking it against [AnnotationSchema]. Its fields keep their order.
func parseAnnotation(data string) (Annotation, error) {
	b := []byte(data)
	if err := validateAnnotation(b); err != nil {
		return Annotation{}, err
	}
	var obj annotationObject
	if err := json.Unmarshal(b, &obj); err != nil {
		return Annotation{}, err
	}
	return Annotation{Kind: obj.Kind, Data: obj.Data}, nil
}

// kindSchemas is [AnnotationSchema] resolved once for each kind it
// describes, and once for every other kind.
type kindSchemas struct {
	kinds map[string]*jsonschema.Resolved
	other *jsonschema.Resolved
}

// annotationSchemas returns [AnnotationSchema], resolved for each kind. The
// schema's top level allOf checks the fields of each kind it describes,
// with an if on the kind and a then of its definition. Checking every
// branch for every annotation is slow, so each kind is checked against the
// schema with that allOf replaced by the then of its kind alone, and other
// kinds against the schema without it.
var annotationSchemas = sync.OnceValues(func() (*kindSchemas, error) {
	var root jsonschema.Schema
	if err := json.Unmarshal(AnnotationSchema, &root); err != nil {
		return nil, err
	}
	base := root.CloneSchemas()
	base.AllOf = nil
	other, err := base.Resolve(nil)
	if err != nil {
		return nil, err
	}
	ks := &kindSchemas{kinds: make(map[string]*jsonschema.Resolved, len(root.AllOf)), other: other}
	for i, branch := range root.AllOf {
		var kind string
		if branch.If != nil && branch.If.Properties["kind"] != nil && branch.If.Properties["kind"].Const != nil {
			kind, _ = (*branch.If.Properties["kind"].Const).(string)
		}
		if kind == "" || branch.Then == nil {
			return nil, fmt.Errorf("allOf %d is not an if on a kind with a then", i)
		}
		one := root.CloneSchemas()
		one.AllOf = []*jsonschema.Schema{one.AllOf[i].Then}
		if ks.kinds[kind], err = one.Resolve(nil); err != nil {
			return nil, err
		}
	}
	return ks, nil
})

// ErrInvalidAnnotation is wrapped by the errors for an annotation that
// [AnnotationSchema] does not allow.
var ErrInvalidAnnotation = errors.New("reel: invalid annotation")

// validateAnnotation checks the JSON object of an annotation against
// [AnnotationSchema], resolved for its kind.
func validateAnnotation(data []byte) error {
	ks, err := annotationSchemas()
	if err != nil {
		return fmt.Errorf("reel: annotation schema: %w", err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidAnnotation, err)
	}
	rs := ks.other
	if obj, ok := v.(map[string]any); ok {
		if kind, ok := obj["kind"].(string); ok && ks.kinds[kind] != nil {
			rs = ks.kinds[kind]
		}
	}
	if err := rs.Validate(v); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidAnnotation, err)
	}
	return nil
}
