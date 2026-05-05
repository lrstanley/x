<img width="1176" height="280" alt="banner" src="https://github.com/user-attachments/assets/b7950fed-b8f8-4155-852f-4333e5ef1b69" />

---

`steep` provides test helpers for [Bubble Tea v2](https://github.com/charmbracelet/bubbletea) programs and component models. It runs a model through the real runtime with a `vt` terminal emulator as input/output, captures the latest rendered terminal screen, and adds helpers for sending input, waiting on output, inspecting messages, mutating state inside `Update`, and writing snapshots. Those messages are [`tea.Msg`](https://pkg.go.dev/charm.land/bubbletea/v2#Msg) values—the same kinds `Update` receives in Bubble Tea v2. Go excerpts in this file are test-shaped snippets only; import `github.com/lrstanley/x/charm/steep`, `charm.land/bubbletea/v2` (as `tea`), and any other packages you use (`time`, `strings`, `github.com/lrstanley/x/charm/steep/snapshot`, and so on).

Example of waiting condition failures:

<img width="833" height="302" alt="image" src="https://github.com/user-attachments/assets/f5f2cf9e-c0ba-48e7-a5f3-2cc63353a8ae" />

Example of failures when there is a snapshot mismatch (with stripping of ANSI data, to loosen constraints on snapshot comparisons):

<img width="961" height="390" alt="image" src="https://github.com/user-attachments/assets/63a26069-1dc9-49fa-93d4-8afd27850a4d" />

---

## Root models

Use `NewHarness` when the thing under test is a full Bubble Tea root model that
implements `tea.Model` with `Update(tea.Msg)` and `View() tea.View`.

Example (construct `model` (`tea.Model`) in your test, plus any custom message types such as `openCommandMsg`):

```go
func TestCommandPalette(t *testing.T) {
	// model := ...
	h := steep.NewHarness(t, model, steep.WithWindowSize(48, 8))

	h.WaitStrings([]string{"size=48x8", "vault read", "vault status"})
	h.Type("status")
	h.WaitString("query=status")
	h.AssertNotString("vault read secret/data/app")

	h.SendProgram(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	selected := steep.WaitMessage[openCommandMsg](t, h)
	h.WaitString("opened=vault status")

	if selected.Command != "vault status" {
		t.Fatalf("selected command = %q, want vault status", selected.Command)
	}
}
```

## Component models

Use `NewComponentHarness` for components that are not full `tea.Model` roots, but
do expose `View() string` and one of these update shapes: `Update(tea.Msg) tea.Cmd` or `Update(tea.Msg) (M, tea.Cmd)`.

This matches the style used by component-heavy TUIs: create the component with
mock data, drive it through key messages, wait for visible output, then assert
dimensions or snapshots.

Example (`MyTable` is your component type; `*MyTable` matches what you pass to `NewComponentHarness`):

```go
func TestInventoryTable(t *testing.T) {
	table := &MyTable{}
	h := steep.NewComponentHarness(t, table,
		steep.WithWindowSize(32, 6),
		steep.WithStripANSI(),
	)

	steep.Mutate(h, func(m *MyTable) *MyTable {
		// seed rows, filters, etc.
		return m
	})
	h.WaitStrings([]string{"secret/app", "database", "token"})

	h.Type("j")
	h.WaitSettleMessages(
		steep.WithSettleTimeout(25*time.Millisecond),
		steep.WithCheckInterval(5*time.Millisecond),
	)

	steep.Mutate(h, func(m *MyTable) *MyTable {
		m.filter = "secret"
		m.selected = 0
		return m
	})
	h.WaitString("secret/app")
	w, ht := steep.Dimensions(h.View())
	h.WaitSettleView().
		AssertNotString("database").
		AssertDimensions(w, ht).
		RequireViewSnapshot(snapshot.WithSuffix("inventory"))
}
```

## Harness view and terminal layout

- **`Harness.View`** returns the current **terminal screen** from the vt emulator (`vt.Render`). **`Wait*`**, **`Assert*`**, **`Require*`**, **`WaitSettle`** (view settlement), layout helpers that measure the **`View()` string** (`**Dimensions**`, **`AssertWidth`**, and related), and snapshot methods all sample this output. Package-level helpers work the same way when you pass your own **`Viewable`**.
- **`Harness.Width`**, **`Harness.Height`**, and **`Harness.Dimensions`** report the **emulator window size** in cells (from **`WithWindowSize`** and **`Resize`**). That is separate from measuring the **text layout** of **`Harness.View()`** via **`Dimensions(string)`** or harness layout assertions.
- After shutdown, **`FinalModel`** returns the last root **`tea.Model`**. Use **`QuitProgram`** (alternatively rely on test cleanup), then **`WaitFinished`**, before **`FinalModel`** if you need the final model.

## Terminal emulation

The harness runs a `tea.Program` wired to a real `vt` emulator. Besides `Resize` (which should deliver `tea.WindowSizeMsg`), you can use:

- **`Focus`** / **`Blur`** — focus in/out when focus reporting is enabled.
- **`Paste`** — paste text (bracketed paste when enabled).
- **`Bounds`**, **`Width`**, **`Height`**, **`Dimensions`**, **`IsAltScreen`** — query terminal state.
- **`Scrollback`**, **`ScrollbackCount`**, **`SetScrollbackSize`**, **`ClearScrollback`** — scrollback inspection and limits.
- **`FgColor`**, **`BgColor`**, **`CursorColor`**, and **`SetFgColor`** / **`SetBgColor`** / **`SetCursorColor`** / **`SetDefault*Color`** — read or override palette state on the emulator.

## Common helpers

- **`Type("text")`** sends one `tea.KeyPressMsg` per rune (`Key` carries both `Code` and `Text`).
- **`SendProgram(msg)`** sends any **`tea.Msg`** (Bubble Tea messages, terminal-driven events, or your own test messages).
- **`WaitString`**, **`WaitStrings`**, **`WaitNotString`**, **`WaitNotStrings`** wait until the model view contains or omits substrings. **`WaitBytes`**, **`WaitNotBytes`**, **`WaitBytesFunc`**, and **`WaitStringFunc`** are the byte- and predicate-shaped variants (package-level and on **`Harness`**).
- **`WaitMatch`** and **`WaitNotMatch`** wait until the view matches, or no longer matches, a regular expression (compiled with the standard `regexp` package and checked with `MatchString`). Invalid patterns fail the test when the helper runs. Use `^` and `$` in the pattern when the whole buffer must match, not a substring.
- **`WaitSettleMessages`** waits until `Update` stops receiving messages for the configured settle timeout. Use **`WithSettleIgnoreMsgs`** with one value per type to ignore (for example `steep.WithSettleIgnoreMsgs(myTickMsg{})`) so periodic or background message types do not reset the quiet period.
- **`WaitSettleView`** waits until repeated **`View()`** samples stop changing.
- **`MessageHistory`** returns an iterator over all messages observed so far (excluding internal `Mutate` traffic). **`Messages(ctx)`** yields historical messages then streams live ones; **`LiveMessages(ctx)`** yields only messages that arrive after the call returns.
- **`WaitMessage[T]`** waits until a message of concrete type **`T`** has been observed (history + live). **`WaitLiveMessage[T]`** waits only for a **new** message after the call. **`WaitMessageWhere`** / **`WaitLiveMessageWhere`** generalize that with a predicate on **`tea.Msg`**.
- **`AssertHasMessage[T]`** / **`RequireHasMessage[T]`** assert type presence in history.
- **`FilterMessagesType[T]`**, **`FilterMessagesFunc[T]`**, and **`IgnoreMessagesReflect`** help narrow iterators when asserting on message streams.
- **`WithStripANSI`** removes ANSI from the view before string/regex waits, substring assertions, layout checks, and (when set on the harness) snapshot comparisons.
- **`Dimensions`**, **`AssertWidth`**, **`RequireWidth`**, **`AssertHeight`**, **`RequireHeight`**, **`AssertDimensions`**, and **`RequireDimensions`** use ANSI-aware display width (on the view string with ANSI stripped when **`WithStripANSI`** is set). On a harness, those helpers measure the current **`Harness.View()`** buffer. **`Harness.Width`**, **`Harness.Height`**, and **`Harness.Dimensions`** report the terminal size in cells from the emulator, not the string dimensions of **`View()`**.
- **`AssertMatch`**, **`AssertNotMatch`**, **`RequireMatch`**, and **`RequireNotMatch`** check the current view against a regular expression the same way as **`WaitMatch`** / **`WaitNotMatch`**.
- **`AssertString`** / **`RequireString`** (and **`AssertNotString`** / **`RequireNotString`**) check a single substring. Use **`AssertStrings`** / **`RequireStrings`** (and **`AssertNotStrings`** / **`RequireNotStrings`**) when the view must contain, or must omit, every string in a list (same idea as **`WaitStrings`** / **`WaitNotStrings`**).
- **`Assert*`** helpers report errors and keep the test running. Use **`Require*`** helpers when the next step depends on the check passing and should stop immediately.

Most helpers exist in both package-level and harness-level forms. Use the package-level helpers when you already have a simple value that implements **`View() string`** and do not need a **`tea.Program`**:

```go
func TestReadyLayout(t *testing.T) {
	// model exposes View() string
	steep.AssertString(t, model.View, "ready")
	steep.AssertDimensions(t, model.View, 80, 24)
}
```

Use the harness methods when the test needs runtime behavior, such as window size messages, commands, key input, observed messages, snapshots, terminal emulation, or final model state:

```go
func TestSelection(t *testing.T) {
	// model / table: your component or root model
	h := steep.NewComponentHarness(t, model)
	h.Type("j")
	h.WaitString("selected")
}
```

Options you pass to **`NewHarness`** or **`NewComponentHarness`** are kept on the **`Harness`** and merged with the options on each method that accepts **`...Option`** (for example **`Wait*`**, **`Assert*`**, **`Require*`**, **`Mutate`**, and **`WaitFinished`**). Harness-level options are applied first; options on a specific call are applied after and win for the same setting (a per-call **`WithTimeout(5*time.Second)`** overrides a default **`WithTimeout(2*time.Second)`** on the constructor for that call only). Use **`WithProgramOptions`** to append extra **`tea.ProgramOption`** values when constructing the **`tea.Program`** (input, output, signals, and initial window size are still fixed by the harness). Snapshot methods still take **`snapshot.Option`** arguments separately; the harness’s **`WithStripANSI`** is mapped into the snapshot path when you use **`AssertViewSnapshot`** or **`RequireViewSnapshot`**, as before.

```go
func TestTimeouts(t *testing.T) {
	// model := ...
	h := steep.NewHarness(t, model,
		steep.WithTimeout(5*time.Second),
		steep.WithStripANSI(),
	)
	h.WaitString("ok")                                     // 5s timeout, strips ANSI
	h.WaitString("slow", steep.WithTimeout(30*time.Second)) // 30s for this call only
}
```

Harness **`Wait*`** methods (except **`WaitFinished`**), assertion, snapshot, and **`Mutate`** methods return **`*Harness`**, so they can be chained:

```go
func TestChained(t *testing.T) {
	// h from NewHarness / NewComponentHarness
	h.WaitString("ready").
		WaitSettleView().
		AssertString("ready").
		AssertNotString("loading").
		AssertMatch("ready").
		AssertNotMatch("(?i)error|panic").
		RequireViewSnapshot(snapshot.WithStripANSI())
}
```

When the model keeps scheduling ticks or other chatter after the interesting work finishes, pass sample values so those dynamic types are skipped for settlement:

```go
func TestSettleIgnoringTick(t *testing.T) {
	// h from NewHarness / NewComponentHarness
	h.WaitSettleMessages(
		steep.WithSettleIgnoreMsgs(myTickMsg{}),
		steep.WithSettleTimeout(25*time.Millisecond),
	)
}
```

To read the view after a content wait, use **`Harness.View`** (or the package-level **`WaitString`**, **`WaitMatch`** / **`WaitNotMatch`**, or **`WaitViewFunc`** helpers, which return the last sampled view that satisfied the wait):

```go
func TestInspectView(t *testing.T) {
	// h from NewHarness / NewComponentHarness
	h.WaitString("ready")
	// e.g. fail if strings.Contains(h.View(), "warning")
}
```

Shutting down: call **`QuitProgram`** when the test should end the program, then **`WaitFinished`** (also invoked from harness cleanup) before **`FinalModel`** if you need the model after exit.

## Mutating models

Prefer to drive models through public behavior: send messages, type keys, or use commands that a user could actually trigger. **`Mutate`** is available for the cases where that would make a test noisy or impractical, such as seeding large component data sets, setting an internal filter, or moving a component into a state that has no public message.

**`Mutate`** runs the change from inside the `tea.Program`'s **`Update`** handling, which keeps the harness state consistent, and returns **`*Harness`** for chaining:

```go
func TestMutate(t *testing.T) {
	// h := steep.NewHarness(t, model) or NewComponentHarness(...)
	steep.Mutate(h, func(m *MyModel) *MyModel {
		// m.rows = ...
		m.filter = "secret"
		return m
	})
}
```

Use it sparingly. If the behavior can be tested by sending **`tea.Msg`** values or typing keys, that usually gives a better test because it covers the same path a real `tea.Program` uses.

## Snapshots

`steep` includes a small snapshot package at
`github.com/lrstanley/x/charm/steep/snapshot`. Snapshots are written under
`testdata`. Set `UPDATE_SNAPSHOTS=true` or pass `snapshot.WithUpdate(true)` to
create or update snapshots.

Package-only snapshot check (for harness snapshots, see below):

```go
func TestSnapshotPlain(t *testing.T) {
	// view := expected rendered output or golden string
	snapshot.AssertEqual(t, view,
		snapshot.WithSuffix("status"),
		snapshot.WithStripANSI(),
	)
}
```

Use `snapshot.WithTransform` with `bytes.ReplaceAll` (or similar) when you need to normalize unstable substrings before comparison.

Use `snapshot.RequireEqual` when a mismatch should stop the test immediately.
For harnesses, use the convenience methods:

```go
func TestHarnessSnapshots(t *testing.T) {
	// h := steep.NewHarness(t, model) or NewComponentHarness(...)
	h.AssertViewSnapshot(snapshot.WithSuffix("ansi"))
	h.AssertViewSnapshot(snapshot.WithStripANSI(), snapshot.WithSuffix("plain"))
	h.RequireViewSnapshot(snapshot.WithSuffix("ansi"))
	h.RequireViewSnapshot(snapshot.WithStripANSI(), snapshot.WithSuffix("plain"))
	h.AssertViewSnapshotNoANSI(snapshot.WithSuffix("plain"))
	h.RequireViewSnapshotNoANSI(snapshot.WithSuffix("plain"))
}
```

Use `WithSuffix` when a test writes more than one snapshot or when a subtest
needs a more descriptive file name. Use `WithStripANSI` and `WithStripPrivate`
when terminal styling, spinners, or private-use glyphs would otherwise make
snapshots unstable.
