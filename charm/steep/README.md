<img width="1176" height="280" alt="banner" src="https://github.com/user-attachments/assets/b7950fed-b8f8-4155-852f-4333e5ef1b69" />

---

`steep` provides test helpers for Bubble Tea programs and component models. It
runs a model through the real Bubble Tea runtime, captures the latest view, and
adds helpers for sending input, waiting for output, checking messages, mutating
state inside `Update`, and writing snapshots.

Example of waiting condition failures:

<img width="833" height="302" alt="image" src="https://github.com/user-attachments/assets/f5f2cf9e-c0ba-48e7-a5f3-2cc63353a8ae" />

Example of failures when there is a snapshot mismatch (with stripping of ANSI data, to loosen constraints on snapshot comparisons):

<img width="961" height="390" alt="image" src="https://github.com/user-attachments/assets/63a26069-1dc9-49fa-93d4-8afd27850a4d" />

---

```go
import "github.com/lrstanley/x/charm/steep"
```

## Root Models

Use `NewHarness` when the thing under test is a full Bubble Tea root model that
implements `tea.Model`.

```go
type openCommandMsg struct {
	Command string
}

type commandPalette struct {
	width, height int
	query         string
	commands      []string
	opened        []string
}

func (m commandPalette) Init() tea.Cmd { return nil }

func (m commandPalette) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		switch msg.Key().Code {
		case tea.KeyEnter:
			selection := m.firstVisibleCommand()
			if selection == "" {
				return m, nil
			}
			return m, func() tea.Msg {
				return openCommandMsg{Command: selection}
			}
		default:
			m.query += msg.Key().Text
		}
	case openCommandMsg:
		m.opened = append(m.opened, msg.Command)
	case tea.QuitMsg:
		return m, tea.Quit
	}
	return m, nil
}

func (m commandPalette) View() tea.View {
	var b strings.Builder
	fmt.Fprintf(&b, "size=%dx%d\n", m.width, m.height)
	fmt.Fprintf(&b, "query=%s\n", m.query)
	for _, command := range m.visibleCommands() {
		fmt.Fprintf(&b, "- %s\n", command)
	}
	if len(m.opened) > 0 {
		fmt.Fprintf(&b, "opened=%s\n", m.opened[len(m.opened)-1])
	}
	return tea.NewView(strings.TrimSuffix(b.String(), "\n"))
}

func (m commandPalette) visibleCommands() []string {
	if m.query == "" {
		return m.commands
	}

	var out []string
	for _, command := range m.commands {
		if strings.Contains(command, m.query) {
			out = append(out, command)
		}
	}
	return out
}

func (m commandPalette) firstVisibleCommand() string {
	visible := m.visibleCommands()
	if len(visible) == 0 {
		return ""
	}
	return visible[0]
}

func TestCommandPalette(t *testing.T) {
	model := commandPalette{
		commands: []string{
			"vault read secret/data/app",
			"vault status",
			"vault token lookup",
		},
	}
	h := steep.NewHarness(t, model, steep.WithInitialTermSize(48, 8))

	h.WaitContainsStrings(t, []string{"size=48x8", "vault read", "vault status"})
	h.Type("status")
	h.WaitContainsString(t, "query=status")
	h.AssertStringNotContains(t, "vault read secret/data/app")

	h.Send(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	selected := steep.WaitMessage[openCommandMsg](t, h)
	h.WaitContainsString(t, "opened=vault status")

	if selected.Command != "vault status" {
		t.Fatalf("selected command = %q, want vault status", selected.Command)
	}
}
```

## Component Models

Use `NewComponentHarness` for components that are not full `tea.Model` roots, but
do expose `View() string` and one of these update shapes:

```go
Update(tea.Msg) tea.Cmd
Update(tea.Msg) (M, tea.Cmd)
```

This matches the style used by component-heavy TUIs: create the component with
mock data, drive it through key messages, wait for visible output, then assert
dimensions or snapshots.

```go
type inventoryRow struct {
	ID   string
	Name string
	Type string
}

type inventoryTable struct {
	width, height int
	rows          []inventoryRow
	filter        string
	selected      int
}

func (m *inventoryTable) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		switch msg.String() {
		case "down", "j":
			m.moveDown()
		case "up", "k":
			m.moveUp()
		}
	}
	return nil
}

func (m *inventoryTable) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	rows := m.visibleRows()
	var b strings.Builder
	fmt.Fprintf(&b, "inventory %dx%d\n", m.width, m.height)
	fmt.Fprintf(&b, "filter=%s\n", m.filter)
	if len(rows) == 0 {
		return b.String() + "no rows"
	}
	for i, row := range rows {
		prefix := " "
		if i == m.selected {
			prefix = ">"
		}
		fmt.Fprintf(&b, "%s %-10s %s\n", prefix, row.Name, row.Type)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func (m *inventoryTable) GetWidth() int {
	w, _ := steep.Dimensions(m.View())
	return w
}

func (m *inventoryTable) GetHeight() int {
	_, h := steep.Dimensions(m.View())
	return h
}

func (m *inventoryTable) visibleRows() []inventoryRow {
	if m.filter == "" {
		return m.rows
	}

	var out []inventoryRow
	for _, row := range m.rows {
		if strings.Contains(row.Name, m.filter) || strings.Contains(row.Type, m.filter) {
			out = append(out, row)
		}
	}
	return out
}

func (m *inventoryTable) moveDown() {
	if m.selected < len(m.visibleRows())-1 {
		m.selected++
	}
}

func (m *inventoryTable) moveUp() {
	if m.selected > 0 {
		m.selected--
	}
}

func TestInventoryTable(t *testing.T) {
	table := &inventoryTable{}
	h := steep.NewComponentHarness(t, table, steep.WithInitialTermSize(32, 6))

	steep.Mutate(t, h, func(m *inventoryTable) *inventoryTable {
		m.rows = []inventoryRow{
			{ID: "secret-app", Name: "secret/app", Type: "kv"},
			{ID: "database", Name: "database", Type: "database"},
			{ID: "token", Name: "token", Type: "auth"},
		}
		return m
	})
	h.WaitContainsStrings(t, []string{"secret/app", "database", "token"})

	h.Type("j")
	h.WaitSettleMessages(
		t,
		steep.WithSettleTimeout(25*time.Millisecond),
		steep.WithCheckInterval(5*time.Millisecond),
	)

	steep.Mutate(t, h, func(m *inventoryTable) *inventoryTable {
		m.filter = "secret"
		m.selected = 0
		return m
	})
	h.WaitContainsString(t, "secret/app")
	h.WaitSettleView(t).
		AssertStringNotContains(t, "database", "token").
		AssertDimensions(t, table.GetWidth(), table.GetHeight()).
		RequireSnapshotNoANSI(t, snapshot.WithSuffix("inventory"))
}
```

## Common Helpers

- `Type("text")` sends one key press per rune.
- `Send(msg)` sends any `tea.Msg` to the running program.
- `WaitContainsString` and `WaitContainsStrings` wait until the view matches.
- `WaitNotContainsString` waits until unwanted content disappears.
- `WaitSettleMessages` waits until `Update` stops receiving messages for the
  configured settle timeout. Use `WithSettleIgnoreMsgs` with one value per type
  to ignore (for example `steep.WithSettleIgnoreMsgs(myTickMsg{})`) so periodic
  or background message types do not reset the quiet period.
- `WaitSettleView` waits until repeated `View` samples stop changing.
- `Messages` returns observed messages, excluding internal mutation messages.
- `MessagesOfType[T]`, `WaitMessage[T]`, `WaitMessages[T]`, and
  `WaitMessageWhere[T]` inspect messages by concrete type.
- `Dimensions`, `AssertWidth`, `RequireWidth`, `AssertHeight`, `RequireHeight`,
  `AssertDimensions`, and `RequireDimensions` use ANSI-aware display width.
- `Assert*` helpers report errors and keep the test running. Use `Require*`
  helpers when the next step depends on the check passing and should stop
  immediately.

Most helpers exist in both package-level and harness-level forms. Use the
package-level helpers when you already have a simple value that implements
`View() string` and do not need the Bubble Tea runtime:

```go
steep.AssertStringContains(t, model, "ready")
steep.AssertDimensions(t, model, 80, 24)
```

Use the harness methods when the test needs runtime behavior, such as window
size messages, commands, key input, observed messages, snapshots, or final model
state:

```go
h := steep.NewComponentHarness(t, model)
h.Type("j")
h.WaitContainsString(t, "selected")
```

Harness `Wait*` methods (except [WaitFinished]), assertion, and snapshot methods
return `*Harness`, so they can be chained:

```go
h.WaitContainsString(t, "ready").
	WaitSettleView(t).
	AssertStringContains(t, "ready").
	AssertStringNotContains(t, "loading").
	RequireSnapshotNoANSI(t)
```

When the model keeps scheduling ticks or other chatter after the interesting
work finishes, pass sample values so those dynamic types are skipped for
settlement:

```go
h.WaitSettleMessages(t,
	steep.WithSettleIgnoreMsgs(heartbeatTick{}),
	steep.WithSettleTimeout(25*time.Millisecond),
)
```

To read the view after a content wait, use [Harness.View] (or the package-level
[WaitContainsString] / [WaitView] helpers, which return the matched view):

```go
h.WaitContainsString(t, "ready")
if strings.Contains(h.View(), "warning") {
	t.Fatal("unexpected warning")
}
```

## Mutating Models

Prefer to drive models through public behavior: send messages, type keys, or use
commands that a user could actually trigger. `Mutate` is available for the cases
where that would make a test noisy or impractical, such as seeding large
component data sets, setting an internal filter, or moving a component into a
state that has no public message.

`Mutate` runs the change from inside the Bubble Tea `Update` loop, which keeps
the harness state consistent:

```go
steep.Mutate(t, h, func(m *inventoryTable) *inventoryTable {
	m.rows = rows
	m.filter = "secret"
	return m
})
```

Use it sparingly. If the behavior can be tested by sending `tea.Msg` values or
typing keys, that usually gives a better test because it covers the same path a
real program uses.

## Snapshots

`steep` includes a small snapshot package at
`github.com/lrstanley/x/charm/steep/snapshot`. Snapshots are written under
`testdata`. Set `UPDATE_SNAPSHOTS=true` or pass `snapshot.WithUpdate(true)` to
create or update snapshots.

```go
func TestStatusView(t *testing.T) {
	view := "\x1b[32mcluster=acme-corp\x1b[0m\nstatus=unsealed\ntoken=s.dev-token\n"

	snapshot.AssertEqual(
		t,
		view, // Or yourModel.View()
		snapshot.WithSuffix("status"),
		snapshot.WithStripANSI(),
		snapshot.WithTransform(func(bts []byte) []byte {
			return bytes.ReplaceAll(bts, []byte("s.dev-token"), []byte("<token>"))
		}),
	)
}
```

Use `snapshot.RequireEqual` when a mismatch should stop the test immediately.
For harnesses, use the convenience methods:

```go
h.AssertSnapshot(t, snapshot.WithSuffix("ansi"))
h.AssertSnapshotNoANSI(t, snapshot.WithSuffix("plain"))
h.RequireSnapshot(t, snapshot.WithSuffix("ansi"))
h.RequireSnapshotNoANSI(t, snapshot.WithSuffix("plain"))
```

Use `WithSuffix` when a test writes more than one snapshot or when a subtest
needs a more descriptive file name. Use `WithStripANSI` and `WithStripPrivate`
when terminal styling, spinners, or private-use glyphs would otherwise make
snapshots unstable.
