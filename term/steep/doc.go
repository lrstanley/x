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
// Export capture runs on After for HookInput, HookTerminal, HookProgram, HookSettle,
// HookReady, and HookClose. Call [Harness.WaitSettle] or a wait helper after input
// when GIF/PNG frames must reflect the updated terminal.
//
// Artifacts are written under [testing.TB.ArtifactDir]. Environment variables
// STEEP_EXPORT_MODE, STEEP_EXPORT_TYPE, STEEP_EXPORT_DIR, STEEP_SLOW, and related
// keys override options unless [WithIgnoreEnv] is set.
package steep
