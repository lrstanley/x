// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"bytes"
	_ "embed"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"sync"

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
	a := Annotation{Kind: kind}
	if v != nil {
		data, err := json.Marshal(v, json.Deterministic(true), jsontext.AllowInvalidUTF8(true))
		if err != nil {
			return a, fmt.Errorf("reel: annotation %q: %w", kind, err)
		}
		a.Data = data
	}
	if _, err := a.marshal(); err != nil {
		return a, err
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

// marshal returns the annotation as the JSON object of an asciicast event's
// data, after checking it against [AnnotationSchema].
func (e Annotation) marshal() (string, error) {
	var b strings.Builder
	kind, err := jsontext.AppendQuote(nil, e.Kind)
	if err != nil {
		return "", fmt.Errorf("reel: annotation kind %q: %w", e.Kind, err)
	}
	b.WriteString(`{"kind":`)
	b.Write(kind)
	if len(e.Data) > 0 {
		data := jsontext.Value(bytes.Clone(e.Data))
		if err := data.Compact(); err != nil {
			return "", fmt.Errorf("reel: annotation %q: %w", e.Kind, err)
		}
		if data.Kind() != '{' {
			return "", fmt.Errorf("reel: annotation %q: data is a JSON %v, not an object", e.Kind, data.Kind())
		}
		if len(data) > 2 {
			b.WriteByte(',')
			b.Write(data[1:])
		} else {
			b.WriteByte('}')
		}
	} else {
		b.WriteByte('}')
	}
	s := b.String()
	if err := validateAnnotation(s); err != nil {
		return "", err
	}
	return s, nil
}

// parseAnnotation parses the data of an asciicast annotation event, after
// checking it against [AnnotationSchema].
func parseAnnotation(data string) (Annotation, error) {
	var a Annotation
	if err := validateAnnotation(data); err != nil {
		return a, err
	}
	var fields map[string]jsontext.Value
	if err := json.Unmarshal([]byte(data), &fields); err != nil {
		return a, err
	}
	if err := json.Unmarshal(fields["kind"], &a.Kind); err != nil {
		return a, err
	}
	delete(fields, "kind")
	if len(fields) > 0 {
		var err error
		if a.Data, err = json.Marshal(fields, json.Deterministic(true)); err != nil {
			return a, err
		}
	}
	return a, nil
}

// annotationSchema is [AnnotationSchema], resolved.
var annotationSchema = sync.OnceValues(func() (*jsonschema.Resolved, error) {
	var s jsonschema.Schema
	if err := json.Unmarshal(AnnotationSchema, &s); err != nil {
		return nil, err
	}
	return s.Resolve(nil)
})

// ErrInvalidAnnotation is wrapped by the errors for an annotation that
// [AnnotationSchema] does not allow.
var ErrInvalidAnnotation = errors.New("reel: invalid annotation")

// validateAnnotation checks the JSON object of an annotation against
// [AnnotationSchema].
func validateAnnotation(data string) error {
	rs, err := annotationSchema()
	if err != nil {
		return fmt.Errorf("reel: annotation schema: %w", err)
	}
	var v any
	if err := json.Unmarshal([]byte(data), &v); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidAnnotation, err)
	}
	if err := rs.Validate(v); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidAnnotation, err)
	}
	return nil
}
