// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package steep provides test helpers for Bubble Tea programs and models.
//
// Use NewModel to run a root Bubble Tea model in a test:
//
//	tm := steep.NewModel(t, model, steep.WithInitialTermSize(80, 24))
//	tm.Type("hello")
//	tm.ExpectStringContains(t, "hello")
//	if err := tm.Quit(); err != nil {
//		t.Fatal(err)
//	}
//	tm.WaitFinished(t)
//
// Use NewViewModel to drive smaller components that expose View() string and an
// Update method without starting a full Bubble Tea program.
package steep
