// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"encoding"
	"fmt"
	"image/color"
	"strconv"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/reel/vt"
)

// Screen is the terminal at one moment, as JSON: its cells, cursor, colors,
// and the state around them. [Session.Screen] returns the live one, and a
// ".json" output (see [File]) writes it from a replay.
type Screen struct {
	Title         string          `json:"title,omitempty"`
	Rows          int             `json:"rows"`
	Cols          int             `json:"cols"`
	AltScreen     bool            `json:"alt_screen,omitempty"`
	Focused       bool            `json:"focused,omitempty"`
	Cursor        ScreenCursor    `json:"cursor,omitempty"`
	FgColor       ScreenColor     `json:"fg_color,omitzero"`
	BgColor       ScreenColor     `json:"bg_color,omitzero"`
	ReverseColors bool            `json:"reverse_colors,omitempty"`
	ProgramStatus []ProgramStatus `json:"program_status,omitempty"`
	// Cells are the rows of cells, top first. A nil cell is blank: no
	// content, style, or link.
	Cells [][]*ScreenCell `json:"cells"`
}

// ScreenPosition is a cell position, from 0.
type ScreenPosition struct {
	X int `json:"x"`
	Y int `json:"y"`
}

var (
	_ encoding.TextMarshaler   = ScreenColor{}
	_ encoding.TextUnmarshaler = (*ScreenColor)(nil)
)

// ScreenColor is a color of a [Screen], written as text: a palette color's
// number (0 to 255), or "#rrggbb". A nil Color is written as "".
type ScreenColor struct {
	Color color.Color
}

// MarshalText implements [encoding.TextMarshaler].
func (c ScreenColor) MarshalText() ([]byte, error) {
	switch col := c.Color.(type) {
	case nil:
		return []byte{}, nil
	case ansi.BasicColor, ansi.IndexedColor, vt.Indexed:
		return fmt.Appendf(nil, "%d", col), nil
	default:
		r, g, b, _ := c.Color.RGBA()
		return fmt.Appendf(nil, "#%02x%02x%02x", r>>8, g>>8, b>>8), nil
	}
}

// UnmarshalText implements [encoding.TextUnmarshaler]. 0 to 15 read as
// [ansi.BasicColor], and 16 to 255 as [ansi.IndexedColor].
func (c *ScreenColor) UnmarshalText(text []byte) error {
	s := string(text)
	if s == "" {
		return nil
	}
	if i, err := strconv.Atoi(s); err == nil {
		switch {
		case i >= 0 && i <= 15:
			c.Color = ansi.BasicColor(i)
			return nil
		case i >= 16 && i <= 255:
			c.Color = ansi.IndexedColor(i)
			return nil
		}
	}
	col := ansi.XParseColor(s)
	if col == nil {
		return fmt.Errorf("reel: invalid color %q", s)
	}
	c.Color = col
	return nil
}

// ScreenCursor is the cursor of a [Screen].
type ScreenCursor struct {
	Position ScreenPosition `json:"position"`
	Visible  bool           `json:"visible"`
	Color    ScreenColor    `json:"color,omitzero"`
	Style    vt.CursorShape `json:"style"`
	Blink    bool           `json:"blink"`
}

// CellStyle is the style of a [ScreenCell].
type CellStyle struct {
	Fg             ScreenColor  `json:"fg,omitzero"`
	Bg             ScreenColor  `json:"bg,omitzero"`
	UnderlineColor ScreenColor  `json:"underline_color,omitzero"`
	Underline      vt.Underline `json:"underline,omitempty"`
	Attrs          vt.Attr      `json:"attrs,omitempty"`
}

// CellLink is the hyperlink of a [ScreenCell].
type CellLink struct {
	URL    string `json:"url,omitempty"`
	Params string `json:"params,omitempty"`
}

// ScreenCell is one cell of a [Screen].
type ScreenCell struct {
	// Content is one grapheme cluster, often a single rune.
	Content string    `json:"content,omitempty"`
	Style   CellStyle `json:"style,omitzero"`
	Link    CellLink  `json:"link,omitzero"`
	// Width is the cluster's width in cells.
	Width int `json:"width,omitzero"`
}

// ProgramStatus is one OSC 7501 record of a [Screen]. A nil Progress means
// the program reported no percentage.
type ProgramStatus struct {
	ID       string `json:"id,omitempty"`
	State    string `json:"state"`
	Kind     string `json:"kind,omitempty"`
	App      string `json:"app,omitempty"`
	Title    string `json:"title,omitempty"`
	Message  string `json:"message,omitempty"`
	Progress *int   `json:"progress,omitempty"`
}

// NewScreen returns a Screen of grid's cells alone, such as for a grid built
// from text. A nil grid has no cells.
func NewScreen(grid *vt.Buffer) *Screen {
	s := &Screen{}
	if grid == nil {
		return s
	}
	bounds := grid.Bounds()
	s.Rows, s.Cols = bounds.Dy(), bounds.Dx()
	s.Cells = make([][]*ScreenCell, s.Rows)
	for y := range s.Rows {
		s.Cells[y] = make([]*ScreenCell, s.Cols)
		for x := range s.Cols {
			c := grid.CellAt(bounds.Min.X+x, bounds.Min.Y+y)
			if c == nil || c.Content == "" && c.Width == 0 {
				continue
			}
			s.Cells[y][x] = &ScreenCell{
				Content: c.Content,
				Style: CellStyle{
					Fg:             ScreenColor{c.Style.Fg},
					Bg:             ScreenColor{c.Style.Bg},
					UnderlineColor: ScreenColor{c.Style.UnderlineColor},
					Underline:      c.Style.Underline,
					Attrs:          c.Style.Attrs,
				},
				Link:  CellLink{URL: c.Link.URL, Params: c.Link.Params},
				Width: c.Width,
			}
		}
	}
	return s
}

// screenOf returns term's screen, with focused.
func screenOf(term *vt.Terminal, focused bool) *Screen {
	s := NewScreen(term.Screen())
	s.Cols, s.Rows = term.Size()
	cur := term.Cursor()
	s.Cursor = ScreenCursor{
		Position: ScreenPosition{X: cur.X, Y: cur.Y},
		Visible:  cur.Visible, Blink: cur.Blink, Style: cur.Shape,
	}
	if c, ok := term.CursorColor(); ok {
		s.Cursor.Color = ScreenColor{c}
	}
	s.Title, s.AltScreen, s.Focused = term.Title(), term.AltScreen(), focused
	s.FgColor, s.BgColor = ScreenColor{term.Foreground()}, ScreenColor{term.Background()}
	s.ReverseColors = term.ReverseColors()
	for _, st := range term.ProgramStatuses() {
		ps := ProgramStatus{
			ID: st.ID, State: st.State.String(), Kind: st.Kind.String(),
			App: st.App, Title: st.Title, Message: st.Message,
		}
		if st.Progress >= 0 {
			ps.Progress = new(st.Progress)
		}
		s.ProgramStatus = append(s.ProgramStatus, ps)
	}
	return s
}

// Screen returns the terminal's screen as it is now.
func (s *Session) Screen() *Screen {
	s.mu.Lock()
	defer s.mu.Unlock()
	return screenOf(s.term, s.Focused())
}
