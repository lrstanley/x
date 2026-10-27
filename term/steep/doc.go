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
// # Hooks
//
// Harness actions invoke [Hook] callbacks from [WithHooks] before and after
// the action body. Hook errors fail the test immediately. Each action is also
// marked in the session's timeline once it is done, labeled with its event
// and count, such as "input-2" (see [Harness.Mark]).
//
// # Recording and export
//
// A harness runs its program in a [reel.Session], whose [reel.Timeline]
// records every byte the program wrote and everything the test did to the
// terminal, at a cost of a copy of the bytes. Recordings are made from the
// timeline, so a test never sleeps or slows down to be recorded, and
// [WithSlow] is only for programs that need real time between actions.
//
// By default nothing is written: the timeline stays in memory, for
// [Harness.Timeline], since a failing test already prints the screen as
// text, and artifacts written into a package directory may not be ignored
// by version control. An export policy turns exporting on:
// [WithExportOnFailure] exports when the test failed, and [WithExportAlways]
// whatever the result. Either exports once, at [Harness.Close], which runs as
// a cleanup of the test, so a test that fails, calls FailNow, or panics still
// exports. A test killed by "go test -timeout" writes nothing.
//
// [WithRecording] sets what an export writes as a [reel.Profile]: its
// outputs (GIF, MP4, WebM, WebP, PNG, and asciicast), renderer, encoder
// options, effects, and timing. Without outputs it writes
// "{name}_{status}.gif" and "{name}_{status}.cast", where {name} is the
// test's name and {status} "passed" or "failed", under [WithArtifacts], or
// [testing.TB.ArtifactDir], which is only kept when tests run with
// "go test -artifacts". [WithStills] adds a PNG or text snapshot at each mark
// of the hook events it names.
//
// Effects run in the order the profile lists them. steep adds two to those in
// the effects package: [Logo], an animated intro, and [Outro], a PASS or FAIL
// card with the failures the harness reported, from the [Result] each export
// passes them. A failed test's last frame holds for 3s, unless the profile
// sets a hold, so the screen it failed on can be read, and
// [effects.PauseBadge] marks that frame before the FAIL card:
//
//	steep.WithRecording(reel.Profile{
//		Effects: []reel.Effect{
//			steep.Logo(2 * time.Second),
//			effects.Keys(time.Second),
//			effects.PauseBadge(),
//			steep.Outro(2 * time.Second),
//		},
//	}),
//	steep.WithExportOnFailure(),
//
// The ".cast" output is the recording itself: [reel.ReadTimeline] and
// [reel.Export] with the same profile and [Result], and for a failed test the
// 3s hold, write the same files again, byte for byte, but for the exceptions
// [reel.Timeline] lists. So a profile with [effects.Redact] or
// [effects.RedactInputs] must set outputs without ".cast". Stills run
// through them, and no other effect.
//
// Environment variables override these options unless [WithIgnoreEnv] is
// set: STEEP_EXPORT_MODE (on, failure, or off) sets the policy,
// STEEP_EXPORT_TYPE the outputs and stills, STEEP_EXPORT_DIR the artifacts
// directory, and STEEP_EXPORT_FPS, STEEP_EXPORT_SPEED, STEEP_EXPORT_QUALITY,
// STEEP_EXPORT_MAX_FRAMES, STEEP_EXPORT_MAX_BYTES, STEEP_EXPORT_OPTIMIZE,
// STEEP_RENDERER_SCALE, and STEEP_RENDERER_FONT_FAMILY patch the profile (see
// [WithRecording]), so any test can be recorded without changing it:
//
//	STEEP_EXPORT_MODE=on STEEP_EXPORT_TYPE=gif,mp4 go test -artifacts ./...
package steep
