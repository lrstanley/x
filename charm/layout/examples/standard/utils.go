// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"cmp"
	"sync"

	"github.com/segmentio/ksuid"
)

func init() { //nolint:gochecknoinits
	ksuid.SetRand(ksuid.FastRander)
}

type uuid struct {
	once  sync.Once
	value string
}

func (u *uuid) UUID() string {
	u.once.Do(func() {
		u.value = ksuid.New().String()
	})
	return u.value
}

func clamp[T cmp.Ordered](v, min, max T) T {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
