// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package utils

import "sync"

// Pool is a generic wrapped over sync.Pool, that also allows for a prepare function
// to be called on the value before it is returned.
type Pool[T any] struct {
	init     sync.Once
	internal sync.Pool

	New     func() T
	Prepare func(v T) T
}

func (p *Pool[T]) Get() T {
	p.init.Do(func() {
		p.internal = sync.Pool{
			New: func() any { return p.New() },
		}
	})

	if p.Prepare == nil {
		return p.internal.New().(T)
	}
	return p.Prepare(p.internal.New().(T))
}

func (p *Pool[T]) Put(v T) {
	p.internal.Put(v)
}
