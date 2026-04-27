// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package steep provides test helpers for Bubble Tea programs and models.
//
// Use NewHarness to run a root Bubble Tea model in a test:
//
//	h := steep.NewHarness(t, model, steep.WithInitialTermSize(80, 24))
//	h.Type("hello")
//	h.AssertStringContains(t, "hello")
//	if err := h.Quit(); err != nil {
//		t.Fatal(err)
//	}
//	h.WaitFinished(t)
//
// Use NewComponentHarness for components that expose View() string and an Update
// method through the asynchronous Bubble Tea runtime.
package steep
