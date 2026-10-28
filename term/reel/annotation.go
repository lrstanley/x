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

// AnnotationSchema is the JSON Schema (annotation.schema.json) of an
// [Annotation]'s data in an asciicast file. It checks the fields of the
// kinds it describes and allows unknown kinds, fields, and values, so files
// written by newer tools still read back whole.
//
//go:embed annotation.schema.json
var AnnotationSchema []byte

// AnnotationTextMax is the most bytes of text kept in a single annotation
// field; see [ClipText].
const AnnotationTextMax = 4 << 10

// ClipText returns s as valid UTF-8 (invalid bytes become U+FFFD), cut at a
// rune boundary to at most [AnnotationTextMax] bytes. It sets *truncated
// when s was cut, and leaves it alone otherwise.
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

// Annotation is an event that records something outside what the terminal
// showed, such as an effect to apply to recordings or what a test did. A
// replay ignores it, but its time counts toward the timeline's duration.
//
// In an asciicast file it is an event with code "R" whose data is a JSON
// object of its Kind and fields, as [AnnotationSchema] describes. Other
// asciicast readers ignore it.
type Annotation struct {
	event
	// Kind is dot-separated lower case words, such as "reel.effect".
	Kind string
	// Data is the annotation's fields as a JSON object without the kind, or
	// nil for none. It must not be modified.
	Data jsontext.Value
}

// NewAnnotation returns an annotation of kind with the fields of v, which
// must be nil or marshal to a JSON object without a "kind" member. Invalid
// UTF-8 in strings is replaced with U+FFFD. The result must validate
// against [AnnotationSchema]; otherwise the error wraps
// [ErrInvalidAnnotation].
func NewAnnotation(kind string, v any) (Annotation, error) {
	a, err := newAnnotation(kind, v)
	if err != nil {
		return a, err
	}
	return a, a.validate()
}

// newAnnotation is [NewAnnotation] without schema validation, for
// annotations reel builds itself.
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

// annotationObject is an [Annotation]'s JSON form: its kind, then its fields.
type annotationObject struct {
	Kind string         `json:"kind"`
	Data jsontext.Value `json:",embed"`
}

// encode returns the annotation's asciicast event data without schema
// validation. Data that is not a JSON object, or has a "kind" member, is an
// error.
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

// parseAnnotation validates and parses an asciicast annotation event's data.
// Field order is preserved.
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

// kindSchemas is [AnnotationSchema] resolved per described kind, plus once
// for all other kinds.
type kindSchemas struct {
	kinds map[string]*jsonschema.Resolved
	other *jsonschema.Resolved
}

// annotationSchemas resolves [AnnotationSchema] per kind. The schema's
// top-level allOf has an if/then branch per kind; validating every branch
// for every annotation is slow, so each kind gets a copy whose allOf is just
// its own then, and other kinds get a copy with no allOf.
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

// ErrInvalidAnnotation is wrapped by errors for annotations that
// [AnnotationSchema] rejects.
var ErrInvalidAnnotation = errors.New("reel: invalid annotation")

// validateAnnotation checks an annotation's JSON object against the schema
// resolved for its kind.
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
