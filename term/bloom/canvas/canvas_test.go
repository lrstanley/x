// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package canvas

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

const method = ansi.GraphemeWidth

func TestBlit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		dst  string
		src  string
		rect image.Rectangle
		clip image.Rectangle
		want string
	}{
		{"copy", "......", "ab", image.Rect(1, 0, 3, 1), image.Rect(0, 0, 6, 1), ".ab..."},
		{"blanks the rest of rect", "......", "ab", image.Rect(1, 0, 5, 1), image.Rect(0, 0, 6, 1), ".ab  ."},
		{"clip", "......", "abcd", image.Rect(0, 0, 4, 1), image.Rect(1, 0, 3, 1), ".bc..."},
		{"clip past the source", "......\n......", "a", image.Rect(0, 0, 4, 2), image.Rect(1, 0, 4, 2), ".   ..\n.   .."},
		{"wide glyph cut on the left", "......", "世x", image.Rect(0, 0, 3, 1), image.Rect(1, 0, 6, 1), ". x..."},
		{"wide glyph cut on the right", "......", "x世", image.Rect(0, 0, 3, 1), image.Rect(0, 0, 2, 1), "x ...."},
		{"over the tail of a wide glyph", "世界..", "x", image.Rect(1, 0, 2, 1), image.Rect(0, 0, 6, 1), " x界.."},
		{"over the lead of a wide glyph", "a世..", "x", image.Rect(1, 0, 2, 1), image.Rect(0, 0, 5, 1), "ax .."},
	}

	t.Run("blanks keep the background", func(t *testing.T) {
		t.Parallel()
		dst := Parse("\x1b[44m....\x1b[m", method)
		dst.Blit(Parse("a b", method), image.Rect(0, 0, 4, 1), dst.Bounds())
		for x, want := range []string{"a", " ", "b", " "} {
			if c := dst.CellAt(x, 0); c.Content != want || c.Style.Bg == nil {
				t.Fatalf("cell %d = %q on %v, want %q on the old background", x, c.Content, c.Style.Bg, want)
			}
		}
		dst.ClearArea(image.Rect(0, 0, 2, 1))
		if c := dst.CellAt(1, 0); c.Style.Bg != nil {
			t.Fatalf("ClearArea kept the background %v", c.Style.Bg)
		}
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dst := Parse(tt.dst, method)
			dst.Blit(Parse(tt.src, method), tt.rect, tt.clip)
			if got := dst.String(); got != strings.TrimRight(tt.want, " ") {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
			assertWhole(t, dst)
		})
	}
}

// assertWhole fails if c holds part of a wide glyph.
func assertWhole(t *testing.T, c *Canvas) {
	t.Helper()
	for y, l := range c.lines {
		for x := 0; x < len(l); {
			w := l[x].Width
			if w == 0 {
				t.Fatalf("row %d: tail of a wide glyph without its lead at %d", y, x)
			}
			for i := 1; i < w; i++ {
				if x+i >= len(l) || l[x+i].Width != 0 {
					t.Fatalf("row %d: wide glyph at %d is missing its tail", y, x)
				}
			}
			x += w
		}
	}
}

func TestComposite(t *testing.T) {
	t.Parallel()

	dst := Parse("......\n......", method)
	src := Parse("ab世\ncdef", method)
	s := src.Layer(image.Rect(1, 0, 5, 2), dst.Bounds(), dst, DefaultEnv, &Mixer{})
	s.Hide(1, 0) // b
	s.Hide(2, 1) // e
	s.Hide(2, 0) // the lead of 世, which hides the whole glyph
	dst.Composite(s, dst.Bounds())

	if got, want := dst.String(), ".a....\n.cd.f."; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	assertWhole(t, dst)

	// An offset moves the cells; those moved off the canvas are dropped.
	s.Offset = image.Pt(-1, 1)
	moved := Parse("......\n......", method)
	moved.Composite(s, moved.Bounds())
	if got, want := moved.String(), "......\na....."; got != want {
		t.Fatalf("with offset got %q, want %q", got, want)
	}
}

func TestScreenClips(t *testing.T) {
	t.Parallel()

	dst := Parse("......", method)
	scr := dst.Screen(image.Pt(1, 0), image.Pt(4, 1), image.Rect(1, 0, 4, 1))
	for x, r := range "abcde" {
		cell := uv.NewCell(method, string(r))
		scr.SetCell(x, 0, cell)
	}
	scr.SetCell(2, 0, uv.NewCell(method, "世")) // crosses the clip: blank instead
	if got, want := dst.String(), ".ab .."; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	assertWhole(t, dst)
}

func TestRenderMatchesUV(t *testing.T) {
	t.Parallel()

	s := "\x1b[1;31mred\x1b[m plain \x1b[44m  bg  \x1b[m \x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\ 世界\n" +
		"  \x1b[3mitalic\x1b[m   trailing   \n\n\x1b[38;2;10;20;30mtrue color\x1b[m"
	c := Parse(s, method)
	if got, want := c.Render(), uv.Lines(c.lines).Render(); got != want {
		t.Fatalf("Render differs from uv:\ngot  %q\nwant %q", got, want)
	}
}

func TestMeasure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		s    string
		w, h int
	}{
		{"", 0, 0},
		{"abc", 3, 1},
		{"a\nbcd\n", 3, 3},
		{"\x1b[1mbold\x1b[m 世界", 9, 1},
	}
	for _, tt := range tests {
		if w, h := Measure(tt.s, method); w != tt.w || h != tt.h {
			t.Errorf("Measure(%q) = %d, %d, want %d, %d", tt.s, w, h, tt.w, tt.h)
		}
	}
}

func TestCache(t *testing.T) {
	t.Parallel()

	c := NewCache(method)
	a := c.Get("hello")
	c.EndFrame(100)
	if c.Get("hello") != a {
		t.Fatal("a string used again was parsed again")
	}
	if w, h := c.Size("hello"); w != 5 || h != 1 {
		t.Fatalf("Size = %d, %d", w, h)
	}
	for range 2*cacheIdleFrames + 1 { // idle entries go at the next sweep
		c.EndFrame(100)
	}
	if _, ok := c.entries["hello"]; ok {
		t.Fatal("an idle entry was kept")
	}

	// A large view changing every frame must not pile up.
	big := strings.Repeat(strings.Repeat("x", 200)+"\n", 60)
	for i := range 20 {
		c.Get(fmt.Sprintf("%d%s", i, big))
		c.EndFrame(200 * 60)
	}
	if c.cells > 4*200*60+1<<16+200*61 {
		t.Fatalf("cache holds %d cells", c.cells)
	}
}

func TestMixer(t *testing.T) {
	t.Parallel()

	var m Mixer
	black, white := color.RGBA{A: 0xff}, color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	if got := toRGBA(m.Mix(black, white, 0.5)); got.R < 126 || got.R > 129 || got.R != got.G {
		t.Fatalf("Mix half way = %v", got)
	}
	if m.Mix(black, white, 0) != color.Color(black) || m.Mix(black, white, 1) != color.Color(white) {
		t.Fatal("Mix at the ends must return the ends")
	}
	red := color.RGBA{R: 0xff, A: 0xff}
	if got := toRGBA(m.Gray(red, 1)); got.R != got.G || got.G != got.B {
		t.Fatalf("Gray = %v", got)
	}
	if m.Mix(black, white, 0.25) != m.Mix(black, white, 0.25) {
		t.Fatal("Mix is not remembered")
	}
}

// benchFrame is a 200×60 frame of styled text.
func benchFrame() string {
	var b strings.Builder
	for y := range 60 {
		for x := range 10 {
			fmt.Fprintf(&b, "\x1b[38;5;%dmlorem ipsum dolor \x1b[m", 16+(x+y)%200)
		}
		if y < 59 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func BenchmarkParse(b *testing.B) {
	s := benchFrame()
	b.ReportAllocs()
	for b.Loop() {
		Parse(s, method)
	}
}

func BenchmarkBlit(b *testing.B) {
	src := Parse(benchFrame(), method)
	dst := New(200, 60, method)
	b.ReportAllocs()
	for b.Loop() {
		dst.Blit(src, dst.Bounds(), dst.Bounds())
	}
}

func BenchmarkRender(b *testing.B) {
	c := Parse(benchFrame(), method)
	b.ReportAllocs()
	for b.Loop() {
		c.Render()
	}
}
