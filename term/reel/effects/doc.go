// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package effects holds [reel.Effect] values for a [reel.Profile]: [Keys]
// overlays the keys pressed in the timeline, [Intro] shows a title card
// before the first frame, and [PauseBadge] marks the held last frame with a
// pause glyph, so a looping recording reads as finished rather than frozen.
//
// Each effect keeps the state of a recording in the stage it starts for it,
// so one effect value can be shared by profiles that record in parallel.
//
// A [CardRenderer] draws the cards, for effects of a caller's own too:
// lines of text on a blank terminal that matches the recording's frames.
package effects
