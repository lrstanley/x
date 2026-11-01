# bloom

bloom lays out, composites, and routes input for [Bubble Tea](https://github.com/charmbracelet/bubbletea)
views. A view is described every frame as a tree of nodes built from model
state, and any component with a `View() string` method is a leaf.

```go
func (m *model) View() tea.View {
	v := m.ui.View(m.width, m.height, bloom.Overlay(
		bloom.Column(
			bloom.Fill(bloom.Row(
				bloom.Fixed(24, m.sidebar).ID(paneSidebar).Collapsible(1),
				bloom.Fill(m.reader).ID(paneReader).Min(40),
			)),
			m.statusbar,
		),
		bloom.Layer(m.dialog).
			Modal().
			Backdrop(fx.Dim{Amount: 0.5}).
			Transition(m.dialogAnim.Progress(), fx.Slide{From: fx.Bottom}, fx.Fade{}),
	))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}
```

## Who sizes what

Constraints go down, sizes come up, and parents place. A parent decides each
child's rectangle, whether it is collapsed, and how it is clipped. How a child
takes part in sharing space is set where it is placed, by its slot, not by the
child, so the same component can be fixed in one place and fill another.

| Slot                  | Starts at           | Grows      | Shrinks       |
| --------------------- | ------------------- | ---------- | ------------- |
| `Fixed(n, c)`         | n cells             | no         | no            |
| `Auto(c)`, or bare `c` | its natural size    | no         | yes, to `Min` |
| `Fill(c)`, `Grow(w, c)` | 0                 | by weight  | yes, to `Min` |
| `Percent(p, c)`       | p% of the container | no         | yes, to `Min` |

`Min` and `Max` clamp a slot; the minimum is zero unless set. When the
minimums do not fit, `Collapsible(priority)` slots are dropped, lowest
priority first. Children stretch across the cross axis unless aligned. A
slot that can never have any cells, such as `Fixed(0, …)`, takes no gap,
so a slot animated shut leaves none behind.

`Fixed(n, nil)` leaves n empty cells on the container's axis, such as a
blank line in a column; `Space()` grows to fill the remaining space.
`Grid(cols, children...).Gap(x, y)` fills equal-width columns row by row,
keeping the column widths on a short last row. Each row is as tall as its
tallest cell; rows share extra height equally, and give up height in
proportion to their own when short of it. A
slot inside a grid cell sizes its content horizontally within that cell.
`Wrap(children...).Gap(x, y)` flows natural-size children onto new lines,
including its wrapped height when placed in an `Auto` slot. Overwide
children get a line of their own and are clipped.

A child that wants its size implements `Layouter`, and one that knows its
natural size implements `Measurer`. Layout is a pure function of the tree, so
`Solve` gives the same rectangles in `Update` as the frame drawn from the same
state, for components like viewports that need their size there.

## Overlays, effects, and animation

`Overlay` draws `Layer`s over a base: anchored to the screen or to another
node with `AnchorTo`, `Modal` to take every mouse event, with a `Backdrop` and
`Shadow` applied to what is beneath. The [fx](fx) package's effects transform
cells for a progress value, so the same effect serves as a transition on a
node and as a backdrop:

- `Fade`, `Slide`, `Wipe`, `Dissolve`, `Typewriter`, `Scramble`, `Unfold`,
  `Iris`, `Shade`, `Blinds`, `Wave`, `Decode`, and `Glitch` bring a node in.
- `Dim`, `Desaturate`, `Tint`, `Recolor`, and `Spotlight` suit backdrops.
- `Shake`, `Shimmer`, `Burst` (particles), and `Roll` (rolling numbers) take
  their own progress: a shake's, a loading shimmer's phase, a burst's age, or
  a number's roll from its old value.
- `Stagger`, `Delay`, `Reverse`, and `Ease` combine them: a list cascading in
  is `fx.Stagger{Effect: fx.Slide{From: fx.Left}, From: fx.Top}`.

Any `func(*canvas.Surface, float64)` is an effect too.

The [anim](anim) package keeps animations in the model: a `Value` eases or
springs toward a target, a `Presence` animates showing and hiding, and a
`Sequence` chains steps on one value, such as in, hold, and out, or a loop.
They are advanced by messages their commands schedule; `anim.Update(msg,
&m.dialog, &m.toast)` passes a message to all of them. Common cases are one
call each: `Value.Play(0, 1)` runs a one-shot effect such as a shake from the
start, `Presence.ShowFor(2*time.Second)` shows a notification and hides it
again, and `anim.NewLoop(period)` is a phase for a shimmer or scrolling text.
A layer with `.Presence(m.dialog, fx.Fade{})`, or a node in `bloom.Present`,
is there only while its presence is visible.

Layout can animate itself: a slot or layer with an ID and `.Animate()` moves
and resizes smoothly to wherever layout puts it, its children reflowing as it
goes, with no animation state in the model. Pass every message to
`Root.Update` and return its command, which ticks while anything is moving.
`Marquee` scrolls text too long for its space, or cuts it short with "…".
`Text(s).Truncate()` uses the same truncation on one line, turning newlines
into spaces; padding and borders count against the available text width.

`Layer(child).Presence(p, effects...)` takes its progress from an
`anim.Presence` and omits the layer, backdrop, shadow, and input entries
while hidden. `Present(p, child, effects...)` does the same for ordinary
nodes; a hidden child takes no space and leaves no gap in its container.

## Input and focus

`Root.View` installs an `OnMouse` handler that sends the node under the
pointer a `MouseMsg` with the path of IDs to it and local coordinates. IDs can
be of any comparable type, so `msg.Target[itemID]()` finds a list item without
parsing strings. `Focus` keeps the focused ID in the model; `Push` confines it
to a scope, such as a modal layer, until `Pop` puts it back.
`focus.Update(msg, ui.Frame())` handles tab, shift+tab, and routed mouse
clicks within that scope, reporting whether the message was handled.

## Performance

Leaf strings are measured and parsed once and cached, Lip Gloss renders are
reused while a node's style and size do not change, and a frame that is the
same as the last one is not composited again. On a 200×60 dashboard, an
unchanged frame takes about 0.04ms, a frame where most content changed 0.44ms,
and a dialog animating in over a dimmed backdrop 0.58ms, against the 8.3ms of
a frame at 120 frames a second (`go test -bench . ./...`).

## Examples

The [_examples](_examples) run with `go run ./<name>` from that directory, in
rough order of complexity:

| Example | Shows |
| --- | --- |
| [basics](_examples/basics) | Layouts printed with `Render`, no program: every slot, `Min`/`Max`, `Collapsible`, gaps, each `Justify`, `Align`, `Space`, `Box`, wrapping text, `Centered`, natural size, and a layout inside Lip Gloss |
| [standard](_examples/standard) | A sidebar that is a `Layouter`, a plain `View() string` reader, a `Measurer` status bar, and clicks |
| [responsive](_examples/responsive) | Panels collapsing by priority, `Percent`, `Layouter`s that rebuild for their width, paging with `Solve` in Update, and mouse wheel routing |
| [forms](_examples/forms) | Focusable inputs with the cursor, an `AnchorTo` dropdown, stacked `Modal` dialogs as focus scopes with vex's `Recolor` backdrop and shadows, and spring, slide, fade, and typewriter transitions |
| [effects](_examples/effects) | Every effect as a looping `Transition`, two custom `fx.Func` effects, a `Drawable`, and each backdrop over the gallery |
| [transitions](_examples/transitions) | `Unfold`, `Iris`, `Shade`, `Blinds`, `Wave`, and the `Stagger`, `Delay`, `Reverse`, and `Ease` combinators, and dialogs opening from their button |
| [launch](_examples/launch) | A deploy flow: `Shimmer` placeholders, `Roll`ing numbers, `Shake` on a wrong answer, a `Spotlight` tour with `Decode`, and a `Burst` of confetti with a `Glitch`ing badge |
| [ticker](_examples/ticker) | `Marquee` headlines and list items, and `anim.Sequence` for a banner and a pulse |
| [reflow](_examples/reflow) | Layout animations with `.Animate()`: a sidebar, a re-sorting list, a changing split, a panel coming and going, and a layer following the selection |
| [dashboard](_examples/dashboard) | All of it at 120 frames a second, with the time each frame takes |

Their tests drive them with steep, and record a GIF of each when exporting is
turned on: `STEEP_EXPORT_DIR=/tmp STEEP_EXPORT_MODE=on go test -count 1 ./...`.
