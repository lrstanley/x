// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package reel emulates terminal programs and turns what they draw into
// images and video.
//
// A [Session] owns a terminal emulator that a program runs in: the program
// writes output to the session and reads input from it, while the host types,
// presses keys, pastes, clicks, resizes, focuses, scrolls the viewport, and
// sets colors. The session records each of these in a [Timeline], an
// append-only log of events with millisecond offsets from the first.
// Recordings are derived from a timeline by replaying it, so recording costs
// only a copy of the bytes while the program runs. [Mark] events name points
// in a timeline, and [Timeline.Between] cuts the part between two of them. A
// timeline is saved as an asciicast v2 file ([CastMediaType]) with
// [Timeline.WriteTo], and read back, including from files asciinema
// recorded, with [ReadTimeline].
//
// A driver connects a program to a session. [Exec] runs a command on a
// pseudo-terminal and records its exit. [Attach] replays output recorded
// elsewhere, such as a script(1) typescript with its timing file. A program
// that writes capability queries before its first paint, as Bubble Tea
// programs do, should open its session [WithSkipPristine].
//
// [Export] turns a timeline into files: GIF, MP4, WebM, animated WebP, a PNG
// of the last frame, per-mark stills, and asciicast. It replays the timeline
// into a fresh terminal, runs each frame through the [Effect] stages of a
// [Profile], and writes it to every output's [Sink]. Exports are
// deterministic (see [Timeline]). [Record] runs the same pipeline live on a
// [Session].
//
// A [Concealer] effect hides secrets, and a [Cutter] cuts stretches of the
// timeline out; see them for the rules on their order and outputs. Effects
// that implement [Describer] are recorded in asciicast outputs as
// [Annotation] events, so an export of the file can apply them again.
//
// Input is recorded as the exact bytes the terminal sent the program, encoded
// in whatever keyboard and mouse modes the program had set; [vt.DecodeInput]
// decodes them back into keys, text, pastes, and mouse events. The terminal's
// replies to the program's queries are recorded as [Query] annotations, not
// as input.
//
// The module is split into packages that can be used on their own:
//
//   - vt is a terminal emulator backed by a checked-in libghostty-vt Wasm
//     build.
//   - render rasterizes a screen into an image, with fonts, colors, window
//     chrome, cursor, and progress and status indicators.
//   - sink encodes frames to PNG and GIF, and through ffmpeg to MP4, WebM,
//     and animated WebP.
//   - effects has [Effect] values: a key overlay, a title card, pauses at
//     the end or at marks, highlights and zooms on cells, and redaction of
//     secrets.
package reel
