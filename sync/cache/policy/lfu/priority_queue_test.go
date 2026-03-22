// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package lfu

import (
	"container/heap"
	"math/rand/v2"
	"reflect"
	"testing"
	"time"
)

func TestPriorityQueue(t *testing.T) {
	var nums []int
	for range 10 {
		nums = append(nums, rand.IntN(10)+1)
	}
	queue := newPriorityQueue[int, int](len(nums))
	entries := make([]*entry[int, int], 0, len(nums))

	for _, v := range nums {
		entry := newEntry(v, v)
		entries = append(entries, entry)
		heap.Push(queue, entry)
	}

	if got := queue.Len(); len(nums) != got {
		t.Errorf("want %d, but got %d", len(nums), got)
	}

	for idx, entry := range *queue {
		if entry.index != idx {
			t.Errorf("want index %d, but got %d", entry.index, idx)
		}
		if entry.referenceCount != 1 {
			t.Errorf("want count 1")
		}
		if got := entry.val; nums[idx] != got {
			t.Errorf("want value %d but got %d", nums[idx], got)
		}
	}

	for i := range len(nums) - 1 {
		entry := entries[i]
		queue.update(entry, nums[i])
		time.Sleep(time.Millisecond)
	}

	wantValue := nums[len(nums)-1]
	got := heap.Pop(queue).(*entry[int, int]) //nolint:errcheck
	if got.index != -1 {
		t.Errorf("want index -1, got %d", got.index)
	}
	if wantValue != got.val {
		t.Errorf("want the lowest priority value is %d, got %d", wantValue, got.val)
	}
	if want, got := len(nums)-1, queue.Len(); want != got {
		t.Errorf("want %d, got %d", want, got)
	}

	wantValue2 := nums[0]
	got2 := heap.Pop(queue).(*entry[int, int]) //nolint:errcheck
	if got.index != -1 {
		t.Errorf("want index -1, got %d", got.index)
	}
	if wantValue2 != got2.val {
		t.Errorf("the lowest priority value is %d, got %d", wantValue2, got2.val)
	}
	if want, got := len(nums)-2, queue.Len(); want != got {
		t.Errorf("want %d, got %d", want, got)
	}
}

func TestPriorityQueueSwap(t *testing.T) {
	type testCase[K comparable, V any] struct {
		name  string
		queue *priorityQueue[K, V]
		i, j  int
		want  *priorityQueue[K, V]
	}
	tests := []testCase[string, int]{
		{
			name: "swap case",
			queue: func() *priorityQueue[string, int] {
				q := newPriorityQueue[string, int](10)
				q.Push(&entry[string, int]{index: 0})
				q.Push(&entry[string, int]{index: 1})
				return q
			}(),
			i: 0,
			j: 1,
			want: func() *priorityQueue[string, int] {
				q := newPriorityQueue[string, int](10)
				q.Push(&entry[string, int]{index: 1})
				q.Push(&entry[string, int]{index: 0})
				return q
			}(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.queue.Swap(tt.i, tt.j)
			if !reflect.DeepEqual(tt.queue, tt.want) {
				t.Errorf("want %v, got %v", tt.want, tt.queue)
			}
		})
	}
}

func TestPriorityQueue_Pop(t *testing.T) {
	t.Run("pop from empty queue", func(t *testing.T) {
		pq := newPriorityQueue[int, string](0)
		if elem := heap.Pop(pq); elem != nil {
			t.Errorf("expected nil from empty queue, got %v", elem)
		}
	})

	t.Run("pop from queue with single element", func(t *testing.T) {
		pq := newPriorityQueue[int, string](10)
		heap.Push(pq, newEntry(1, "one"))
		if pq.Len() != 1 {
			t.Fatalf("expected queue length of 1, got %d", pq.Len())
		}
		elem := heap.Pop(pq).(*entry[int, string]) //nolint:errcheck
		if elem.key != 1 || elem.val != "one" {
			t.Errorf("expected to pop element with key=1 and val='one', got key=%d and val='%s'", elem.key, elem.val)
		}
		if pq.Len() != 0 {
			t.Errorf("expected empty queue after pop, got length %d", pq.Len())
		}
	})

	t.Run("pop from queue with multiple elements", func(t *testing.T) {
		pq := newPriorityQueue[int, string](10)
		heap.Push(pq, newEntry(1, "one"))
		heap.Push(pq, newEntry(2, "two"))
		heap.Push(pq, newEntry(3, "three"))

		elem := heap.Pop(pq).(*entry[int, string]) //nolint:errcheck
		if elem.key != 1 || elem.val != "one" {
			t.Errorf("expected to pop element with key=1 and val='one', got key %d and val %q", elem.key, elem.val)
		}

		elem = heap.Pop(pq).(*entry[int, string]) //nolint:errcheck
		if elem.key != 2 || elem.val != "two" {
			t.Errorf("expected to pop element with key=2 and val='two', got key %d and val %q", elem.key, elem.val)
		}

		elem = heap.Pop(pq).(*entry[int, string]) //nolint:errcheck
		if elem.key != 3 || elem.val != "three" {
			t.Errorf("expected to pop element with key=3 and val='three', got key %d and val %q", elem.key, elem.val)
		}

		if pq.Len() != 0 {
			t.Errorf("expected empty queue after all pops, got length %d", pq.Len())
		}
	})
}
