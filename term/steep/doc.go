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
// the action body. Hook errors fail the test immediately. Once an action is
// done, it is marked in the timeline with its event and count, such as
// "input-2" or "wait-1", and each failure the harness reports is marked as
// "failure-1" and so on (see [Harness.Mark]).
//
// # Recording and export
//
// A harness runs its program in a [reel.Session], whose [reel.Timeline]
// records everything the program wrote and the test did. Recordings are made
// from the timeline, so a test never slows down to be recorded; [WithSlow] is
// only for programs that need real time between actions, and [reel.Delay],
// passed to [Harness.Type] and the like, for recordings that should show text
// being typed.
//
// The timeline also holds [reel.Annotation] events, described by reel's
// annotation.schema.json, of what the test did and found: its configuration
// ("steep.start"), the program's start and end ("steep.ready",
// "steep.exit"), inputs ("steep.input"), terminal changes and program
// actions ("steep.terminal", "steep.program"), waits ("steep.wait.start",
// "steep.wait.end"), assertions and snapshot comparisons ("steep.assert",
// "steep.snapshot"), failures ("steep.failure"), and the result
// ("steep.result"). Text fields over 4 KiB are cut and marked truncated.
// Messages the model receives ("steep.message") are only recorded with
// [WithRecordMessages].
//
// By default nothing is written: the timeline stays in memory, for
// [Harness.Timeline]. [WithExportOnFailure] exports when the test failed, and
// [WithExportAlways] whether it passed or failed. Either exports once, at
// [Harness.Close], which runs as a test cleanup, so a test that fails, calls
// FailNow, or panics still exports. A test killed by "go test -timeout"
// writes nothing.
//
// [WithRecording] sets what an export writes as a [reel.Profile]. Without
// outputs it writes "{name}_{status}.gif" and "{name}_{status}.cast" under
// [WithArtifacts], or [testing.TB.ArtifactDir], which is only kept with
// "go test -artifacts". [WithStills] adds a PNG or text snapshot at the marks
// of the hook events it names.
//
// steep adds two effects to those in the effects package: [Logo], an
// animated intro, and [Outro], a PASS or FAIL card. Effects run in the order
// the profile lists them, so [effects.Pause] before Outro pauses on the screen
// the test failed on:
//
//	steep.WithRecording(reel.Profile{
//		Effects: []reel.Effect{
//			steep.Logo(2 * time.Second),
//			effects.Pause(),
//			effects.Keys(time.Second),
//			steep.Outro(2 * time.Second),
//		},
//	}),
//	steep.WithExportOnFailure(),
//
// The ".cast" output is the timeline itself, from which [reel.ReadTimeline]
// and [reel.Export] can write the other outputs again. A profile with
// [effects.Redact] or [effects.RedactInputs] must therefore set outputs
// without ".cast" (see [reel.ErrRawOutput]).
//
// # Environment
//
// These variables override the options set in code unless [WithIgnoreEnv]
// is set, so any test can be recorded or tuned without changing it.
// Unparsable values are ignored, except in STEEP_EXPORT.
//
//   - STEEP_TIMEOUT, STEEP_CHECK, STEEP_SETTLE, STEEP_SLOW: durations for
//     [WithTimeout], [WithCheck], [WithSettle], and [WithSlow].
//   - STEEP_TYPING_DELAY: a duration to pause after each key press of
//     [Harness.Type] and [Harness.Key] ([reel.WithTypingDelay]), unless a
//     call passes [reel.Delay].
//   - STEEP_LOG: true or 1 adds the hook of [WithLog].
//   - STEEP_RECORD_MESSAGES: a boolean for [WithRecordMessages].
//   - STEEP_SNAPSHOT_DIR: the default snapshot directory
//     ([snapshot.WithDir]); a directory passed to a snapshot call still wins.
//   - STEEP_EXPORT_MODE: on (or always, true, 1) for [WithExportAlways],
//     failure (or on-failure) for [WithExportOnFailure], off (or false, 0)
//     for neither.
//   - STEEP_EXPORT_DIR: the directory of [WithArtifacts].
//   - STEEP_EXPORT: replaces the outputs and stills with a comma-separated
//     list of outputs (see [reel.ParseFiles]), each a path with optional
//     query parameters, such as "{name}_{status}.gif?max-bytes=5MB" or
//     "{name}_{mark}.snap?match=input-*". A path that holds {mark} with no
//     marks or match parameter is a still at each hook mark of the events
//     [WithStills] uses by default. An output that does not parse fails the
//     test.
//   - STEEP_EXPORT_FPS, STEEP_EXPORT_SPEED: replace [reel.Profile.FPS] and
//     [reel.Profile.Speed].
//   - STEEP_RENDERER_SCALE, STEEP_RENDERER_FONT_FAMILY: appended to
//     [reel.Profile.Render].
//
// For example:
//
//	STEEP_EXPORT_MODE=on STEEP_EXPORT='{name}.mp4,{name}_{mark}.png?match=wait-*' go test -artifacts ./...
package steep
