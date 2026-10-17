// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package entx

import (
	"context"
	"errors"
	"testing"

	"entgo.io/ent"
)

type mutationContextKey struct{}

type mutationWithID struct {
	ent.Mutation
	id int
	ok bool
}

func (m mutationWithID) ID() (int, bool) {
	return m.id, m.ok
}

type mutationWithIDs struct {
	ent.Mutation
	ids []int
	err error
	got context.Context
}

func (m *mutationWithIDs) IDs(ctx context.Context) ([]int, error) {
	m.got = ctx
	return m.ids, m.err
}

type mutationWithIDsOK struct {
	ent.Mutation
	ids []int
	ok  bool
	got context.Context
}

func (m *mutationWithIDsOK) IDs(ctx context.Context) ([]int, bool) {
	m.got = ctx
	return m.ids, m.ok
}

type mutationWithoutIDs struct {
	ent.Mutation
}

func TestGetMutationIDs(t *testing.T) {
	t.Parallel()

	ctx := context.WithValue(context.Background(), mutationContextKey{}, "value")
	wantErr := errors.New("lookup failed")

	tests := []struct {
		name string
		m    ent.Mutation
		want []int
	}{
		{
			name: "single id",
			m:    mutationWithID{id: 42, ok: true},
			want: []int{42},
		},
		{
			name: "single id unavailable",
			m:    mutationWithID{id: 42},
		},
		{
			name: "multiple ids",
			m:    &mutationWithIDs{ids: []int{1, 2, 3}},
			want: []int{1, 2, 3},
		},
		{
			name: "multiple ids with error",
			m:    &mutationWithIDs{ids: []int{4, 5}, err: wantErr},
			want: []int{4, 5},
		},
		{
			name: "multiple ids unavailable",
			m:    &mutationWithIDsOK{ids: []int{6, 7}},
		},
		{
			name: "multiple ids",
			m:    &mutationWithIDsOK{ids: []int{8, 9}, ok: true},
			want: []int{8, 9},
		},
		{
			name: "unsupported mutation",
			m:    mutationWithoutIDs{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := GetMutationIDs(ctx, tt.m); !equalInts(got, tt.want) {
				t.Errorf("GetMutationIDs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetMutationIDs_passesContext(t *testing.T) {
	t.Parallel()

	ctx := context.WithValue(context.Background(), mutationContextKey{}, "value")
	m := &mutationWithIDs{ids: []int{1}}

	GetMutationIDs(ctx, m)
	if m.got != ctx {
		t.Fatalf("IDs context = %p, want %p", m.got, ctx)
	}
}

func TestGetMutationIDs_prefersSingleID(t *testing.T) {
	t.Parallel()

	m := mutationWithIDAndIDs{
		mutationWithID: mutationWithID{id: 10, ok: true},
		ids:            []int{20},
	}

	if got := GetMutationIDs(context.Background(), m); !equalInts(got, []int{10}) {
		t.Fatalf("GetMutationIDs() = %v, want [10]", got)
	}
}

type mutationWithIDAndIDs struct {
	mutationWithID
	ids []int
}

func (m mutationWithIDAndIDs) IDs(context.Context) ([]int, error) {
	return m.ids, nil
}

func equalInts(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
