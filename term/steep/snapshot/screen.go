// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package snapshot

import (
	"bytes"
	"encoding"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"image/color"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/reel/vt"
)

// Position represents a position in the terminal.
type Position struct {
	X int `json:"x" yaml:"x"`
	Y int `json:"y" yaml:"y"`
}

var (
	_ encoding.TextMarshaler   = (*Color)(nil)
	_ encoding.TextUnmarshaler = (*Color)(nil)
)

// Color represents a terminal color, which can be one of the following:
//   - An ANSI 16 color (0-15) of type [ansi.BasicColor].
//   - An ANSI 256 color (0-255) of type [ansi.IndexedColor].
//   - Or any other 24-bit color that implements [color.Color].
type Color struct {
	Color color.Color `json:"color,omitempty" yaml:"color,omitempty"`
}

// MarshalText implements the [encoding.TextMarshaler] interface for Color.
func (c Color) MarshalText() ([]byte, error) {
	switch col := c.Color.(type) {
	case nil:
		return []byte{}, nil
	case ansi.BasicColor:
		return []byte(strconv.Itoa(int(col))), nil
	case ansi.IndexedColor:
		return []byte(strconv.Itoa(int(col))), nil
	default:
		r, g, b, _ := c.Color.RGBA()
		return fmt.Appendf(nil, "#%02x%02x%02x", r>>8, g>>8, b>>8), nil
	}
}

// UnmarshalText implements the [encoding.TextUnmarshaler] interface for Color.
func (c *Color) UnmarshalText(text []byte) error {
	s := string(text)
	if s == "" {
		return nil
	}
	if i, err := strconv.Atoi(s); err == nil {
		if i >= 0 && i <= 15 {
			c.Color = ansi.BasicColor(i)
			return nil
		} else if i >= 16 && i <= 255 {
			c.Color = ansi.IndexedColor(i)
			return nil
		}
	}

	col := ansi.XParseColor(s)
	if col == nil {
		return fmt.Errorf("invalid color: %s", s)
	}
	c.Color = col
	return nil
}

// Cursor represents the cursor state.
type Cursor struct {
	Position Position       `json:"position"       yaml:"position"`
	Visible  bool           `json:"visible"        yaml:"visible"`
	Color    Color          `json:"color,omitzero" yaml:"color,omitzero"`
	Style    vt.CursorShape `json:"style"          yaml:"style"`
	Blink    bool           `json:"blink"          yaml:"blink"`
}

// Style represents the Style of a cell.
type Style struct {
	Fg             Color        `json:"fg,omitzero"              yaml:"fg,omitzero"`
	Bg             Color        `json:"bg,omitzero"              yaml:"bg,omitzero"`
	UnderlineColor Color        `json:"underline_color,omitzero" yaml:"underline_color,omitzero"`
	Underline      vt.Underline `json:"underline,omitempty"      yaml:"underline,omitempty"`
	Attrs          vt.Attr      `json:"attrs,omitempty"          yaml:"attrs,omitempty"`
}

// Link represents a hyperlink in the terminal screen.
type Link struct {
	URL    string `json:"url,omitempty" yaml:"url,omitempty"`
	Params string `json:"params,omitempty" yaml:"params,omitempty"`
}

// Cell represents a single cell in the terminal screen.
type Cell struct {
	// Content is the [Cell]'s content, which consists of a single grapheme
	// cluster. Most of the time, this will be a single rune as well, but it
	// can also be a combination of runes that form a grapheme cluster.
	Content string `json:"content,omitempty" yaml:"content,omitempty"`

	// The style of the cell. Nil style means no style. Zero value prints a
	// reset sequence.
	Style Style `json:"style,omitzero" yaml:"style,omitzero"`

	// Link is the hyperlink of the cell.
	Link Link `json:"link,omitzero" yaml:"link,omitzero"`

	// Width is the mono-spaced width of the grapheme cluster.
	Width int `json:"width,omitzero" yaml:"width,omitzero"`
}

// FromCell copies a grid cell into c.
func (c *Cell) FromCell(cell *vt.Cell) *Cell {
	if cell == nil {
		return nil
	}
	c.Content = cell.Content
	c.Style = Style{
		Fg:             Color{Color: cell.Style.Fg},
		Bg:             Color{Color: cell.Style.Bg},
		Underline:      cell.Style.Underline,
		UnderlineColor: Color{Color: cell.Style.UnderlineColor},
		Attrs:          cell.Style.Attrs,
	}
	c.Link = Link{
		URL:    cell.Link.URL,
		Params: cell.Link.Params,
	}
	c.Width = cell.Width
	return c
}

// ScreenSnapshot represents a snapshot of the terminal state at a given moment.
type ScreenSnapshot struct {
	// TODO: how to handle modes? not JSON marshalable atm.
	// Modes     ansi.Modes `json:"modes,omitempty"      yaml:"modes,omitempty"`
	Title         string          `json:"title,omitempty"      yaml:"title,omitempty"`
	Rows          int             `json:"rows"                 yaml:"rows"`
	Cols          int             `json:"cols"                 yaml:"cols"`
	AltScreen     bool            `json:"alt_screen,omitempty" yaml:"alt_screen,omitempty"`
	Focused       bool            `json:"focused,omitempty"    yaml:"focused,omitempty"`
	Cursor        Cursor          `json:"cursor,omitempty"         yaml:"cursor"`
	FgColor       Color           `json:"fg_color,omitzero"        yaml:"fg_color,omitzero"`
	BgColor       Color           `json:"bg_color,omitzero"        yaml:"bg_color,omitzero"`
	ProgramStatus []ProgramStatus `json:"program_status,omitempty" yaml:"program_status,omitempty"`
	Cells         [][]*Cell       `json:"cells"                    yaml:"cells"`
}

// ProgramStatus is one OSC 7501 record. A nil [ProgramStatus.Progress] means
// the program did not report a percentage.
type ProgramStatus struct {
	ID       string `json:"id,omitempty"       yaml:"id,omitempty"`
	State    string `json:"state"              yaml:"state"`
	Kind     string `json:"kind,omitempty"     yaml:"kind,omitempty"`
	App      string `json:"app,omitempty"      yaml:"app,omitempty"`
	Title    string `json:"title,omitempty"    yaml:"title,omitempty"`
	Message  string `json:"message,omitempty"  yaml:"message,omitempty"`
	Progress *int   `json:"progress,omitempty" yaml:"progress,omitempty"`
}

// Compare compares the current snapshot with the given snapshot, using JSON
// marshaling and [reflect.DeepEqual].
func (ss *ScreenSnapshot) Compare(tb testing.TB, other *ScreenSnapshot, _ ...Option) bool {
	tb.Helper()

	var ssm, otherm any

	ssb, err := json.Marshal(ss)
	if err != nil {
		tb.Fatalf("failed to marshal snapshot: %v", err)
	}
	err = json.Unmarshal(ssb, &ssm)
	if err != nil {
		tb.Fatalf("failed to unmarshal snapshot: %v", err)
	}
	otherb, err := json.Marshal(other)
	if err != nil {
		tb.Fatalf("failed to marshal other snapshot: %v", err)
	}
	err = json.Unmarshal(otherb, &otherm)
	if err != nil {
		tb.Fatalf("failed to unmarshal other snapshot: %v", err)
	}
	return reflect.DeepEqual(ssm, otherm)
}

// WithScreenBuffer updates the ScreenSnapshot with the contents of the given
// screen buffer (Rows, Cols, Cells).
func (ss *ScreenSnapshot) WithScreenBuffer(tb testing.TB, grid *vt.Buffer, _ ...Option) *ScreenSnapshot {
	tb.Helper()
	if grid == nil {
		ss.Rows, ss.Cols, ss.Cells = 0, 0, nil
		return ss
	}
	bounds := grid.Bounds()
	ss.Rows = bounds.Dy()
	ss.Cols = bounds.Dx()
	ss.Cells = make([][]*Cell, ss.Rows)
	for y := range ss.Rows {
		ss.Cells[y] = make([]*Cell, ss.Cols)
		for x := range ss.Cols {
			cell := grid.CellAt(bounds.Min.X+x, bounds.Min.Y+y)
			if cell == nil || (cell.Content == "" && cell.Width == 0) {
				continue
			}
			ss.Cells[y][x] = (&Cell{}).FromCell(cell)
		}
	}
	return ss
}

// AsScreenBuffer converts plain text into a cell grid. Each rune is one cell.
// The trailing newline does not add an empty row.
func AsScreenBuffer(tb testing.TB, v string, opts ...Option) *vt.Buffer {
	tb.Helper()
	cfg := collectOptions(tb, opts...)
	text := normalize(v, cfg)
	if text == "" {
		return vt.NewBuffer(0, 0)
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	width := 0
	for _, line := range lines {
		if n := utf8.RuneCountInString(line); n > width {
			width = n
		}
	}
	buf := vt.NewBuffer(width, len(lines))
	for y, line := range lines {
		x := 0
		for _, r := range line {
			buf.SetCell(x, y, &vt.Cell{Content: string(r), Width: 1})
			x++
		}
	}
	return buf
}

// writeScreen handles the i/o and encoding of a [ScreenSnapshot] to a JSON file.
//
// It will fail the test if there are i/o errors.
func writeScreen(tb testing.TB, snap *ScreenSnapshot, path string, opts ...Option) {
	tb.Helper()

	cfg := collectOptions(tb, opts...)
	buf := &bytes.Buffer{}
	var encOpts []jsontext.Options
	if cfg.indent {
		encOpts = append(encOpts, jsontext.Multiline(true))
	}
	enc := jsontext.NewEncoder(buf, encOpts...)
	err := json.MarshalEncode(enc, snap)
	if err != nil {
		tb.Fatalf("failed to encode snapshot: %v", err)
	}
	err = os.WriteFile(path, buf.Bytes(), 0o600)
	if err != nil {
		tb.Fatalf("failed to write snapshot: %v", err)
	}
}

// WriteScreen writes the given screen snapshot to the given path, processing it
// in the same way as [AssertScreenEqual] and [RequireScreenEqual]. If path is
// empty, it will be generated using the test name and associated options.
//
// It will fail the test if there are i/o errors.
func WriteScreen(tb testing.TB, screen *ScreenSnapshot, path string, opts ...Option) {
	tb.Helper()
	if path == "" {
		cfg := collectOptions(tb, opts...)
		path = snapshotPath(tb, cfg.suffix, ".json", cfg)
	}
	writeScreen(tb, screen, path, opts...)
}

// AssertScreenEqual compares the provided screen snapshot against the stored JSON
// representation of that snapshot.
func AssertScreenEqual(tb testing.TB, screen *ScreenSnapshot, opts ...Option) bool {
	tb.Helper()

	cfg := collectOptions(tb, opts...)
	path := snapshotPath(tb, cfg.suffix, ".json", cfg)

	if cfg.update {
		writeScreen(tb, screen, path, opts...)
		return true
	}

	expectedb, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			errNotExist(tb, path)
			return false
		}
		tb.Fatalf("failed to read snapshot: %v", err)
	}
	expected := &ScreenSnapshot{}
	err = json.Unmarshal(expectedb, expected)
	if err != nil {
		tb.Fatalf("failed to unmarshal snapshot: %v", err)
	}
	matches := screen.Compare(tb, expected, opts...)
	if !matches {
		tb.Errorf("snapshot %q does not match", path)
		return false
	}
	return true
}

func RequireScreenEqual(tb testing.TB, screen *ScreenSnapshot, opts ...Option) {
	tb.Helper()

	if !AssertScreenEqual(tb, screen, opts...) {
		tb.FailNow()
	}
}
