// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package flight_test

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lrstanley/x/sync/flight"
)

type User struct {
	ID   int
	Name string
}

func ExampleGroup_Do() {
	var (
		g  flight.Group[int, *User]
		wg sync.WaitGroup
	)

	fetch := func(id int) (*User, error) {
		time.Sleep(50 * time.Millisecond) // Simulate a slow lookup.
		if id <= 0 {
			return nil, fmt.Errorf("invalid user id %d", id)
		}
		return &User{ID: id, Name: "gopher"}, nil
	}

	users := make([]*User, 5)
	for i := range users {
		wg.Go(func() {
			u, err := g.Do(1, func() (*User, error) { return fetch(1) })
			if err != nil {
				panic(err)
			}
			users[i] = u
		})
	}
	wg.Wait()

	// Callers that overlapped share one lookup and receive the same *User. A
	// caller that arrives after a lookup completes starts a new one.
	fmt.Println(users[0].Name, users[0] == users[len(users)-1])
	// Output: gopher true
}

func ExampleGroup_DoContext() {
	var g flight.Group[string, string]

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// fn's context is canceled only if every caller waiting on "config" gives
	// up, not when any one of them does.
	v, err := g.DoContext(ctx, "config", func(ctx context.Context) (string, error) {
		return "loaded", ctx.Err()
	})
	fmt.Println(v, err)
	// Output: loaded <nil>
}

func ExampleGroup_DoChan() {
	var g flight.Group[string, int]

	select {
	case r := <-g.DoChan("answer", func() (int, error) { return 42, nil }):
		fmt.Println(r.Val, r.Err, r.Shared)
	case <-time.After(time.Second):
		fmt.Println("timed out")
	}
	// Output: 42 <nil> false
}
