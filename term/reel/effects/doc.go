// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package effects holds [reel.Effect] values for a [reel.Profile]: [Keys]
// overlays the keys pressed in the timeline, [Intro] shows a title card
// before the first frame, [PauseBadge] marks the held last frame with a
// pause glyph, so a looping recording reads as finished rather than frozen,
// [Highlight] draws attention to part of the screen, and [Zoom] zooms in on
// it. [Redact] hides secrets from the frames before anything draws them,
// and [RedactInputs] hides them from the keys Keys shows.
//
// Highlight, Zoom, and Redact work on a [Region] of the terminal's cells:
// fixed cells ([Cells], [Row], [Rows]), the cells that show some text
// ([Text], [Regexp], [Env]), which follow it as it moves, or [Any] of
// them. Every effect that works on a region takes the [Timing] options,
// [Between], which limits it to a window between two marks, and [Ease],
// [EaseIn], and [EaseOut], which ease it in and out, besides options of its
// own, such as [DimOthers] and [Outline] for Highlight, [Scale] and [Anchor]
// for Zoom, and [Block], [Blur], and [Replace] for Redact, which takes only
// Between, since easing would show part of what it hides. Redact and
// [RedactInputs] share [MinPartial], how much of the start or end of a
// secret they hide where only that part of it shows.
//
// Redact and RedactInputs are [reel.Concealer] values, listed before every
// other effect. A redacted recording can not be saved as asciicast, and
// its stills of [reel.File.Marks] are redacted too.
//
// Each effect keeps the state of a recording in the stage it starts for it,
// so one effect value can be shared by profiles that record in parallel.
//
// A [CardRenderer] draws the cards, for effects of a caller's own too:
// lines of text on a blank terminal that matches the recording's frames.
package effects
