// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"bytes"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lrstanley/x/term/reel/sink"
)

func TestParseFile(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		raw   string
		path  string
		cast  CastVersion
		marks []string
		opts  int
	}{
		{raw: "out.gif", path: "out.gif"},
		{raw: "./artifacts/{name}_{status}.gif", path: "./artifacts/{name}_{status}.gif"},
		{raw: "file://./foo.cast?version=2", path: "./foo.cast", cast: CastV2},
		{raw: "FILE:///tmp/a%20b.cast?version=3", path: "/tmp/a b.cast", cast: CastV3},
		{raw: "a%20b.gif", path: "a%20b.gif"},
		{raw: "file:///C:/out/x.gif", path: "C:/out/x.gif"},
		{raw: "C://out.gif", path: "C://out.gif"},
		{raw: `C:\out\x.gif?loop=once`, path: `C:\out\x.gif`, opts: 1},
		{raw: "./out.gif?max-bytes=5MB&loop=once&max-frames=10&optimize=frames&optimize=dirty&background=%23fff", path: "./out.gif", opts: 5},
		{raw: "x.png?optimize=none", path: "x.png", opts: 1},
		{raw: "x.mp4?quality=80&full-chroma=true&background=%23102030", path: "x.mp4", opts: 3},
		{raw: "x.webp?quality=100&loop=3", path: "x.webp", opts: 2},
		{raw: "x.webm?quality=0", path: "x.webm", opts: 1},
		{raw: "shot-{mark}.png?marks=a&marks=b", path: "shot-{mark}.png", marks: []string{"a", "b"}},
		{raw: "x.gif?", path: "x.gif"},
	} {
		f, err := ParseFile(tc.raw)
		if err != nil {
			t.Errorf("%q: %v", tc.raw, err)
			continue
		}
		if f.Path != tc.path || f.Cast != tc.cast || !slices.Equal(f.Marks, tc.marks) || len(f.Options) != tc.opts {
			t.Errorf("%q: got path %q cast %d marks %q %d options, want %q %d %q %d",
				tc.raw, f.Path, f.Cast, f.Marks, len(f.Options), tc.path, tc.cast, tc.marks, tc.opts)
		}
	}
}

func TestParseFileErrors(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]string{
		"":                                    "is empty",
		"?loop=once":                          "is empty",
		"file://":                             "is empty",
		"s3://bucket/x.gif":                   `unsupported scheme "s3"`,
		"x.txt":                               "unknown format",
		"x":                                   "unknown format",
		"x.gif?fps=30":                        `unknown parameter "fps"`,
		"x.gif?quality=80":                    `"quality" does not apply to .gif`,
		"x.mp4?version=2":                     `"version" does not apply to .mp4`,
		"x.cast?loop=once":                    `"loop" does not apply to .cast`,
		"x-{mark}.cast?marks=a":               `"marks" does not apply to .cast`,
		"x.webp?full-chroma=1":                `"full-chroma" does not apply to .webp`,
		"x.png?background=%23fff":             `"background" does not apply to .png`,
		"x.mp4?max-bytes=1":                   `"max-bytes" does not apply to .mp4`,
		"x.gif?loop=once&loop=forever":        `"loop" is repeated`,
		"x.cast?version=1":                    `version: "1" is not 2 or 3`,
		"x.gif?loop=0":                        "loop:",
		"x.gif?loop=65536":                    "loop:",
		"x.gif?max-frames=-1":                 "max-frames:",
		"x.gif?max-bytes=5XB":                 "max-bytes:",
		"x.mp4?quality=101":                   "quality:",
		"x.mp4?full-chroma=maybe":             "full-chroma:",
		"x.gif?background=red":                "background:",
		"x.gif?background=%23ff":              "background:",
		"x.gif?optimize=fast":                 "optimize:",
		"x.gif?optimize=none&optimize=frames": "none cannot be combined",
		"x.png?marks=a":                       "must hold {mark}",
		"x-{mark}.png?marks=":                 "label is empty",
		"x.gif?a=%zz":                         "invalid URL escape",
		"file://%zz.gif":                      "invalid URL escape",
	} {
		_, err := ParseFile(raw)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want an error containing %q", raw, err, want)
		}
	}
}

func TestMustFiles(t *testing.T) {
	t.Parallel()
	files := MustFiles("a.gif?loop=once", "b.cast?version=3")
	if len(files) != 2 || files[0].Path != "a.gif" || len(files[0].Options) != 1 || files[1].Cast != CastV3 {
		t.Fatalf("MustFiles = %+v", files)
	}
	defer func() {
		if err, ok := recover().(error); !ok || !strings.Contains(err.Error(), `unknown parameter "fps"`) {
			t.Fatalf("recovered %v, want the parse error", err)
		}
	}()
	MustFiles("a.gif", "b.gif?fps=30")
}

func TestParseBytes(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]int64{
		"0": 0, "512": 512, "512B": 512, "5MB": 5_000_000, "5mb": 5_000_000,
		"1.5KiB": 1536, "2 GiB": 2 << 30, "1kb": 1000, "3MiB": 3 << 20,
		"4.1MB": 4_100_000, "32.3KB": 32_300, "0.0": 0, "1.6": 2,
	} {
		if got, err := parseBytes(raw); err != nil || got != want {
			t.Errorf("%q: got %d, %v, want %d", raw, got, err, want)
		}
	}
	for _, raw := range []string{"", "MB", "-1", "1.2.3", "5 XB", "1e3", "99999999999GB", "0.4", "9223372036854775807", "8589934592GiB"} {
		if got, err := parseBytes(raw); err == nil {
			t.Errorf("%q: got %d, want an error", raw, got)
		}
	}
}

func TestParseHexColor(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]color.NRGBA{
		"#fff":    {0xff, 0xff, 0xff, 0xff},
		"#1a2":    {0x11, 0xaa, 0x22, 0xff},
		"#102030": {0x10, 0x20, 0x30, 0xff},
	} {
		if got, err := parseHexColor(raw); err != nil || got != want {
			t.Errorf("%q: got %v, %v, want %v", raw, got, err, want)
		}
	}
}

// TestParseFileExport checks that a parsed URL's options reach the file it
// writes.
func TestParseFileExport(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var outputs []File
	for _, raw := range []string{
		filepath.Join(dir, "{name}.gif") + "?loop=once&max-frames=1",
		"file://" + filepath.ToSlash(filepath.Join(dir, "{name}.cast")) + "?version=3",
		filepath.Join(dir, "{name}-{mark}.png") + "?marks=shot",
	} {
		f, err := ParseFile(raw)
		if err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, f)
	}
	p := Profile{Outputs: outputs, Encode: []sink.Option{sink.WithLoopCount(sink.LoopForever)}, Vars: map[string]string{"name": "demo"}}
	if err := Export(t.Context(), demoTimeline(), p); err != nil {
		t.Fatal(err)
	}
	g := readGIF(t, filepath.Join(dir, "demo.gif"))
	if g.LoopCount != sink.LoopOnce || len(g.Image) != 1 {
		t.Errorf("gif loop %d with %d frames, want the URL's loop once and 1 frame", g.LoopCount, len(g.Image))
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "demo.cast")); !bytes.HasPrefix(data, []byte(`{"version":3`)) {
		t.Error("cast is not the URL's v3")
	}
	readPNG(t, filepath.Join(dir, "demo-shot.png"))
}
