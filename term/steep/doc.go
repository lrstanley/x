// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package steep provides test helpers for [tea.Program] and models (root and component).
//
// Use NewHarness to run a root [tea.Model] in a test:
//
//	h := steep.NewHarness(t, model, steep.WithWindowSize(80, 24))
//	h.Type("hello").WaitSettle().Assert("hello")
//	h.QuitProgram()
//	h.WaitFinished()
//
// Use NewComponentHarness for components that expose View() string and an Update
// method through the asynchronous [tea.Program] runtime.
//
// # Hooks and export
//
// Harness actions invoke [Hook] callbacks before and after the action body. Built-in
// export hooks run first (see [WithExport], [WithFailureGIF]), then user hooks from
// [WithHooks]. Hook errors fail the test immediately.
//
// GIF frames are taken when the program commits terminal output, including
// each paint in a burst of keys, so a recording does not need [WithSlow].
// After hooks for input, terminal, and program wait until that paint is
// visible, then capture PNG and snapshot artifacts. Settle, ready, and close
// capture the screen those helpers already observed. [WithSlow] still sleeps
// before After hooks when a test wants the program to keep running between
// actions. [WithSlowExport] is the playback delay stamped on each GIF frame.
//
// Artifacts are written under [testing.TB.ArtifactDir] unless [WithExportDir] is
// set. Run tests with "go test -artifacts" (Go 1.26+) for them to be persisted
// (see "-outputdir"); without it, the artifact directory is temporary and
// removed when the test completes. Environment variables
// STEEP_EXPORT_MODE, STEEP_EXPORT_TYPE, STEEP_EXPORT_DIR, STEEP_SLOW, and related
// keys override options unless [WithIgnoreEnv] is set.
package steep
