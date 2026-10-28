// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package effects holds [reel.Effect] values for a [reel.Profile]: [Keys]
// overlays the keys pressed, [Intro] shows a title card before the first
// frame, [Pause] pauses playback at the end or at marks, [Highlight] draws
// attention to part of the screen, [Zoom] zooms in on it, and [Redact] and
// [RedactInputs] hide secrets from the frames and from the keys Keys shows.
// [Hide] cuts stretches out of playback: between marks, or while the screen
// shows, or does not show, a [Region].
//
// Highlight, Zoom, and Redact work on a [Region] of the terminal's cells:
// fixed cells ([Cells], [Row], [Rows]), cells that show some text ([Text],
// [Regexp], [Env]), which follow it as it moves (a [Zoom] holds what it
// matched when it started), or [Any] of them. They take
// the [Timing] options besides their own; Redact takes only [Between].
// Frames their region covers no cells of are left alone.
//
// Highlight, Keys, Redact, and Zoom leave frames without a terminal
// ([reel.Frame.Cells] is nil), such as cards, alone.
//
// # Order
//
// Effects run in the order a profile lists them. List them as:
//
//  1. Redact and RedactInputs, which are [reel.Concealer] values and must
//     come first.
//  2. Hide, so the pauses at marks and the keys pressed in what it cuts are
//     left out too.
//  3. Zoom, which draws its zoomed cells over what the effects before it
//     drew.
//  4. Highlight, so the overlays after it are not dimmed.
//  5. Pause, so its badge is drawn over the effects before it.
//  6. Keys, which then shows the keys as they were when a pause started.
//
// Intro may go anywhere after the concealers.
//
// Each effect keeps per-recording state in the stage it starts, so one
// effect value can be shared by profiles that record in parallel.
//
// Hide is a [reel.Cutter], so, as with Redact and RedactInputs, a recording
// it cuts can not be saved as asciicast. Every other effect is a
// [reel.Describer]: an asciicast output records it as an annotation of kind
// [reel.EffectKind], and [FromTimeline] ([FromAnnotation] for one) reads it
// back, so an export of the file applies it again. A description is lossy
// in a few ways:
//
//   - A region of a caller's own, or [Any] of one, has none: Describe
//     returns an error wrapping [errors.ErrUnsupported], and the file leaves
//     the effect out.
//   - An [Env] region is described by its variable names, never their
//     values.
//   - A [Regexp] region is described by its source text, so it reads back
//     with leftmost-first matching ([regexp.Regexp.Longest] is lost).
//   - Durations are rounded to the millisecond.
//   - Palette colors of a [Line]'s style read back as [vt.Indexed].
//
// Options Start would reject are an error from Describe too. The described
// effects that look for marks are each a [reel.MarkChecker], so a file never
// names marks it does not have.
//
// A [CardRenderer] draws cards, lines of text on a blank terminal that
// matches the recording's frames, for effects of a caller's own too.
package effects
