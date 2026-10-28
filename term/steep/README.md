<img width="1176" height="280" alt="banner" src="https://github.com/user-attachments/assets/b7950fed-b8f8-4155-852f-4333e5ef1b69" />

---

steep is a test harness for [Bubble Tea](https://github.com/charmbracelet/bubbletea)
programs and components. It runs the program in an emulated terminal (a
[reel](../reel) session), drives it with keys, text, mouse events, and
resizes, and asserts on what it draws.

```go
func TestModel(t *testing.T) {
	steep.NewHarness(t, newModel()).
		Wait("Tokyo").
		Key("down").
		AssertSnapshot()
}
```

## Recording

Everything the program draws is kept in the session's timeline, and nothing is
written unless an export policy says so. `WithRecording` takes a `reel.Profile`:
what to write, how to draw it, and the effects to apply.

```go
h := steep.NewHarness(t, newModel(),
	steep.WithRecording(reel.Profile{
		Outputs: reel.Files("{name}_{status}.gif", "{name}_{status}.cast"),
		Effects: []reel.Effect{
			steep.Logo(2 * time.Second),
			effects.Keys(time.Second),
			effects.Pause(),
			steep.Outro(2 * time.Second),
		},
	}),
	steep.WithExportOnFailure(), // or steep.WithExportAlways()
)
```

The export runs once, when the harness closes. `effects.Pause()` pauses for a
second on the last frame, with a pause badge. For a failed test, `steep.Outro`
stretches that pause to at least 3s, then shows a FAIL card listing the
failures. Files go to `t.ArtifactDir()` (kept with `go test -artifacts`),
or to `WithArtifacts(dir)`. The `.cast` file is the recording itself:
exporting it again with `reel.Export`, the same profile, and the same
`steep.Result` writes the same GIF.

Any test can be recorded without changing it:

```sh
STEEP_EXPORT_MODE=on STEEP_EXPORT_TYPE=gif,mp4 go test -artifacts ./...
```

| Variable | Sets |
| --- | --- |
| `STEEP_EXPORT_MODE` | `on`, `failure`, or `off` |
| `STEEP_EXPORT_TYPE` | outputs: `gif`, `mp4`, `webm`, `webp`, `cast`; stills: `png`, `snapshot`; or `all` |
| `STEEP_EXPORT_DIR` | the artifacts directory |
| `STEEP_EXPORT_FPS`, `STEEP_EXPORT_SPEED` | the profile's timing |
| `STEEP_EXPORT_QUALITY`, `STEEP_EXPORT_MAX_FRAMES`, `STEEP_EXPORT_MAX_BYTES`, `STEEP_EXPORT_OPTIMIZE` | encoder options |
| `STEEP_RENDERER_SCALE`, `STEEP_RENDERER_FONT_FAMILY` | renderer options |

See [`_examples/reel-showcase`](_examples/reel-showcase/main_test.go) for a
complete example.
