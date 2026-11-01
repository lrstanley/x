// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package bloom

import (
	"cmp"
	"image/color"
	"slices"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Root renders a layout as a model's [tea.View], keeping what it can between
// frames. Keep one in the root model, by pointer, and pass it the model's
// layout from View. Pass every message to [Root.Update] and return its command.
type Root struct {
	r        *renderer
	interval time.Duration
	pending  bool
}

// Option configures a [Root].
type Option func(*config)

type config struct {
	method ansi.Method
	fg, bg color.Color
	fps    int
	now    func() time.Time
}

// WithWidthMethod sets how the width of graphemes is measured. It must match
// the terminal's, which Bubble Tea measures with [ansi.GraphemeWidth] when
// the terminal supports it. The default is [ansi.GraphemeWidth], as Lip
// Gloss uses.
func WithWidthMethod(method ansi.Method) Option {
	return func(c *config) { c.method = method }
}

// WithColors sets the terminal's default colors, for effects that blend
// colors, until the terminal reports its own (see [Root.Init]).
func WithColors(fg, bg color.Color) Option {
	return func(c *config) { c.fg, c.bg = fg, bg }
}

// WithFPS sets the layout animation tick rate, defaulting to 60.
// Values below one use one frame per second.
func WithFPS(n int) Option { return func(c *config) { c.fps = max(n, 1) } }

// WithClock sets the layout animation clock, for tests. It must not be nil.
func WithClock(now func() time.Time) Option { return func(c *config) { c.now = now } }

// New returns a Root.
func New(opts ...Option) *Root {
	c := config{method: ansi.GraphemeWidth, fps: 60, now: time.Now}
	for _, opt := range opts {
		opt(&c)
	}
	r := &Root{r: newRenderer(c.method), interval: time.Second / time.Duration(c.fps)}
	r.r.motion = &layoutMotion{now: c.now}
	r.r.env.Fg = cmp.Or(c.fg, r.r.env.Fg)
	r.r.env.Bg = cmp.Or(c.bg, r.r.env.Bg)
	return r
}

// Init asks the terminal for its default colors, which effects that blend
// colors use. Return it from the model's Init. Pass every message to
// [Root.Update] and return its command from the model's Update.
func (r *Root) Init() tea.Cmd {
	return tea.Batch(tea.RequestBackgroundColor, tea.RequestForegroundColor)
}

// Update notes what the terminal reports about itself: its default colors
// and color profile. Pass every message to Update and return its command:
// it schedules frames while layout animations run.
func (r *Root) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		r.r.env.Bg = msg.Color
	case tea.ForegroundColorMsg:
		r.r.env.Fg = msg.Color
	case tea.ColorProfileMsg:
		r.r.env.Profile = msg.Profile
	}
	if tick, ok := msg.(layoutFrameMsg); ok {
		if tick.root != r {
			return nil
		}
		r.pending = false
		if !r.r.motion.running {
			return nil
		}
	}
	if r.pending || len(r.r.motion.states) == 0 {
		return nil
	}
	r.pending = true
	return tea.Tick(r.interval, func(time.Time) tea.Msg { return layoutFrameMsg{root: r} })
}

// Render lays out and draws c in a w×h frame and returns it as a string. A
// height of zero or less is c's natural height, for programs that do not use
// the whole screen. Nothing is drawn while the width is zero or less, as it is
// before Bubble Tea reports the window's size.
func (r *Root) Render(w, h int, c Child) string {
	if w <= 0 {
		c = nil
	}
	out := r.r.render(c, w, h)
	r.r.updateFrame()
	return out
}

// View lays out and draws c in a w×h frame, and returns a view showing it,
// with an OnMouse handler routing mouse events as [MouseMsg]s and the cursor
// of the [TextNode] that placed one.
func (r *Root) View(w, h int, c Child) tea.View {
	v := tea.NewView(r.Render(w, h, c))
	f := r.r.frame
	v.OnMouse = f.onMouse
	v.Cursor = f.cursor
	return v
}

// Frame returns the geometry of the last frame rendered, for moving focus
// with [Focus] and for finding where nodes are on screen.
func (r *Root) Frame() *Frame {
	return r.r.frame
}

// updateFrame makes the Frame for the frame just drawn, keeping the last one
// when nothing about it changed.
func (r *renderer) updateFrame() {
	if f := r.frame; f != nil && slices.Equal(f.entries, r.entries) && sameCursor(f.cursor, r.cursor) {
		return
	}
	r.frame = &Frame{entries: slices.Clone(r.entries), cursor: r.cursor}
}

func sameCursor(a, b *tea.Cursor) bool {
	return a == b || (a != nil && b != nil && *a == *b)
}

var renderers = sync.Pool{New: func() any { return newRenderer(ansi.GraphemeWidth) }}

// Render lays out and draws c in a w×h frame and returns it as a string,
// for using a layout where a string is wanted. A w or h of zero or less is
// c's natural size on that axis. A model's View should use [Root] instead,
// which keeps more between frames. Render is stateless and draws animation
// targets immediately.
func Render(c Child, w, h int) string {
	r, _ := renderers.Get().(*renderer)
	defer renderers.Put(r)
	return r.render(c, w, h)
}

// Solve lays out c in a w×h frame without drawing it, and returns its
// geometry at animation targets. Solve is stateless; retained layout
// animations may draw intermediate rectangles. Components that need their
// target size in Update can use Solve there. Effects are not run, so a node a
// transition is moving, such as with [fx.Slide], is where it was laid out:
//
//	case tea.WindowSizeMsg:
//		m.width, m.height = msg.Width, msg.Height
//		if r, ok := bloom.Solve(m.layout(), m.width, m.height).Rect(paneReader); ok {
//			m.reader.SetWidth(r.Dx())
//			m.reader.SetHeight(r.Dy())
//		}
func Solve(c Child, w, h int) *Frame {
	r, _ := renderers.Get().(*renderer)
	defer renderers.Put(r)
	r.parse = false
	defer func() { r.parse = true }()
	r.paintTree(c, w, h)
	r.placeEntries()
	r.cache.EndFrame(w * h)
	return &Frame{entries: slices.Clone(r.entries), cursor: r.cursor}
}
