// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package observe_test

import (
	"slices"
	"strings"
	"testing"

	"entgo.io/ent/entc/gen"
	"github.com/lrstanley/x/entx/observe"
)

func TestValidateAnnotations_placement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   *gen.Type
		wantErr string
	}{
		{
			name:  "no-annotation",
			value: &gen.Type{Name: "T", Annotations: map[string]any{}},
		},
		{
			name: "valid-schema-eager-load",
			value: &gen.Type{Name: "T", Annotations: map[string]any{
				observe.Annotation{}.Name(): observe.WithEagerLoad(true),
			}},
		},
		{
			name: "valid-edge-eager-load",
			value: &gen.Type{
				Name: "T",
				Edges: []*gen.Edge{{
					Name: "pets",
					Annotations: map[string]any{
						observe.Annotation{}.Name(): observe.WithEagerLoad(true),
					},
				}},
			},
		},
		{
			name: "eager-load-on-field-fails",
			value: &gen.Type{
				Name: "T",
				Fields: []*gen.Field{{
					Name: "name",
					Annotations: map[string]any{
						observe.Annotation{}.Name(): observe.WithEagerLoad(true),
					},
				}},
			},
			wantErr: "EagerLoad",
		},
		{
			name: "eager-load-on-index-fails",
			value: &gen.Type{
				Name: "T",
				Indexes: []*gen.Index{{
					Name: "idx_name",
					Annotations: map[string]any{
						observe.Annotation{}.Name(): observe.WithEagerLoad(true),
					},
				}},
			},
			wantErr: "EagerLoad",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := observe.ValidateAnnotations(tt.value)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateAnnotations: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestObservedEdges_interaction(t *testing.T) {
	t.Parallel()

	mk := func(schemaEager *bool, edgeAnns map[string]*bool) *gen.Type {
		n := &gen.Type{Name: "User", Annotations: map[string]any{}}
		if schemaEager != nil {
			n.Annotations[observe.Annotation{}.Name()] = observe.WithEagerLoad(*schemaEager)
		}
		for name, eager := range edgeAnns {
			e := &gen.Edge{Name: name, Annotations: map[string]any{}}
			if eager != nil {
				e.Annotations[observe.Annotation{}.Name()] = observe.WithEagerLoad(*eager)
			}
			n.Edges = append(n.Edges, e)
		}
		// Stable edge order for assertions.
		slices.SortFunc(n.Edges, func(a, b *gen.Edge) int {
			return strings.Compare(a.Name, b.Name)
		})
		return n
	}
	ptr := func(v bool) *bool { return &v }
	names := func(n *gen.Type) []string {
		edges := observe.ObservedEdges(n)
		out := make([]string, len(edges))
		for i, e := range edges {
			out[i] = e.Name
		}
		return out
	}

	tests := []struct {
		name        string
		schemaEager *bool
		edges       map[string]*bool
		want        []string
	}{
		{
			name:  "neither-loads-none",
			edges: map[string]*bool{"documents": nil, "notes": nil},
			want:  nil,
		},
		{
			name:        "schema-true-loads-every-unannotated",
			schemaEager: ptr(true),
			edges:       map[string]*bool{"documents": nil, "notes": nil},
			want:        []string{"documents", "notes"},
		},
		{
			name:        "schema-true-opt-out",
			schemaEager: ptr(true),
			edges:       map[string]*bool{"documents": nil, "notes": ptr(false)},
			want:        []string{"documents"},
		},
		{
			name:        "schema-true-redundant-include",
			schemaEager: ptr(true),
			edges:       map[string]*bool{"documents": ptr(true), "notes": ptr(false)},
			want:        []string{"documents"},
		},
		{
			name:        "schema-false-same-as-unset",
			schemaEager: ptr(false),
			edges:       map[string]*bool{"documents": nil, "notes": nil},
			want:        nil,
		},
		{
			name:  "selective-include",
			edges: map[string]*bool{"documents": ptr(true), "notes": nil},
			want:  []string{"documents"},
		},
		{
			name:  "explicit-false-without-schema",
			edges: map[string]*bool{"documents": ptr(false), "notes": nil},
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := names(mk(tt.schemaEager, tt.edges))
			if !slices.Equal(got, tt.want) {
				t.Fatalf("ObservedEdges = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAnnotation_Merge(t *testing.T) {
	t.Parallel()

	out, _ := observe.WithEagerLoad(false).Merge(observe.WithEagerLoad(true)).(observe.Annotation)
	if !out.GetEagerLoad() {
		t.Fatal("expected EagerLoad true after merge")
	}
	out, _ = observe.WithEagerLoad(true).Merge(observe.WithEagerLoad(false)).(observe.Annotation)
	if out.EagerLoad == nil || *out.EagerLoad {
		t.Fatal("expected EagerLoad false after merge")
	}
}
