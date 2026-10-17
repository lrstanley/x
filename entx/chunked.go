// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package entx

import (
	"context"
	"iter"
)

// PagableQuery is a query that can be paginated. All ent should implement this
// interface.
type PagableQuery[P any, T any] interface {
	Limit(int) P
	Offset(int) P
	All(context.Context) ([]*T, error)
}

// Chunked takes a pagable query, and yields an iterator of chunks of the results.
// If the size is less than 1, it will default to 250.
func Chunked[P PagableQuery[P, T], T any](
	ctx context.Context,
	size int,
	query PagableQuery[P, T],
) iter.Seq2[*T, error] {
	if size <= 1 {
		size = 250
	}

	return func(yield func(*T, error) bool) {
		var offset int
		var results []*T
		var err error

		for {
			results, err = query.Limit(size).Offset(offset).All(ctx)
			if err != nil {
				yield(nil, err)
				return
			}

			for _, result := range results {
				if !yield(result, nil) {
					return
				}
			}

			if len(results) < size {
				return
			}

			offset += size
		}
	}
}
