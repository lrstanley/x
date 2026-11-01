// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package integration_test

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lrstanley/x/term/bloom"
	"github.com/lrstanley/x/term/bloom/flex"
	"github.com/lrstanley/x/term/bloom/fx"
	"github.com/lrstanley/x/term/steep/snapshot"
)

// view is a component drawn from a fixed string.
type view string

func (v view) View() string { return string(v) }

// responsive switches between a row and a column at 30 columns.
type responsive struct{}

func (responsive) View() string { return "fallback" }

func (responsive) Layout(ctx bloom.Context) bloom.Child {
	if ctx.Width >= 30 {
		return bloom.Row(bloom.Fill(bloom.Text("wide-a")), bloom.Fill(bloom.Text("wide-b")))
	}
	return bloom.Column(bloom.Text("narrow-a"), bloom.Text("narrow-b"))
}

// hatch draws a pattern on every cell it is given, to show clipping.
type hatch struct{}

func (hatch) Draw(scr uv.Screen, area uv.Rectangle) {
	for y := area.Min.Y; y < area.Max.Y+2; y++ {
		for x := area.Min.X; x < area.Max.X+2; x++ {
			scr.SetCell(x, y, uv.NewCell(scr.WidthMethod(), "/"))
		}
	}
}

var (
	border = lipgloss.NewStyle().Border(lipgloss.NormalBorder())
	dots   = view(strings.TrimSuffix(strings.Repeat(strings.Repeat(".", 30)+"\n", 8), "\n"))
)

func sidebarTree() bloom.Child {
	return bloom.Column(
		bloom.Fill(bloom.Row(
			bloom.Fixed(14, bloom.Box(border, bloom.Column(bloom.Text("Home"), bloom.Text("Settings"), bloom.Space(), bloom.Text("v1.0")))).ID("sidebar").Collapsible(1),
			bloom.Fill(bloom.Box(border, bloom.Text("reader"))).ID("reader").Min(20),
		).Gap(1)),
		bloom.Text("status"),
	)
}

func TestLayout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		w, h int
		tree bloom.Child
	}{
		{"row", 30, 3, bloom.Row(bloom.Fixed(8, bloom.Box(border, bloom.Text("fixed"))), bloom.Fill(bloom.Box(border, bloom.Text("fill"))), bloom.Box(border, bloom.Text("auto"))).Gap(1)},
		{"grow weights", 30, 3, bloom.Row(bloom.Grow(1, bloom.Box(border, bloom.Text("1"))), bloom.Grow(2, bloom.Box(border, bloom.Text("2"))))},
		{"min and max", 30, 3, bloom.Row(bloom.Fill(bloom.Box(border, bloom.Text("max 8"))).Max(8), bloom.Fill(bloom.Box(border, bloom.Text("min 18"))).Min(18))},
		{"column space", 10, 5, bloom.Column(bloom.Text("top"), bloom.Space(), bloom.Text("bottom"))},
		{"justify", 20, 1, bloom.Row(bloom.Text("a"), bloom.Text("b"), bloom.Text("c")).Justify(flex.SpaceBetween)},
		{"align", 12, 3, bloom.Row(bloom.Text("top"), bloom.Text("mid").Style(lipgloss.NewStyle()), bloom.Text("x\ny\nz")).Align(bloom.Center).Gap(1)},
		{"slot align", 12, 3, bloom.Row(bloom.Auto(bloom.Text("end")).Align(bloom.End), bloom.Text("x\ny\nz")).Gap(1)},
		{"sidebar wide", 40, 6, sidebarTree()},
		{"sidebar collapsed", 30, 6, sidebarTree()},
		{"nested boxes", 20, 6, bloom.Box(border.Padding(0, 1), bloom.Column(bloom.Box(border, bloom.Text("inner")), bloom.Text("below")))},
		{"styled text wraps", 12, 4, bloom.Column(bloom.Text("hello wrapping world").Style(lipgloss.NewStyle().Padding(0, 1)))},
		{"scroll", 6, 3, bloom.Scroll(view("1\n2\n3\n4\n5\n6"), 2)},
		{"centered", 11, 5, bloom.Centered(bloom.Box(border, bloom.Text("mid")))},
		{"layer anchors", 30, 8, bloom.Overlay(dots,
			bloom.Layer(bloom.Text("TL")).Anchor(bloom.Start, bloom.Start),
			bloom.Layer(bloom.Text("BR")).Anchor(bloom.End, bloom.End).Margin(1),
			bloom.Layer(bloom.Column(bloom.Text("opaque"), bloom.Space(), bloom.Text("layer"))).Size(8, 4).Offset(0, -1),
		)},
		{"anchor to", 30, 8, bloom.Overlay(
			bloom.Column(bloom.Row(bloom.Text("find: "), bloom.Fixed(12, bloom.Text("[query     ]")).ID("input")), bloom.Fill(dots)),
			bloom.Layer(bloom.Box(border, bloom.Text("result 1\nresult 2"))).AnchorTo("input", bloom.Bottom),
		)},
		{"layouter wide", 40, 1, responsive{}},
		{"layouter narrow", 20, 2, responsive{}},
		{"drawable clipped", 12, 4, bloom.Row(bloom.Fixed(3, bloom.Text("ab")), bloom.Fixed(4, bloom.Drawable(hatch{})), bloom.Text("cd"))},
		{"wide glyphs clipped", 7, 2, bloom.Row(bloom.Fixed(3, bloom.Text("世界世")), bloom.Text("|界"))},
		{"slide half way", 20, 6, bloom.Overlay(view(strings.Repeat("....................\n", 6)), bloom.Layer(bloom.Box(border, bloom.Text("up"))).Transition(0.5, fx.Slide{From: fx.Bottom}))},
		{"natural size", 0, 0, bloom.Row(bloom.Box(border, bloom.Text("a")), bloom.Box(border, bloom.Text("bb\nbb"))).Gap(1)},
		{"natural size of a percent", 0, 0, bloom.Row(bloom.Percent(50, bloom.Box(border, bloom.Text("half"))), bloom.Text("|"))},
		{"box smaller than its frame", 1, 3, bloom.Box(border, bloom.Text("x"))},
		{"fill wraps at its share", 10, 4, bloom.Column(bloom.Row(
			bloom.Fill(bloom.Text("aa aa aa").Style(lipgloss.NewStyle())),
			bloom.Fill(bloom.Text("bb bb bb").Style(lipgloss.NewStyle())),
		), bloom.Text("--"))},
		{"transformed text", 0, 0, bloom.Row(bloom.Text("x").Style(lipgloss.NewStyle().Transform(strings.ToUpper)), bloom.Text("y").Style(lipgloss.NewStyle().Transform(func(string) string { return "hello" })))},
		{"transform wraps", 4, 4, bloom.Column(bloom.Text("x").Style(lipgloss.NewStyle().Transform(func(string) string { return "aa aa aa" })), bloom.Text("end"))},
		{"style width wraps", 10, 3, bloom.Column(bloom.Text("abcdef").Style(lipgloss.NewStyle().Width(3)), bloom.Text("end"))},
		{"anchor scrolled away", 20, 4, bloom.Overlay(
			bloom.Scroll(bloom.Column(bloom.Text("anchor").ID("a"), bloom.Text("1"), bloom.Text("2"), bloom.Text("3"), bloom.Text("4")), 2),
			bloom.Layer(bloom.Text("popup")).AnchorTo("a", bloom.Bottom),
		)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			snapshot.RequireEqual(t, bloom.Render(tt.tree, tt.w, tt.h), snapshot.WithANSI(false))
		})
	}
}

func TestBackdrop(t *testing.T) {
	t.Parallel()

	base := bloom.Text(strings.Repeat("base text \n", 4)).Style(lipgloss.NewStyle().Foreground(lipgloss.Color("#ff8800")))
	tree := bloom.Overlay(base,
		bloom.Layer(bloom.Box(border, bloom.Text("vex"))).
			Backdrop(fx.Recolor{Fg: lipgloss.Color("#666666"), Bg: lipgloss.Color("#111111")}).
			Shadow(1, 1, fx.Dim{Amount: 1}),
	)
	snapshot.RequireEqual(t, bloom.New().Render(14, 6, tree))
}
