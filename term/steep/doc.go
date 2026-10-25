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
// export hooks run first (see [WithExport], [WithFailureGIF], [WithFailurePNG]), then
// user hooks from [WithHooks]. Hook errors fail the test immediately.
//
// GIF frames are taken when the program commits terminal output, including
// each paint in a burst of keys, so a recording does not need [WithSlow].
// MP4, WebM, and WebP videos (see [WithExport], they need ffmpeg) are recorded
// from the same frames.
// After hooks for input, terminal, and program wait until that paint is
// visible, then capture PNG and snapshot artifacts. Settle, ready, and close
// capture the screen those helpers already observed. [WithSlow] still sleeps
// before After hooks when a test wants the program to keep running between
// actions.
//
// Each GIF or video frame plays for the real time it was on screen, snapped to
// the [WithExportFPS] grid (default 20) with a minimum of one step, so the test
// never sleeps to pace the recording. [WithExportSpeed] speeds up or slows down
// playback without affecting the test.
//
// Artifacts are written under [testing.TB.ArtifactDir] unless [WithExportDir] is
// set. Run tests with "go test -artifacts" (Go 1.26+) for them to be persisted
// (see "-outputdir"); without it, the artifact directory is temporary and
// removed when the test completes. Environment variables
// STEEP_EXPORT_MODE, STEEP_EXPORT_TYPE, STEEP_EXPORT_DIR, STEEP_SLOW, and related
// keys override options unless [WithIgnoreEnv] is set.
package steep
