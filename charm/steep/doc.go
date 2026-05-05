// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package steep provides test helpers for [tea.Program] and models (root and component).
//
// Use NewHarness to run a root [tea.Model] in a test:
//
//	h := steep.NewHarness(t, model, steep.WithWindowSize(80, 24))
//	h.Type("hello").WaitSettle().AssertString("hello")
//	h.QuitProgram()
//	h.WaitFinished()
//
// Use NewComponentHarness for components that expose View() string and an Update
// method through the asynchronous [tea.Program] runtime.
package steep
