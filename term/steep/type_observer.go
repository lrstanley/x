// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package steep

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"
)

type observedType struct {
	rtype         reflect.Type
	count         int
	firstReceived time.Time
	lastReceived  time.Time
}

// typeObserver counts the types of values it observes, and when each was
// first and last seen, for failure messages.
type typeObserver[T any] struct {
	mu    sync.RWMutex
	types map[string]*observedType
}

func newTypeObserver[T any]() *typeObserver[T] {
	return &typeObserver[T]{
		types: make(map[string]*observedType),
	}
}

func (o *typeObserver[T]) observe(v ...T) *typeObserver[T] {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, v := range v {
		typ := reflect.TypeOf(v)
		now := time.Now()
		if _, ok := o.types[typ.String()]; !ok {
			o.types[typ.String()] = &observedType{
				rtype:         typ,
				firstReceived: now,
			}
		}
		obs := o.types[typ.String()]
		obs.count++
		obs.lastReceived = now
	}
	return o
}

func (o *typeObserver[T]) sortedLastReceived() []*observedType {
	o.mu.RLock()
	defer o.mu.RUnlock()
	values := slices.Collect(maps.Values(o.types))
	slices.SortFunc(values, func(a, b *observedType) int {
		return b.lastReceived.Compare(a.lastReceived)
	})
	return values
}

func (o *typeObserver[T]) String() string {
	b := strings.Builder{}
	b.WriteString("observed types:")
	if len(o.types) == 0 {
		fmt.Fprint(&b, " <none>")
		return b.String()
	}
	for _, obs := range o.sortedLastReceived() {
		fmt.Fprintf(&b,
			"\n  %q x%d -- first: %s ago",
			obs.rtype.String(),
			obs.count,
			time.Since(obs.firstReceived),
		)
		if !obs.firstReceived.Equal(obs.lastReceived) {
			fmt.Fprintf(&b,
				", last: %s ago",
				time.Since(obs.lastReceived),
			)
		}
	}
	return b.String()
}
