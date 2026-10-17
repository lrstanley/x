// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package entx

import (
	"context"
	"errors"
	"slices"
	"testing"
)

type chunkedRow struct {
	value int
}

type chunkedCall struct {
	limit  int
	offset int
}

type chunkedQuery struct {
	rows   []*chunkedRow
	limit  int
	offset int
	calls  *[]chunkedCall
	err    error
}

func (q chunkedQuery) Limit(limit int) chunkedQuery {
	q.limit = limit
	return q
}

func (q chunkedQuery) Offset(offset int) chunkedQuery {
	q.offset = offset
	return q
}

func (q chunkedQuery) All(context.Context) ([]*chunkedRow, error) {
	*q.calls = append(*q.calls, chunkedCall{limit: q.limit, offset: q.offset})
	if q.err != nil {
		return nil, q.err
	}

	if q.offset >= len(q.rows) {
		return nil, nil
	}
	end := min(q.offset+q.limit, len(q.rows))
	return q.rows[q.offset:end], nil
}

func TestChunked(t *testing.T) {
	t.Parallel()

	rows := []*chunkedRow{{value: 1}, {value: 2}, {value: 3}, {value: 4}, {value: 5}}
	var calls []chunkedCall
	query := chunkedQuery{rows: rows, calls: &calls}

	var got []int
	for row, err := range Chunked(context.Background(), 2, query) {
		if err != nil {
			t.Fatalf("Chunked() returned error: %v", err)
		}
		got = append(got, row.value)
	}

	if !slices.Equal(got, []int{1, 2, 3, 4, 5}) {
		t.Fatalf("Chunked() yielded %v, want [1 2 3 4 5]", got)
	}
	wantCalls := []chunkedCall{
		{limit: 2, offset: 0},
		{limit: 2, offset: 2},
		{limit: 2, offset: 4},
	}
	if !slices.Equal(calls, wantCalls) {
		t.Fatalf("query calls = %v, want %v", calls, wantCalls)
	}
}

func TestChunked_defaultsSmallSize(t *testing.T) {
	t.Parallel()

	for _, size := range []int{0, -1} {
		t.Run(testNameForSize(size), func(t *testing.T) {
			t.Parallel()

			var calls []chunkedCall
			query := chunkedQuery{
				rows:  []*chunkedRow{{value: 1}},
				calls: &calls,
			}
			for row, err := range Chunked(context.Background(), size, query) {
				if err != nil {
					t.Fatalf("Chunked() returned error: %v", err)
				}
				if row.value != 1 {
					t.Fatalf("yielded row value = %d, want 1", row.value)
				}
			}

			want := []chunkedCall{{limit: 250, offset: 0}}
			if !slices.Equal(calls, want) {
				t.Fatalf("query calls = %v, want %v", calls, want)
			}
		})
	}
}

func TestChunked_stopsWhenYieldReturnsFalse(t *testing.T) {
	t.Parallel()

	var calls []chunkedCall
	query := chunkedQuery{
		rows:  []*chunkedRow{{value: 1}, {value: 2}, {value: 3}},
		calls: &calls,
	}

	var got []int
	for row, err := range Chunked(context.Background(), 2, query) {
		if err != nil {
			t.Fatalf("Chunked() returned error: %v", err)
		}
		got = append(got, row.value)
		if len(got) == 1 {
			break
		}
	}

	if !slices.Equal(got, []int{1}) {
		t.Fatalf("Chunked() yielded %v, want [1]", got)
	}
	want := []chunkedCall{{limit: 2, offset: 0}}
	if !slices.Equal(calls, want) {
		t.Fatalf("query calls = %v, want %v", calls, want)
	}
}

func TestChunked_yieldsErrorAndStops(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("query failed")
	var calls []chunkedCall
	query := chunkedQuery{
		calls: &calls,
		err:   wantErr,
	}

	var gotErr error
	var yields int
	for row, err := range Chunked(context.Background(), 2, query) {
		yields++
		if row != nil {
			t.Fatalf("error result row = %#v, want nil", row)
		}
		gotErr = err
	}

	if !errors.Is(gotErr, wantErr) {
		t.Fatalf("Chunked() error = %v, want %v", gotErr, wantErr)
	}
	if yields != 1 {
		t.Fatalf("yield count = %d, want 1", yields)
	}
}

func testNameForSize(size int) string {
	if size < 0 {
		return "negative"
	}
	return "zero"
}
