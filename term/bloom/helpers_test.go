// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package bloom

import (
	"image"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lrstanley/x/term/bloom/anim"
	"github.com/lrstanley/x/term/bloom/fx"
)

func TestTextTruncate(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, text string
		width      int
		style      lipgloss.Style
		want       string
	}{
		{"plain", "abcdef", 4, lipgloss.NewStyle(), "abc…"},
		{"wide", "世界你好", 4, lipgloss.NewStyle(), "世… "},
		{"grapheme", "e\u0301e\u0301e\u0301", 2, lipgloss.NewStyle(), "e\u0301…"},
		{"newlines", "a\r\nb\nc", 5, lipgloss.NewStyle(), "a b c"},
		{"padding", "abcdef", 6, lipgloss.NewStyle().Padding(0, 1), " abc… "},
		{"border", "abcdef", 6, lipgloss.NewStyle().BorderLeft(true).BorderRight(true).BorderStyle(lipgloss.NormalBorder()), "│abc…│"},
		{"style width", "abcdef", 6, lipgloss.NewStyle().Width(3), "ab…   "},
		{"one cell", "世界", 1, lipgloss.NewStyle(), "…"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			n := Text(tt.text).Style(tt.style).Truncate()
			got := ansi.Strip(Render(n, tt.width, 1))
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
			if marquee := Render(Marquee(tt.text, 0).Style(tt.style), tt.width, 1); Render(n, tt.width, 1) != marquee {
				t.Fatalf("differs from marquee: %q", marquee)
			}
		})
	}
	if got := Text("abc\ndef").Truncate().View(); got != "abc def" {
		t.Fatalf("natural size: %q", got)
	}
	if got := ansi.Strip(Render(Column(Text("abcdef").Truncate(), Text("end")), 3, 0)); got != "ab…\nend" {
		t.Fatalf("natural height: %q", got)
	}
}

func expectRect(t *testing.T, f *Frame, id string, want image.Rectangle) {
	t.Helper()
	if got, ok := f.Rect(id); !ok || got != want {
		t.Fatalf("%s rect = %v, %v; want %v", id, got, ok, want)
	}
}

func TestGrid(t *testing.T) {
	t.Parallel()
	children := []Child{Text("a").ID("a"), Text("bb").ID("b"), Text("c").ID("c")}
	n := Grid(2, children...).Gap(1, 1)
	f := Solve(n, 11, 7)
	expectRect(t, f, "a", image.Rect(0, 0, 5, 3))
	expectRect(t, f, "b", image.Rect(6, 0, 11, 3))
	expectRect(t, f, "c", image.Rect(0, 4, 5, 7))
	if got := n.View(); got != "a  bb\n     \nc    " {
		t.Fatalf("natural: %q", got)
	}
	f = Solve(Grid(2, Fixed(2, Text("a")).ID("fixed"), Fill(Text("b")).ID("fill")), 10, 2)
	expectRect(t, f, "fixed", image.Rect(0, 0, 2, 2))
	expectRect(t, f, "fill", image.Rect(5, 0, 10, 2))
	expectRect(t, Solve(Grid(0, Text("a").ID("a")), 4, 2), "a", image.Rect(0, 0, 4, 2))
	if got := Grid(2).View(); got != "" {
		t.Fatalf("empty: %q", got)
	}
	n = Grid(2, Text("a\nb"), Text("c"), Text("d\ne\nf")).Gap(0, 1)
	if got := Render(Column(n, Text("end").ID("end")), 10, 0); len(strings.Split(got, "\n")) != 7 {
		t.Fatalf("natural row heights: %q", got)
	}
}

func TestWrap(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		node  WrapNode
		width int
		want  string
	}{
		{"one line", Wrap(Text("aa"), Text("bbb")), 6, "aabbb"},
		{"wrap", Wrap(Text("aa"), Text("bbb")).Gap(1, 1), 5, "aa \n   \nbbb"},
		{"overwide", Wrap(Text("abcdefgh"), Text("bb")).Gap(1, 0), 4, "abcd\nbb  "},
		{"tall", Wrap(Text("a\nb"), Text("c"), Text("dd")).Gap(1, 1), 3, "a c\nb  \n   \ndd "},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ansi.Strip(Render(tt.node, tt.width, 0)); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
	n := Wrap(Text("aaa").ID("a"), Text("bbb").ID("b")).Gap(1, 1)
	expectRect(t, Solve(Column(Auto(n), Text("end").ID("end")), 5, 10), "end", image.Rect(0, 3, 5, 4))
	if got := n.View(); got != "aaa bbb" {
		t.Fatalf("natural width: %q", got)
	}
	if got := Wrap().View(); got != "" {
		t.Fatalf("empty: %q", got)
	}
}

func TestWrapMeasureCache(t *testing.T) {
	t.Parallel()
	var calls int
	children := make([]Child, 120)
	for i := range children {
		children[i] = meteredLeaf{n: &calls}
	}
	Solve(Column(Auto(Wrap(children...).Gap(1, 1)), Text("footer")), 40, 60)
	if calls != len(children) {
		t.Fatalf("measured %d times for %d children", calls, len(children))
	}
}

func TestGridNaturalRows(t *testing.T) {
	t.Parallel()
	n := Grid(2, Text("a"), Text("b"), Text("c\nd\ne").ID("tall")).Gap(1, 1)
	expectRect(t, Solve(n, 9, 0), "tall", image.Rect(0, 2, 4, 5))
}

func TestPresent(t *testing.T) {
	t.Parallel()
	p := anim.NewPresence()
	for _, vertical := range []bool{false, true} {
		children := []Child{Text("a").ID("a"), Fill(Present(p, Text("hidden").ID("hidden"))).ID("slot"), Text("b").ID("b")}
		n := Row(children...).Gap(2)
		if vertical {
			n = Column(children...).Gap(2)
		}
		f := Solve(n, 10, 10)
		if _, ok := f.Rect("hidden"); ok {
			t.Fatal("hidden presence has an entry")
		}
		if _, ok := f.Rect("slot"); ok {
			t.Fatal("hidden slot has an entry")
		}
		want := image.Rect(3, 0, 4, 10)
		if vertical {
			want = image.Rect(0, 3, 10, 4)
		}
		expectRect(t, f, "b", want)
	}
	if got := Render(Present(p, Text("hidden")), 0, 0); got != "" {
		t.Fatalf("absent root: %q", got)
	}
	p.Show()
	f := Solve(Row(Present(p, Text("shown").ID("shown")), Text("b").ID("b")).Gap(1), 10, 1)
	expectRect(t, f, "b", image.Rect(6, 0, 7, 1))
	expectRect(t, f, "shown", image.Rect(0, 0, 5, 1))
}

func TestLayerPresence(t *testing.T) {
	t.Parallel()
	p := anim.NewPresence()
	r := New()
	tree := func() Child {
		return Overlay(Text("base").ID("base"), Layer(Text("dialog").ID("child")).ID("layer").Modal().Backdrop(fx.Dim{}).Shadow(1, 1, fx.Dim{}).Presence(p, fx.Fade{}))
	}
	base := r.Render(12, 3, Text("base").ID("base"))
	if got := r.Render(12, 3, tree()); got != base {
		t.Fatalf("hidden layer changed rendering: %q", got)
	}
	for _, id := range []string{"child", "layer"} {
		if _, ok := r.Frame().Rect(id); ok {
			t.Fatalf("hidden %s has entry", id)
		}
	}
	if hit, _ := r.Frame().hit(image.Pt(1, 1)); hit < 0 || r.Frame().entries[hit].id != "base" {
		t.Fatal("hidden layer took hit")
	}
	p.Show()
	r.Render(12, 3, tree())
	if _, ok := r.Frame().Rect("layer"); !ok {
		t.Fatal("visible layer missing")
	}
	p.Hide()
	r.Render(12, 3, tree())
	if _, ok := r.Frame().Rect("layer"); !ok {
		t.Fatal("exiting layer omitted before presence finished")
	}
	p = anim.NewPresence()
	r.Render(12, 3, tree())
	if _, ok := r.Frame().Rect("layer"); ok {
		t.Fatal("hidden layer cached as visible")
	}
}

func TestFocusUpdate(t *testing.T) {
	t.Parallel()
	r := New()
	v := r.View(10, 2, Row(Text("a").ID("a").Focusable(), Auto(Text("b").ID("b").Focusable()).ID("scope").Scope()))
	var f Focus
	for _, tt := range []struct {
		msg     tea.Msg
		handled bool
		want    string
	}{
		{tea.KeyPressMsg{Code: tea.KeyTab}, true, "a"},
		{tea.KeyPressMsg{Code: tea.KeyTab}, true, "b"},
		{tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, true, "a"},
		{tea.KeyReleaseMsg{Code: tea.KeyTab}, false, "a"},
		{tea.KeyPressMsg{Code: 'x'}, false, "a"},
		{v.OnMouse(tea.MouseClickMsg{X: 1, Y: 0, Button: tea.MouseLeft})(), true, "b"},
		{v.OnMouse(tea.MouseMotionMsg{X: 0, Y: 0})(), false, "b"},
	} {
		if got := f.Update(tt.msg, r.Frame()); got != tt.handled || !f.Is(tt.want) {
			t.Fatalf("%T: handled %v, focus %v; want %v, %s", tt.msg, got, f.Current(), tt.handled, tt.want)
		}
	}
	f.Push("scope")
	if !f.Update(tea.KeyPressMsg{Code: tea.KeyTab}, r.Frame()) || !f.Within(r.Frame(), "scope") {
		t.Fatal("tab escaped scope")
	}
	if f.Update(v.OnMouse(tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})(), r.Frame()) {
		t.Fatal("click escaped scope")
	}
	if !f.Update(tea.KeyPressMsg{Code: tea.KeyTab}, nil) {
		t.Fatal("tab not handled without frame")
	}
}

func BenchmarkWrapFrame(b *testing.B) {
	children := make([]Child, 120)
	for i := range children {
		children[i] = Text("service running").Style(benchItem)
	}
	tree := Wrap(children...).Gap(1, 1)
	r := New()
	b.ReportAllocs()
	for b.Loop() {
		r.View(benchW, benchH, tree)
	}
}

func BenchmarkGridFrame(b *testing.B) {
	children := make([]Child, 40)
	for i := range children {
		children[i] = Box(benchBorder, Text("service\n100 req/s"))
	}
	tree := Grid(5, children...).Gap(1, 1)
	r := New()
	b.ReportAllocs()
	for b.Loop() {
		r.View(benchW, benchH, tree)
	}
}

func TestBoxTooSmallForItsFrame(t *testing.T) {
	t.Parallel()

	box := Box(lipgloss.NewStyle().Border(lipgloss.RoundedBorder()), Text("x"))
	if got := Render(Row(Fixed(1, box), Text("y")), 4, 3); strings.ContainsAny(got, "╭│╰") {
		t.Fatalf("a box one cell wide drew part of its border:\n%s", got)
	}
	if got := Render(Row(Fixed(2, box)), 4, 3); !strings.Contains(got, "╭╮") {
		t.Fatalf("a box just big enough for its border was not drawn:\n%s", got)
	}
}

func TestEmptySlotTakesNoGap(t *testing.T) {
	t.Parallel()
	for _, n := range []Child{Fixed(0, Text("x")), Auto(Text(""))} {
		f := Solve(Row(n, Text("b").ID("b"), n, Text("c").ID("c"), n).Gap(2), 10, 1)
		expectRect(t, f, "b", image.Rect(0, 0, 1, 1))
		expectRect(t, f, "c", image.Rect(3, 0, 4, 1))
		if got := Render(Row(n, Text("b"), n, Text("c")).Gap(2), 0, 0); got != "b  c" {
			t.Fatalf("natural size: %q", got)
		}
	}
	// A growing slot with no content still takes its gap, so measuring
	// leaves room for it rather than wrapping the text.
	if got := Render(Row(Text("a b"), Space()).Gap(1), 0, 0); got != "a b " {
		t.Fatalf("natural size with Space: %q", got)
	}
}

func TestGridRowHeights(t *testing.T) {
	t.Parallel()
	grid := Grid(1, Text("a").ID("a"), Text("b\nc\nd").ID("b"))
	// Extra height is shared on top of each row's own height, so no row
	// loses content it had at its natural height.
	for h, want := range map[int][2]image.Rectangle{
		4: {image.Rect(0, 0, 1, 1), image.Rect(0, 1, 1, 4)},
		6: {image.Rect(0, 0, 1, 2), image.Rect(0, 2, 1, 6)},
		7: {image.Rect(0, 0, 1, 2), image.Rect(0, 2, 1, 7)},
		// Short of height, rows give it up in proportion to their own.
		2: {image.Rect(0, 0, 1, 1), image.Rect(0, 1, 1, 2)},
	} {
		f := Solve(grid, 1, h)
		expectRect(t, f, "a", want[0])
		expectRect(t, f, "b", want[1])
	}
	if got := Render(grid, 1, 5); !strings.Contains(got, "d") {
		t.Fatalf("height 5 lost a line:\n%s", got)
	}
}

func TestTruncateKeepsStyleLimits(t *testing.T) {
	t.Parallel()
	tall := Text("x").Style(lipgloss.NewStyle().Height(3)).Truncate()
	expectRect(t, Solve(Column(tall, Text("y").ID("y")), 0, 0), "y", image.Rect(0, 3, 1, 4))

	// A transform that lengthens the text or adds lines is cut to one line too.
	for _, tf := range []func(string) string{
		func(s string) string { return strings.Repeat(s, 4) },
		func(s string) string { return s + "\n" + s },
	} {
		grown := Text("abc").Style(lipgloss.NewStyle().Transform(tf)).Truncate()
		if got := Render(Column(grown, Text("end")), 4, 0); got != "abc…\nend " {
			t.Fatalf("transform: %q", got)
		}
	}

	narrow := Text("abcdef").Style(lipgloss.NewStyle().MaxWidth(3)).Truncate()
	if got := Render(narrow, 0, 0); got != "ab…" {
		t.Fatalf("MaxWidth: %q", got)
	}
	if got := Render(Row(narrow, Text("|")), 0, 0); got != "ab…|" {
		t.Fatalf("MaxWidth measured: %q", got)
	}
}

// TestTruncateMeasuresAsDrawn checks that a truncated line's measured size,
// worked out from its style, is the size it is drawn at.
func TestTruncateMeasuresAsDrawn(t *testing.T) {
	t.Parallel()
	base := lipgloss.NewStyle()
	styles := map[string]lipgloss.Style{
		"plain":     base.Bold(true),
		"padding":   base.Padding(1, 2),
		"border":    base.Border(lipgloss.RoundedBorder()),
		"margins":   base.Margin(1, 3),
		"width":     base.Width(6).Padding(0, 1),
		"height":    base.Height(3).Border(lipgloss.NormalBorder()),
		"max width": base.MaxWidth(5).Padding(0, 1),
		"max both":  base.MaxWidth(7).MaxHeight(2).Border(lipgloss.NormalBorder()),
	}
	for name, st := range styles {
		for _, s := range []string{"ab", "a much longer line of text", "wide 字字字 glyphs"} {
			// Widths too narrow for a style's own frame are clipped instead.
			for _, avail := range []int{0, 9, 14, 80} {
				n := Text(s).Style(st).Truncate()
				f := Solve(Row(Auto(n).ID("n")), avail, 0)
				r, _ := f.Rect("n")
				got := textOf(n, r.Dx(), 0)
				if w, h := lipgloss.Size(got); w != r.Dx() || h != r.Dy() {
					t.Errorf("%s, %q in %d: measured %dx%d, drawn %dx%d:\n%s", name, s, avail, r.Dx(), r.Dy(), w, h, got)
				}
			}
		}
	}
}
