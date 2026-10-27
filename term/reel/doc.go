// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package reel emulates terminal programs and turns what they draw into
// images and video.
//
// A [Session] owns a terminal emulator that a program runs in: the program
// writes its output to the session and reads its input from it, while the
// host types, presses keys, pastes, clicks, resizes, focuses, and sets colors.
// The session records each of these in a [Timeline], an append-only event log
// of the bytes and host actions that changed the terminal, with each event's
// offset from the first, in whole milliseconds. Recordings are derived from
// a timeline by replaying it, so recording costs a copy of the bytes and
// nothing more while the program runs. [Mark] events name points in a
// timeline, and [Timeline.Between] cuts the part between two of them. A
// timeline is saved as an asciicast v3 file (".cast", [CastMediaType]) with
// [Timeline.WriteTo], which asciinema plays, and read back, or read from a
// file asciinema recorded, with [ReadTimeline].
//
// A driver connects a program to a session. [Exec] runs a command on a
// pseudo-terminal, copying its output to the session and the session's
// input to it, and records its exit. [Attach] writes output read from
// elsewhere, such as a typescript script(1) recorded, timed by its timing
// file. steep, a test harness for Bubble Tea programs, runs the program
// in the test on a session. A program that writes capability queries
// before its first paint, as Bubble Tea programs do, wants its session
// opened [WithSkipPristine], so recordings start on that paint.
//
// [Export] turns a timeline into files: GIF, MP4, WebM, animated WebP, a PNG
// of the last frame, per-mark stills, and asciicast. It replays the timeline
// into a fresh terminal, draws each change the terminal shows as a frame,
// runs the frames through the [Effect] stages of a [Profile], and writes them
// to the [Sink] of every output, with delays taken from the timeline, never
// the wall clock, so exporting a timeline twice writes the same bytes.
// [Record] runs the same pipeline on a [Session] while it runs, tailing its
// timeline with [Timeline.Updated], and writes the files Export would. An
// effect that hides secrets is a [Concealer], which runs before every other
// effect, and on stills too, which skip the others but for a [StillEffect],
// and makes asciicast outputs, the timeline as recorded, an error.
//
// Input is recorded as the exact bytes the terminal sent the program, encoded
// in whatever keyboard and mouse modes the program had set.
// [vt.DecodeInput] decodes them back into key names, typed text, pastes, and
// mouse events.
//
// The module is split into packages that can be used on their own:
//
//   - vt is a terminal emulator backed by a checked-in libghostty-vt Wasm
//     build, with the cell buffer and terminal state it fills and a renderer
//     reads.
//   - render rasterizes a screen into an image: fonts, colors, window chrome,
//     cursor, and progress and status indicators. Its fonts, types, and units
//     subpackages hold the bundled fonts and shared option types.
//   - sink encodes frames to PNG and GIF, and through ffmpeg to MP4, WebM,
//     and animated WebP.
//   - effects has [Effect] values: an overlay of the keys pressed, a title
//     card before the first frame, drawn with a card renderer that effects
//     of a caller's own can use too, a pause badge on the held last frame,
//     a highlight of cells, and a zoom on them, drawn sharp, found by
//     position or by the text they show, between two marks, and redaction
//     of secrets, from the screen and from the keys shown.
package reel
