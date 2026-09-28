// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package observe

import (
	"encoding/json/v2"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"entgo.io/ent/entc/gen"
	"entgo.io/ent/schema"
)

// Ensure Annotation implements the necessary schema interfaces.
var (
	_ schema.Annotation = (*Annotation)(nil)
	_ schema.Merger     = (*Annotation)(nil)
)

// Annotation configures observe codegen for schemas and edges.
type Annotation struct {
	// EagerLoad controls edge eager-loading during full-object observation.
	// On a schema, true loads every edge of that entity (unless an edge opts
	// out). On an edge, true forces that edge on even when the schema flag is
	// off; false opts that edge out even when the schema flag is on. Unset on
	// an edge follows the schema flag.
	EagerLoad *bool `json:",omitzero" ent:"schema,edge"`
}

// Name implements [schema.Annotation].
func (Annotation) Name() string {
	return "Observe"
}

// Decode unmarshals the annotation from the codegen graph representation.
func (a *Annotation) Decode(o any) error {
	buf, err := json.Marshal(o)
	if err != nil {
		return err
	}
	return json.Unmarshal(buf, a)
}

// Merge merges o into a. Later non-zero values win for pointer fields.
func (a Annotation) Merge(o schema.Annotation) schema.Annotation {
	var am Annotation
	switch o := o.(type) {
	case Annotation:
		am = o
	case *Annotation:
		am = *o
	default:
		return a
	}
	if am.EagerLoad != nil {
		a.EagerLoad = am.EagerLoad
	}
	return a
}

// getSupportedType uses reflection to check that set annotation fields are
// allowed on typ ("schema", "field", "edge", or "index").
func (a Annotation) getSupportedType(name, typ string) error {
	ant := reflect.ValueOf(a)
	for i := range ant.NumField() {
		f := ant.Field(i)
		if f.IsZero() || (f.Kind() == reflect.Pointer && f.IsNil()) {
			continue
		}
		supported := strings.Split(ant.Type().Field(i).Tag.Get("ent"), ",")
		if !slices.Contains(supported, typ) {
			return fmt.Errorf(
				"observe: annotation field %q is set on %q %s type, but only one of the following types is supported: %s",
				ant.Type().Field(i).Name,
				name,
				typ,
				strings.Join(supported, ", "),
			)
		}
	}
	return nil
}

// GetAnnotation returns the observe annotation on the given graph item.
func GetAnnotation(v any) *Annotation {
	switch v := v.(type) {
	case *gen.Type:
		return decodeAnnotation(v.Annotations)
	case *gen.Field:
		return decodeAnnotation(v.Annotations)
	case *gen.Edge:
		return decodeAnnotation(v.Annotations)
	case *gen.Index:
		return decodeAnnotation(v.Annotations)
	default:
		panic(fmt.Sprintf("observe: unsupported annotation target %T", v))
	}
}

func decodeAnnotation(as gen.Annotations) *Annotation {
	ant := &Annotation{}
	if as != nil && as[ant.Name()] != nil {
		if err := ant.Decode(as[ant.Name()]); err != nil {
			panic(fmt.Sprintf("observe: failed to decode annotation: %v", err))
		}
	}
	return ant
}

// ValidateAnnotations ensures observe annotations are attached only to
// supported schema elements (matching entrest's placement checks).
func ValidateAnnotations(nodes ...*gen.Type) error {
	for _, t := range nodes {
		if err := GetAnnotation(t).getSupportedType(t.Name, "schema"); err != nil {
			return err
		}
		for _, f := range t.Fields {
			if err := GetAnnotation(f).getSupportedType(f.Name, "field"); err != nil {
				return err
			}
		}
		for _, e := range t.Edges {
			if err := GetAnnotation(e).getSupportedType(e.Name, "edge"); err != nil {
				return err
			}
		}
		for _, idx := range t.Indexes {
			if err := GetAnnotation(idx).getSupportedType(idx.Name, "index"); err != nil {
				return err
			}
		}
	}
	return nil
}

// GetEagerLoad reports whether the annotation requests eager-loading (schema
// all-edges, or an edge explicitly included). Unset or false is false.
func (a *Annotation) GetEagerLoad() bool {
	return a.EagerLoad != nil && *a.EagerLoad
}

// IncludeEdge reports whether e should be eager-loaded when observing n as a
// full object. Explicit [WithEagerLoad] on the edge wins; otherwise the
// schema's [WithEagerLoad] applies. Unset / false means do not load.
func IncludeEdge(n *gen.Type, e *gen.Edge) bool {
	ea := GetAnnotation(e)
	if ea.EagerLoad != nil {
		return *ea.EagerLoad
	}
	return GetAnnotation(n).GetEagerLoad()
}

// ObservedEdges returns the edges of n that full-object observation should
// eager-load, in schema order. ID-only and fields-only modes ignore this list.
func ObservedEdges(n *gen.Type) []*gen.Edge {
	var out []*gen.Edge
	for _, e := range n.Edges {
		if IncludeEdge(n, e) {
			out = append(out, e)
		}
	}
	return out
}

// WithEagerLoad marks a schema or edge for eager-loading during full-object
// observation. On a schema, true loads every edge unless an edge opts out with
// [WithEagerLoad](false). On an edge, true loads that edge even when the
// schema flag is off; false opts out even when the schema flag is on. May be
// placed only on schemas and edges; codegen fails otherwise.
func WithEagerLoad(v bool) Annotation {
	return Annotation{EagerLoad: &v}
}
