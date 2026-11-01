# reel

`reel` records terminals and renders recordings without writing Go. It runs
[VHS](https://github.com/charmbracelet/vhs) tapes, records commands, drives
programs for agents and tests, and turns asciicast files into GIFs, videos,
and stills, with reel's renderer and effects.

```console
go install github.com/lrstanley/x/term/reel/cmd/reel@latest
```

Run `reel --help`, or `reel <command> --help`, for every flag. Commands that
list things (`session list`, `themes`) print a table on a terminal, and
tab-separated rows without headers when piped; `--json` prints JSON.

## Commands

| Command | What it does |
| --- | --- |
| `reel run demo.tape` | Run VHS tapes and write their outputs. |
| `reel test tests/` | Run tapes as tests: a failed `Wait` or `Snapshot` fails the tape. |
| `reel rec -o demo.cast -- htop` | Record a command, or `$SHELL`, in this terminal. |
| `reel session start -- htop` | Drive a program in a detached, recorded terminal (see [Sessions](#sessions)). |
| `reel export in.cast -o out.gif` | Render an asciicast file (v2 or v3, from reel, asciinema, or others). |
| `reel still in.cast --mark done -o shot.png` | Render one moment as `.png`, `.snap` (text), or `.json`. |
| `reel cut in.cast --from a --to b -o out.cast` | Cut a file to the window between two marks. |
| `reel convert in.cast -o out.cast --cast-version 3` | Convert between asciicast v2 and v3. |
| `reel attach typescript --timing timing -o out.gif` | Render a `script(1)` recording. |
| `reel play in.cast --speed 2 --idle-limit 1s` | Replay a file in this terminal. It plays in reel's emulator, drawn on the alternate screen, so the recording cannot change this terminal; `q` or `ctrl+c` stops it. |
| `reel info in.cast [--json]` | Print the size, duration, events, marks, and effects. |
| `reel themes [TEXT] [--dark] [--hue blue] [--preview]` | List the named themes. |
| `reel fonts` | List the font families reel can draw with. |

```console
$ reel run demo.tape
demo.tape: wrote demo.gif
demo.tape: wrote demo.cast

$ reel run demos/*.tape -o 'out/{tape}.gif' --theme "Catppuccin Mocha" --keys
$ reel run untrusted.tape --deny Env,Source,Require --skip Screenshot
$ reel export in.cast -o out.gif -o out.mp4 --between login,logout --intro 1s --pause
$ reel rec -o demo.cast -o demo.gif --keys    # ctrl+] m drops a mark while recording
```

## Outputs

`-o` is repeatable, and its extension picks the format: `.gif`, `.mp4`,
`.webm`, `.webp`, `.cast`, `.snap` (the screen's text), `.json` (the screen's
cells), or `.png` with `?marks=`. Options go in the query, such as
`out.gif?max-bytes=5MB&loop=once` or `shot-{mark}.png?match=step:*`. Paths take
`{duration}`, `{timestamp}`, `{tape}` (for `run`), and `--var NAME=VALUE`.

For `run`, `-o` replaces the tape's `Output` commands; its `Screenshot`s are kept.

A `.cast` holds the raw recording, so it cannot be written with effects that
hide or cut what it shows (`--redact*`, `--hide`, or a tape's `Hide` or
`Redact`). Write it in a separate run.

## Configuration

There is no configuration file: a tape configures a demo, and flags and
environment variables configure a run. For `run`, a setting is taken from
the first of:

1. a flag, such as `--theme`
2. its environment variable, such as `REEL_THEME`
3. the tape's `Set`, such as `Set Theme`
4. the default (VHS's for tapes, reel's otherwise)

| Flag | Environment | Tape setting |
| --- | --- | --- |
| `--theme NAME\|FILE\|JSON` | `REEL_THEME` | `Theme` |
| `--font FAMILY[,FAMILY...]` | `REEL_FONT` | `FontFamily` |
| `--font-size PX` | `REEL_FONT_SIZE` | `FontSize` |
| `--letter-spacing PX` | | `LetterSpacing` |
| `--line-height X` | | `LineHeight` |
| `--padding PX` | `REEL_PADDING` | `Padding` |
| `--margin PX` | `REEL_MARGIN` | `Margin` |
| `--margin-fill #RRGGBB` | `REEL_MARGIN_FILL` | `MarginFill` |
| `--radius PX` | `REEL_RADIUS` | `BorderRadius` |
| `--[no-]chrome` | `REEL_CHROME` | `WindowBar` |
| `--fps N` | `REEL_FPS` | `Framerate` |
| `--speed X` | `REEL_SPEED` | `PlaybackSpeed` |
| `--title TEXT` | | `Title` |
| `--[no-]keys` | `REEL_KEYS` | `KeyOverlay` |
| `--[no-]toasts` | `REEL_TOASTS` | `Toasts` |
| `--[no-]scrollbar`, `--[no-]prompt-marks`, `--[no-]link-underline`, `--[no-]bell` | | `Scrollbar`, `PromptMarks`, `LinkUnderline`, `Bell` |

Themes are the [bubbletint](https://github.com/lrstanley/bubbletint) catalog,
named in any case (`"Catppuccin Mocha"` or `catppuccin_mocha`), or xterm.js
theme JSON, inline or in a file, as VHS takes it.

Other environment variables: `REEL_SESSION` (the session to drive) and
`REEL_RUNTIME_DIR` (where session sockets live; defaults to
`$XDG_RUNTIME_DIR/reel`).

### Effects

These flags add effects to `run`, `export`, `attach`, `rec`, and session exports:

```console
--intro 1s --intro-line "My demo"   # a title card
--outro 2s --outro-line "Thanks"
--pause                             # a pause at the end, with a badge
--pause-at build:done               # a pause at a mark
--hold-last 2s
--mouse                             # the pointer, clicks, and drags
--hide setup:start,setup:done       # cut a window between marks
--highlight 'rows=2-4;dim=0.4;between=a,b'
--zoom 're=ERROR.*;scale=2'
--redact 'env=GITHUB_TOKEN;style=blur'
--redact-env GITHUB_TOKEN --redact-regexp 'sk-[A-Za-z0-9]{20,}'
```

A `SPEC` is `REGION[;KEY=VALUE]...`. `REGION` is one of `row=Y`,
`rows=Y0-Y1`, `cells=X0,Y0,X1,Y1`, `text=TEXT`, `re=PATTERN`, or `env=NAME`.
Every effect takes `between=FROM,TO` (marks; either may be empty). Highlight
also takes `ease=`, `dim=`, `outline=#RRGGBB[:PX]`, and `padding=`; zoom takes
`ease=`, `scale=`, and `anchor=X,Y`; redact takes `style=blur|block|replace:C`.
Values cannot contain `;`.

## Tapes

`reel run` reads VHS tapes: every VHS command and setting, except
`LoopOffset`, frame-directory and `.txt`/`.ascii` outputs, and the `ksh`
shell. `WindowBar` styles are drawn as `Colorful`, and `WindowBarSize` and
`CursorBlink false` are ignored, each with a warning.

reel adds:

| Addition | Meaning |
| --- | --- |
| `Set Columns N`, `Set Rows N` | The screen in cells, instead of fitting `Width` and `Height`. |
| `Set Title`, `Scrollbar`, `PromptMarks`, `LinkUnderline`, `Bell`, `KeyOverlay`, `Toasts` | See [Configuration](#configuration). |
| `Mark LABEL` | A mark, for `between=`, `--pause-at`, `--hide`, or `?marks=`. |
| `Home` | The Home key. |
| `Wait "text"` | `Wait` for a string as is, as well as a `/regexp/`. |
| `Highlight "SPEC"`, `Zoom "SPEC"`, `Redact "SPEC"` | Effects, as their flags take them. |
| `Snapshot NAME` (`reel test` only) | Compare the screen to `testdata/<tape>/NAME.snap`. |

`--deny` and `--skip` fail or ignore any of these too.

## Tests

`reel test` runs tapes without writing their outputs. A tape fails at the
first command that does: a `Wait` that times out (with the screen it last
saw), a `Snapshot` that differs, or any other error.

```console
$ reel test tests/ --artifacts out/
ok    tests/login.tape	210ms
FAIL  tests/settings.tape	1.2s
      tape: tests/settings.tape:9:1: the screen differs from Snapshot after in tests/testdata/settings/after.snap (- want, + got):
        row 2:
        - Theme:  dark
        + Theme:  light
1 passed, 1 failed

$ reel test tests/ --update    # write the golden files
```

`--artifacts DIR` writes a `.cast` and `.gif` of each failed tape.

## Sessions

`reel session` (or `reel s`) runs a program in a detached terminal that each
command drives, so agents and scripts can test any TUI or CLI without
writing code. Everything is recorded, so the session can be exported at any
point.

```console
$ reel s start --size 100x30 -- htop
htop
$ reel s wait --text "Load average"
$ reel s key F6 down down enter
$ reel s screen                         # the screen's text
$ reel s screen -f json                 # its cells, styles, and cursor
$ reel s still -o shot.png              # an image of it
$ reel s mark sorted
$ reel s do - <<'EOF'
Type "q"
Wait+Screen /\$ $/
EOF
$ reel s stop -o demo.gif -o demo.cast --keys
```

- With one session running, it is the one driven. Otherwise, name it with
  `-s NAME` or `REEL_SESSION`.
- `wait` takes `--text`, `--regexp`, `--gone`, or `--idle DUR`, plus
  `--cursor-line` and `--timeout` (15s by default).
- `--json` prints each response, and each error, as JSON.
- A session stays up for `--linger` (10m by default) after its program exits,
  so its last screen and recording can still be read.

Each session is a background `reel` process listening on a Unix socket in a
directory only you can open (`REEL_RUNTIME_DIR`, `$XDG_RUNTIME_DIR/reel`, or
a directory of yours in the temporary directory).

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Success. |
| 1 | An error. |
| 3 | A `session wait` timed out, or a `reel test` tape failed. |
| 4 | No such session. |
| 80 | Invalid command-line usage. |
