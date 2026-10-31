// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/reel/vt"
)

// inspector holds a loaded timeline and a player seeking through it.
type inspector struct {
	tl     *reel.Timeline
	events []reel.Event
	player *reel.Player
	// stream concatenates output so sequences split across events stay whole.
	// starts maps each output event to its stream offset; tokens indexes its tokens.
	tokens map[int][]token
	stream []byte
	starts map[int]int
	// palette is the renderer's palette, which draws colors the program did
	// not set itself.
	palette render.Palette
	scale   float64
	// checkpoints is how many terminals the player keeps parked through the
	// cast, so seeking back stays quick; none for a cast quick to replay.
	// replayed and replayTime are the events forward seeks applied, and how
	// long they took, which it is planned from.
	checkpoints int
	replayed    int
	replayTime  time.Duration
	display     display
	// displayFor is display's defaults, and why each part is on.
	displayFor    display
	displayReason map[string]string
}

// load reads a cast to explore, drawing its frames at scale, such as the
// page's device pixel ratio.
func load(ctx context.Context, data []byte, scale float64) (*inspector, error) {
	opts := []render.Option{render.WithScale(scale)}
	tl, err := reel.ReadTimeline(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	h := tl.Header()
	indexed := map[int]color.Color{}
	for i, c := range h.Palette {
		if c != nil {
			indexed[i] = c
		}
	}
	pal, err := render.PaletteOf(append([]render.Option{render.WithPalette(render.Palette{Indexed: indexed})}, opts...)...)
	if err != nil {
		return nil, err
	}
	player, err := reel.NewPlayer(ctx, tl, opts...)
	if err != nil {
		return nil, err
	}
	x := &inspector{tl: tl, events: slices.Collect(tl.Events()), player: player, palette: pal, scale: scale}
	x.tokenize()
	x.displayFor, x.displayReason = x.detectDisplay()
	if err = x.setDisplay(x.displayFor); err != nil {
		_ = player.Close()
		return nil, err
	}
	return x, nil
}

type headerJSON struct {
	Cols         int        `json:"cols"`
	Rows         int        `json:"rows"`
	Title        string     `json:"title,omitempty"`
	SkipPristine bool       `json:"skip_pristine,omitempty"`
	Scrollback   int        `json:"scrollback"`
	CellWidth    int        `json:"cell_width,omitempty"`
	CellHeight   int        `json:"cell_height,omitempty"`
	KittyStorage uint64     `json:"kitty_storage,omitempty"`
	Fg           string     `json:"fg,omitempty"`
	Bg           string     `json:"bg,omitempty"`
	Cursor       string     `json:"cursor,omitempty"`
	Palette      [16]string `json:"palette"`
}

type summaryJSON struct {
	Header   headerJSON  `json:"header"`
	Duration float64     `json:"duration"`
	Events   []eventJSON `json:"events"`
	// DisplayReason explains why each default display part is enabled.
	Display       display           `json:"display"`
	DisplayReason map[string]string `json:"display_reason"`
}

type eventJSON struct {
	I     int     `json:"i"`
	At    float64 `json:"at"`
	Type  string  `json:"type"`
	Label string  `json:"label,omitempty"`
	Size  int     `json:"size,omitempty"`
	// Kind is an annotation's kind, and Data its fields.
	Kind string          `json:"kind,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
	// Tags counts an output event's tokens by lane.
	Tags map[string]int `json:"tags,omitempty"`
	// Category is a mark's label category, such as "wait" for "wait:1".
	Category string `json:"category,omitempty"`
	// Source is where in Go source a mark or annotation was made, when the
	// recording has sources.
	Source *reel.Source `json:"source,omitempty"`
}

func (x *inspector) summary() summaryJSON {
	h := x.tl.Header()
	s := summaryJSON{
		Header: headerJSON{
			Cols: h.Cols, Rows: h.Rows, Title: h.Title, SkipPristine: h.SkipPristine,
			Scrollback: h.Scrollback, CellWidth: h.CellWidth, CellHeight: h.CellHeight,
			KittyStorage: h.KittyStorage,
			Fg:           hex(h.Theme.Fg), Bg: hex(h.Theme.Bg), Cursor: hex(h.Theme.Cursor),
		},
		Duration:      ms(x.tl.Duration()),
		Events:        make([]eventJSON, len(x.events)),
		Display:       x.displayFor,
		DisplayReason: x.displayReason,
	}
	for i := range 16 {
		s.Header.Palette[i] = hex(render.ResolveColor(vt.State{}, x.palette, vt.Indexed(i), false))
	}
	for i, ev := range x.events {
		e := eventJSON{I: i, At: ms(ev.At())}
		switch ev := ev.(type) {
		case reel.Output:
			e.Type, e.Size = "output", len(ev.Data)
			e.Tags = map[string]int{}
			var text strings.Builder
			for _, tok := range x.tokens[i] {
				if tok.Group == groupText {
					if text.Len() < 120 {
						text.WriteString(tok.Raw)
					}
					continue
				}
				e.Tags[tok.Tag]++
			}
			e.Label = cut(text.String(), 120)
		case reel.Input:
			e.Type, e.Size, e.Label = "input", len(ev.Data), strokes(ev.Data)
		case reel.Resize:
			e.Type, e.Label = "resize", fmt.Sprintf("%dx%d", ev.Cols, ev.Rows)
		case reel.Mark:
			e.Type, e.Label, e.Source = "mark", ev.Label, ev.Source
			e.Category, _ = reel.SplitLabel(ev.Label)
		case reel.Focus:
			e.Type, e.Label = "focus", map[bool]string{true: "focused", false: "blurred"}[ev.On]
		case reel.Scroll:
			e.Type, e.Label = "scroll", fmt.Sprintf("offset %d", ev.Offset)
		case reel.ScrollbackSize:
			e.Type, e.Label = "scrollback", fmt.Sprintf("%d lines", ev.Lines)
		case reel.Theme:
			e.Type, e.Label = "theme", fmt.Sprintf("fg %s bg %s cursor %s", hex(ev.Fg), hex(ev.Bg), hex(ev.Cursor))
		case reel.Exit:
			e.Type, e.Label = "exit", fmt.Sprintf("status %d", ev.Status)
		case reel.Annotation:
			e.Type, e.Kind, e.Data, e.Source = "annotation", ev.Kind, ev.Data, ev.Source
			e.Label = annotationLabel(ev)
		default:
			e.Type = fmt.Sprintf("%T", ev)
		}
		s.Events[i] = e
	}
	return s
}

// annotationKindKey is the field an annotation's kind is stored under.
const annotationKindKey = "kind"

// annotationLabel picks the fields of an annotation that say the most about
// it in one line.
func annotationLabel(a reel.Annotation) string {
	var m map[string]any
	if a.Decode(&m) != nil {
		return ""
	}
	var parts []string
	for _, k := range []string{"query", "param", "effect", "type", "status", "outcome", "message", "key", "text", "label", "name", "test"} {
		if v, ok := m[k]; ok {
			parts = append(parts, fmt.Sprint(v))
		}
	}
	if len(parts) == 0 {
		keys := slices.Sorted(func(yield func(string) bool) {
			for k := range m {
				if k != annotationKindKey && !yield(k) {
					return
				}
			}
		})
		return strings.Join(keys, " ")
	}
	return cut(strings.Join(parts, " · "), 120)
}

func strokes(data []byte) string {
	var parts []string
	for _, s := range vt.DecodeInput(data) {
		switch s.Kind {
		case vt.StrokeKey:
			parts = append(parts, "<"+s.Key.String()+">")
		case vt.StrokeText:
			parts = append(parts, strconv.Quote(s.Text))
		case vt.StrokePaste:
			parts = append(parts, fmt.Sprintf("paste(%d bytes)", len(s.Text)))
		case vt.StrokeMouse:
			parts = append(parts, fmt.Sprintf("mouse %+v", s.Mouse))
		}
	}
	if len(parts) == 0 {
		return escape(data)
	}
	return cut(strings.Join(parts, " "), 120)
}

type detailJSON struct {
	I      int     `json:"i"`
	Raw    string  `json:"raw,omitempty"`
	Tokens []token `json:"tokens,omitempty"`
}

// detail returns what an event holds in full: its bytes and tokens.
func (x *inspector) detail(i int) (detailJSON, error) {
	if i < 0 || i >= len(x.events) {
		return detailJSON{}, fmt.Errorf("no event %d", i)
	}
	d := detailJSON{I: i}
	switch ev := x.events[i].(type) {
	case reel.Output:
		d.Raw, d.Tokens = escapeAll(ev.Data), x.tokenFields(i)
	case reel.Input:
		d.Raw = escapeAll(ev.Data)
	}
	return d, nil
}

// tokenFields returns event i's tokens with their fields, decoding each
// sequence again from the stream.
func (x *inspector) tokenFields(i int) []token {
	toks := slices.Clone(x.tokens[i])
	p := ansi.NewParser()
	p.SetDataSize(64 << 10)
	for k, tok := range toks {
		if tok.Group == groupText {
			continue
		}
		at := x.starts[i] + tok.Offset
		seq, _, _, _ := ansi.DecodeSequence(x.stream[at:at+tok.Len], 0, p)
		toks[k].Fields = fields(seq, p)
	}
	return toks
}

// escapeAll escapes b in full, with a far larger cap than tokens get.
func escapeAll(b []byte) string {
	if len(b) > 256<<10 {
		return escape(b[:256<<10]) + fmt.Sprintf("…(+%d bytes)", len(b)-256<<10)
	}
	var sb strings.Builder
	for len(b) > 0 {
		n := min(len(b), maxRaw/2)
		for n < len(b) && n > 0 && b[n]&0xc0 == 0x80 {
			n--
		}
		sb.WriteString(escape(b[:n]))
		b = b[n:]
	}
	return sb.String()
}

type stateJSON struct {
	At               float64              `json:"at"`
	Applied          int                  `json:"applied"`
	Title            string               `json:"title,omitempty"`
	WorkingDirectory string               `json:"working_directory,omitempty"`
	Focused          bool                 `json:"focused"`
	AltScreen        bool                 `json:"alt_screen"`
	ReverseColors    bool                 `json:"reverse_colors"`
	Cursor           vt.Cursor            `json:"cursor"`
	Fg               string               `json:"fg"`
	Bg               string               `json:"bg"`
	CursorColor      string               `json:"cursor_color,omitempty"`
	ScrollbackLen    int                  `json:"scrollback_len"`
	ScrollbackOffset int                  `json:"scrollback_offset"`
	Progress         string               `json:"progress,omitempty"`
	Bell             uint64               `json:"bells"`
	Notifications    []vt.Notification    `json:"notifications,omitempty"`
	Clipboard        string               `json:"clipboard,omitempty"`
	Prompt           string               `json:"prompt,omitempty"`
	PromptMarks      []vt.PromptMark      `json:"prompt_marks,omitempty"`
	ProgramStatus    []reel.ProgramStatus `json:"program_status,omitempty"`
	PaletteSet       map[int]string       `json:"palette_set,omitempty"`
	Generation       uint64               `json:"generation"`
}

// layoutJSON is where the frame's image places the cell grid, in pixels.
type layoutJSON struct {
	X     int `json:"x"`
	Y     int `json:"y"`
	CellW int `json:"cell_w"`
	CellH int `json:"cell_h"`
}

type frameJSON struct {
	Layout layoutJSON `json:"layout"`
	State  stateJSON  `json:"state"`
	Cols   int        `json:"cols"`
	Rows   int        `json:"rows"`
	// Screen is the screen's cells, only when asked for: they are most of a
	// frame's size, and only needed to inspect cells.
	Screen *reel.Screen `json:"screen,omitempty"`
	Colors []colorUse   `json:"colors"`
}

// seek moves the player to just after event i, or before every event when
// i is negative.
func (x *inspector) seek(i int) error {
	from, start := x.player.Pos(), time.Now()
	if err := x.player.Seek(i + 1); err != nil {
		return err
	}
	// Time seeks forward, which apply events from where the last one left.
	if n := x.player.Pos() - from; n > 0 {
		x.replayed += n
		x.replayTime += time.Since(start)
	}
	return nil
}

// frame describes the screen after the applied events, with its cells when
// cells is set.
func (x *inspector) frame(cells bool) frameJSON {
	f := x.player.Frame()
	st := f.State
	s := stateJSON{
		At: ms(f.At), Applied: x.player.Pos(),
		Title: st.Title, WorkingDirectory: st.WorkingDirectory, Focused: st.Focused,
		AltScreen: st.AltScreen, ReverseColors: st.ReverseColors, Cursor: st.Cursor,
		Fg:            hex(render.ResolveColor(st, x.palette, nil, false)),
		Bg:            hex(render.ResolveColor(st, x.palette, nil, true)),
		ScrollbackLen: st.ScrollbackLen, ScrollbackOffset: st.ScrollbackOffset,
		Bell: st.Bell.Count, Notifications: st.Notifications, Clipboard: st.Clipboard.Text,
		PromptMarks: st.PromptMarks, Generation: st.Generation,
	}
	if st.CursorColorSet {
		s.CursorColor = hex(st.CursorColor)
	}
	if p := st.ShownProgress(); p.Reported {
		s.Progress = fmt.Sprintf("%v %d%%", p.State, p.Percent)
	}
	if st.Prompt != (vt.Prompt{}) {
		s.Prompt = fmt.Sprintf("%+v", st.Prompt)
	}
	for i := range 256 {
		if c, ok := st.Palette.Color(uint8(i)); ok {
			if s.PaletteSet == nil {
				s.PaletteSet = map[int]string{}
			}
			s.PaletteSet[i] = hex(c)
		}
	}
	for _, ps := range st.ProgramStatuses {
		p := reel.ProgramStatus{
			ID: ps.ID, State: ps.State.String(), Kind: ps.Kind.String(),
			App: ps.App, Title: ps.Title, Message: ps.Message,
		}
		if ps.Progress >= 0 {
			p.Progress = new(ps.Progress)
		}
		s.ProgramStatus = append(s.ProgramStatus, p)
	}
	out := frameJSON{State: s, Colors: x.colors(f.Screen, st)}
	out.Cols, out.Rows = f.Screen.Width(), f.Screen.Height()
	if cells {
		out.Screen = x.player.Screen()
	}
	if l, err := x.player.Layout(); err == nil {
		out.Layout = layoutJSON{X: l.Grid.Min.X, Y: l.Grid.Min.Y, CellW: l.Cell.X, CellH: l.Cell.Y}
	}
	return out
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xc0 == 0x80 {
		n--
	}
	return s[:n] + "…"
}

// Seeking back replays from the nearest checkpoint, so one is kept for
// about every checkpointSpan of replay, up to maxCheckpoints of them, each
// holding a terminal's memory.
const (
	checkpointSpan = 50 * time.Millisecond
	maxCheckpoints = 8
)

// minTimed is how many events seeks forward must have applied before their
// pace is trusted to plan checkpoints.
const minTimed = 32

// planCheckpoints returns how many checkpoints keep seeking back from
// anywhere to about checkpointSpan, from the pace seeks forward have
// replayed events at. It only grows, so the checkpoints built stay.
func (x *inspector) planCheckpoints() int {
	if x.replayed < minTimed {
		return x.checkpoints
	}
	whole := x.replayTime * time.Duration(x.player.Len()) / time.Duration(x.replayed)
	return max(x.checkpoints, min(maxCheckpoints, int(whole/checkpointSpan)))
}

// prepare builds checkpoints for up to budget, and reports whether there is
// more to do.
func (x *inspector) prepare(budget time.Duration) (bool, error) {
	x.checkpoints = x.planCheckpoints()
	return x.player.Prepare(x.checkpoints, budget)
}
