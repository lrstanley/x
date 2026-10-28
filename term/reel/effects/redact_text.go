// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package effects

import (
	"fmt"
	"image"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/vt"
)

// Matching is an option [Redact] and [RedactInputs] take, such as
// [MinPartial].
type Matching func(*matching)

// matching is how the text of [Text] and [Env] regions is matched.
type matching struct {
	// minPartial is the fewest characters of a secret hidden where only its
	// start or end shows.
	minPartial int
}

// defaultMatching is the matching without options.
var defaultMatching = matching{minPartial: 3}

// MinPartial sets the fewest characters of the start or end of a [Text] or
// [Env] secret that [Redact] and [RedactInputs] hide where only that part
// of it shows. A start is hidden where it is cut off at the end of a line,
// at the cursor, or before a character that is not a letter or digit, as
// while it is typed into a prompt; an end at the start of a line or after
// such a character, as when its first wrapped row has scrolled away. A
// secret of n characters or fewer is only hidden whole, so an n as long as
// the longest secret turns partial matching off. The default is 3: a lower
// n shows less of a secret as it is typed, but hides more unrelated text
// that starts or ends like it. An n below 1 is an error from Start.
func MinPartial(n int) Matching {
	return func(m *matching) { m.minPartial = n }
}

func (o Matching) applyRedact(r *redact) {
	if o != nil {
		o(&r.matching)
	}
}

// check returns an error for a matching that matches nothing.
func (m matching) check() error {
	if m.minPartial < 1 {
		return fmt.Errorf("effects: min partial %d is below 1", m.minPartial)
	}
	return nil
}

// redactMask is the character hidden text shows as outside the cells: in a
// title, in program status, and in typed text.
const redactMask = '•'

// secrets is what a region given to [Redact] or [RedactInputs] looks for,
// as of one frame: the literals of [Text] and [Env] regions (see
// [secrets.ranges]), the regular expressions of [Regexp] regions, and the
// other regions, which find no text and are matched with their Rects.
type secrets struct {
	literals   []string
	minPartial int
	res        []*regexp.Regexp
	others     []Region
}

// secretsOf returns what region looks for now, matched with m. The values
// of an [Env] region's variables are read when it is called.
func secretsOf(region Region, m matching) secrets {
	s := secrets{minPartial: m.minPartial}
	addSecrets(&s, region)
	s.literals = textNeedles(s.literals)
	return s
}

// addSecrets adds what region looks for to s.
func addSecrets(s *secrets, region Region) {
	switch r := region.(type) {
	case nil:
	case anyRegion:
		for _, sub := range r {
			addSecrets(s, sub)
		}
	case textRegion:
		s.literals = append(s.literals, string(r))
	case envRegion:
		s.literals = append(s.literals, r.values()...)
	case regexpRegion:
		if r.re != nil {
			s.res = append(s.res, r.re)
		}
	default:
		s.others = append(s.others, r)
	}
}

// findsText reports whether s looks for any text.
func (s secrets) findsText() bool {
	return len(s.literals) > 0 || len(s.res) > 0
}

// ranges returns the byte ranges of text that s matches, which may
// overlap: every match of each regular expression but empty ones, every
// literal, every prefix of a literal of at least s.minPartial characters
// that ends where text does, at byte cursor, or before a character that is
// not a letter or digit, and every suffix of one as long that starts where
// text does, or after a character that is not a letter or digit. A literal
// of s.minPartial characters or fewer matches only whole.
func (s secrets) ranges(text string, cursor int) [][2]int {
	var out [][2]int
	for _, lit := range s.literals {
		out = appendLiteral(out, text, lit, cursor, s.minPartial)
		out = appendSuffix(out, text, lit, s.minPartial)
	}
	for _, re := range s.res {
		for _, m := range re.FindAllStringIndex(text, -1) {
			if m[1] > m[0] {
				out = append(out, [2]int{m[0], m[1]})
			}
		}
	}
	return out
}

// appendLiteral appends the ranges of text that lit and its prefixes match
// to out (see [secrets.ranges]).
func appendLiteral(out [][2]int, text, lit string, cursor, minPartial int) [][2]int {
	// least is the length of the shortest prefix that matches: the first
	// minPartial characters, or all of lit.
	least, chars := len(lit), 0
	for i := range lit {
		if chars == minPartial {
			least = i
			break
		}
		chars++
	}
	for i := 0; i < len(text); {
		j := strings.IndexByte(text[i:], lit[0])
		if j < 0 {
			break
		}
		i += j
		n := commonPrefix(text[i:], lit)
		if n == len(lit) || (n >= least && prefixEnds(text, i+n, cursor)) {
			out = append(out, [2]int{i, i + n})
			i += n
			continue
		}
		i++
	}
	return out
}

// appendSuffix appends the ranges of text that suffixes of lit match to
// out, the longest at each start (see [secrets.ranges]).
func appendSuffix(out [][2]int, text, lit string, minPartial int) [][2]int {
	// starts are where the suffixes of lit that can match start, longest
	// first.
	var starts []int
	for i := range lit {
		if i > 0 && utf8.RuneCountInString(lit[i:]) >= minPartial {
			starts = append(starts, i)
		}
	}
	if len(starts) == 0 {
		return out
	}
	for i := 0; i < len(text); {
		_, n := utf8.DecodeRuneInString(text[i:])
		if r, _ := utf8.DecodeLastRuneInString(text[:i]); i == 0 || !wordChar(r) {
			for _, j := range starts {
				if strings.HasPrefix(text[i:], lit[j:]) {
					out = append(out, [2]int{i, i + len(lit) - j})
					break
				}
			}
		}
		i += n
	}
	return out
}

// wordChar reports whether r is a letter or digit. A partial secret only
// matches where it is not next to one.
func wordChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// commonPrefix returns the length of the longest prefix of lit that a
// starts with, in whole characters of lit.
func commonPrefix(a, lit string) int {
	n := 0
	for n < len(a) && n < len(lit) && a[n] == lit[n] {
		n++
	}
	for n < len(lit) && n > 0 && !utf8.RuneStart(lit[n]) {
		n--
	}
	return n
}

// prefixEnds reports whether a prefix of a secret that ends at byte i of
// text could be the secret cut off: i is where text ends, or the cursor, or
// the character there is not a letter or digit.
func prefixEnds(text string, i, cursor int) bool {
	if i >= len(text) || i == cursor {
		return true
	}
	r, _ := utf8.DecodeRuneInString(text[i:])
	return !wordChar(r)
}

// rects returns the cells s covers on f: the ranges of each line of f's
// screen, with the cursor's cell, and the Rects of the other regions.
func (s secrets) rects(f *reel.CellFrame) []image.Rectangle {
	var out []image.Rectangle
	for _, r := range s.others {
		out = append(out, r.Rects(f)...)
	}
	if !s.findsText() {
		return out
	}
	cur := f.State.Cursor
	for line := range screenLines(f) {
		for _, m := range s.ranges(line.text, line.offsetAt(cur.X, cur.Y)) {
			out = line.appendMatch(out, m[0], m[1])
		}
	}
	return out
}

// scrub returns text with every character of the ranges s matches replaced
// by redactMask, so it keeps its length in characters.
func (s secrets) scrub(text string) string {
	ranges := s.ranges(text, -1)
	if len(ranges) == 0 {
		return text
	}
	hidden := make([]bool, len(text))
	for _, m := range ranges {
		for i := m[0]; i < m[1]; i++ {
			hidden[i] = true
		}
	}
	var b strings.Builder
	for i := 0; i < len(text); {
		_, n := utf8.DecodeRuneInString(text[i:])
		if slices.Contains(hidden[i:i+n], true) {
			b.WriteRune(redactMask)
		} else {
			b.WriteString(text[i : i+n])
		}
		i += n
	}
	return b.String()
}

// scrubStatuses returns statuses with the text of each record scrubbed (see
// [secrets.scrub]), and whether any changed. statuses is not modified.
func (s secrets) scrubStatuses(statuses []vt.ProgramStatus) ([]vt.ProgramStatus, bool) {
	var out []vt.ProgramStatus
	for i, st := range statuses {
		app, title, msg := s.scrub(st.App), s.scrub(st.Title), s.scrub(st.Message)
		if app == st.App && title == st.Title && msg == st.Message {
			continue
		}
		if out == nil {
			out = append([]vt.ProgramStatus(nil), statuses...)
		}
		out[i].App, out[i].Title, out[i].Message = app, title, msg
	}
	if out == nil {
		return statuses, false
	}
	return out, true
}
