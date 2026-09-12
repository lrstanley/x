// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package entx

import (
	"context"

	"entgo.io/ent"
)

// GetMutationIDs returns the entity ids addressed by the mutation, if possible.
// This should work for mutations that either select a single or multiple entities.
// If the mutation does not implement this interface, it will return nil.
func GetMutationIDs(ctx context.Context, m ent.Mutation) (ids []int) {
	if t, ok := m.(interface {
		ID() (int, bool)
	}); ok {
		id, iok := t.ID()
		if iok {
			return []int{id}
		}
	}
	if t, ok := m.(interface {
		IDs(context.Context) ([]int, error)
	}); ok {
		ids, _ = t.IDs(ctx)
		return ids
	}
	if t, ok := m.(interface {
		IDs(context.Context) ([]int, bool)
	}); ok {
		var iok bool
		ids, iok = t.IDs(ctx)
		if iok {
			return ids
		}
	}

	return nil
}
