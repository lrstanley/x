# reel inspector

A static web page for inspecting `.cast` files that reel and steep write: open
one, scrub through its events on a timeline, and see the screen, terminal
state, colors, escape sequences, and annotations after any of them.

The page holds little logic of its own. [`wasm`](wasm) is reel compiled to
WebAssembly: it reads the cast with `reel.ReadTimeline`, replays it with
`reel.Player` (the replay `reel.Export` uses), and draws frames with reel's
renderer, so a frame here matches the frame an export writes; a cast's mouse
input is drawn over it by `effects.Mouse`, as of the playhead. It runs in a
Web Worker ([`public/reel-worker.js`](public/reel-worker.js)), so a long
replay never blocks the page.

When a recording has sources (`reel.WithSource`, or steep's `STEEP_SOURCE=1`),
marks and annotations show the line of the test behind them, and failures in
the Recording tab say where they were reported.

Casts never leave the browser. The last 20 opened are kept in IndexedDB, and
the address names the open cast (`?cast=<file name>`; opening another file
of the same name replaces it) and how it is viewed: the event (`#e=123`), the event list's filters, the timeline's
hidden lanes and axis, the playback speed, and the inspector's tab. A reload
or Back returns to it, and each tab keeps its own. These addresses only work
in the browser that stored the cast.
`?url=https://…/test.cast` opens a cast another host serves, if it allows
cross-origin requests.

## Developing

Needs Go and [Bun](https://bun.sh).

```sh
bun install
bun run dev        # builds the assets below, then serves on :3000
bun run build      # builds the assets and the static site into .output/public
bun run preview    # serves .output/public on :3000
bun run typecheck  # the app, and the worker against its typed protocol
bun run test       # the frame scheduler, worker client, and timeline geometry
```

The site's generated assets are not committed:

- `bun run wasm` builds `public/reel.wasm`, after changing reel or
  [`wasm`](wasm). Its tests run natively: `cd wasm && go test ./...`.
- `bun run samples` records the sample casts the landing page offers into
  `public/samples`, by running `TestInspectorSamples` in steep's
  [simple example](../../steep/_examples/simple/samples_test.go), with
  sources recorded.

`bun run assets` runs both.

## Deploying

`.output/public` is the whole site; any static host serves it. Set
`NUXT_APP_BASE_URL` when building for a subpath, such as
`NUXT_APP_BASE_URL=/x/ bun run build` for a GitHub Pages project site.

`reel.wasm` is about 21 MB, 9 MB gzipped: most of it is the libghostty-vt
emulator (itself wasm2go output) and reel's embedded fonts.

## Known limits

- Seeking backward replays the cast from the nearest checkpoint before it.
  For a cast that takes long to replay, the worker keeps up to 8 terminals
  parked through it (about 6 MB each), built while it is idle, so on a 4 MB
  cast a step back takes about 40-130 ms rather than 400 ms. Seeking forward
  only applies the events in between.
- Frames are drawn at the size the page shows them, so a small panel draws
  fewer pixels; the cells, needed only to inspect them, are fetched on hover.
- The timeline draws every lane on one canvas and only the visible range, so
  large casts stay smooth, but the event list's labels are summaries: the
  inspector has the full bytes and tokens.
