// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package bloom

import (
	"math"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var lineBreaks = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ")

// MarqueeNode is a line of text that scrolls when it does not fit. Build one
// with [Marquee].
type MarqueeNode struct {
	s, gap string
	w, gw  int // the cell widths of s and gap, measured once
	phase  float64
	style  lipgloss.Style
	styled bool
}

// Marquee returns a single line showing s, a string that may hold ANSI
// styles and whose newlines are shown as spaces. Text that fits its width is
// shown as it is, at its natural width, so the node shrinks in a [Fill] slot.
// Text that does not fit is cut off with "…" while phase is 0, and otherwise
// scrolls: phase from 0 to 1 moves it one full loop, the text and the gap
// after it, wrapping around seamlessly. A phase outside that range wraps.
//
// Show a marquee at phase 0 until something, such as focus, gives it a
// phase to scroll with. A looping Sequence from package anim can drive it:
//
//	anim.NewSequence(0, anim.Jump(0), anim.To(1, anim.Duration(8*time.Second), anim.Ease(anim.Linear)))
func Marquee(s string, phase float64) MarqueeNode {
	if math.IsNaN(phase) || math.IsInf(phase, 0) {
		phase = 0
	}
	if strings.ContainsAny(s, "\r\n") {
		s = lineBreaks.Replace(s)
	}
	return MarqueeNode{s: s, gap: "   ", w: ansi.StringWidth(s), gw: 3, phase: phase}
}

// Style renders the text with style, at the size the node is given as
// [TextNode.Style] does, so a background fills it. The style's padding and
// borders count against the width the text has to scroll in.
func (n MarqueeNode) Style(style lipgloss.Style) MarqueeNode {
	n.style, n.styled = style, true
	return n
}

// Gap sets what is shown between the end of the text and its start again
// while scrolling, three spaces by default.
func (n MarqueeNode) Gap(s string) MarqueeNode {
	n.gap, n.gw = s, ansi.StringWidth(s)
	return n
}

// View implements [Child], drawing the marquee at its natural size.
func (n MarqueeNode) View() string { return Render(n, 0, 0) }

// Layout implements [Layouter].
func (n MarqueeNode) Layout(ctx Context) Child {
	inner := lineWidth(n.style, ctx.Width)
	if inner <= 0 {
		return nil
	}
	if n.w <= inner {
		return n.text(n.s)
	}
	if n.phase == 0 {
		return n.text(truncateLine(n.s, inner))
	}

	loop := n.s + n.gap
	period := n.w + n.gw
	off := int((n.phase - math.Floor(n.phase)) * float64(period))
	off = min(off, period-1)
	loop += loop

	// Cut keeps a wide glyph that starts before its left edge, which would
	// push everything after it out of place, so a glyph cut in half at the
	// left is replaced by a space.
	lead := ""
	if ansi.StringWidth(ansi.Cut(loop, 0, off)) < off {
		lead, off = " ", off+1
	}
	return n.text(padTo(lead+ansi.Cut(loop, off, off+inner-len(lead)), inner))
}

func (n MarqueeNode) text(s string) TextNode {
	if n.styled {
		return Text(s).Style(n.style)
	}
	return Text(s)
}

// padTo pads s with spaces to w cells, as truncating may leave it short when
// a wide glyph does not fit.
func padTo(s string, w int) string {
	if d := w - ansi.StringWidth(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// truncateLine preserves graphemes, padding a cut before a wide glyph.
func truncateLine(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return padTo(ansi.Truncate(s, width, "…"), width)
}

// lineWidth is the room for a line of text inside width, kept within the
// style's own width and MaxWidth, less its padding, borders, and margins.
func lineWidth(style lipgloss.Style, width int) int {
	if w := style.GetWidth(); w > 0 {
		width = min(width, w+style.GetHorizontalMargins())
	}
	if w := style.GetMaxWidth(); w > 0 {
		// Lip Gloss cuts the rendered lines, margins included, to MaxWidth.
		width = min(width, w)
	}
	return max(width-style.GetHorizontalFrameSize(), 0)
}
