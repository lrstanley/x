// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"fmt"
	"os"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/lrstanley/x/term/still/export"
	"github.com/lrstanley/x/term/vt"
	"github.com/lrstanley/x/term/vt/screen"
)

// frameBudget is one frame at the highest export frame rate. Capturing and
// encoding a frame must take less, or a fast burst of paints backs up.
const frameBudget = time.Second / export.MaxFrameRate

// reportBudget reports the benchmark's time per op as a share of frameBudget.
func reportBudget(b *testing.B) {
	b.ReportMetric(float64(b.Elapsed())/float64(b.N)/float64(frameBudget)*100, "%budget")
}

// iterTB runs a harness's cleanups at the end of one benchmark iteration.
type iterTB struct {
	testing.TB
	cleanups []func()
}

func (t *iterTB) Cleanup(f func()) { t.cleanups = append(t.cleanups, f) }

func (t *iterTB) done() {
	for i := len(t.cleanups) - 1; i >= 0; i-- {
		t.cleanups[i]()
	}
}

var benchmarkSizes = [][2]int{{80, 24}, {240, 72}}

// benchmarkModels are a plain terminal, which is almost all palette black and
// cheap to match to the GIF palette, and a themed one whose background is
// not a palette color, like most real color schemes.
var benchmarkModels = []struct {
	name  string
	model func(text string) tea.Model
}{
	{"plain", func(text string) tea.Model { return rootTestModel{text: text} }},
	{"themed", func(text string) tea.Model { return backgroundModel{rootTestModel{text: text}} }},
}

// BenchmarkExportCapture is the test goroutine's cost per recorded paint:
// rendering the committed screen into a recycled buffer, which the encoder
// releases once it has palettized it.
func BenchmarkExportCapture(b *testing.B) {
	for _, size := range benchmarkSizes {
		b.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(b *testing.B) {
			h := NewHarness(b, rootTestModel{}, WithWindowSize(size[0], size[1]), WithIgnoreEnv(true))
			var screens []*screen.Buffer
			var states []vt.Snapshot
			// Two different screens, alternated so every draw paints instead
			// of reusing the renderer's cached frame.
			for _, text := range []string{"a", "ab"} {
				h.Type(text[len(text)-1:]).Wait("text=" + text)
				screens = append(screens, h.emulator.term.Screen())
				states = append(states, h.emulator.term.Snapshot())
			}
			b.ReportAllocs()
			i := 0
			for b.Loop() {
				releaseNRGBA(copyCommitted(h, screens[i%2], states[i%2]))
				i++
			}
			reportBudget(b)
		})
	}
}

// BenchmarkExportEncode is the encoder's cost per recorded paint: a captured
// frame converted to the GIF palette and stored.
func BenchmarkExportEncode(b *testing.B) {
	for _, m := range benchmarkModels {
		for _, size := range benchmarkSizes {
			b.Run(fmt.Sprintf("%s/%dx%d", m.name, size[0], size[1]), func(b *testing.B) {
				h := NewHarness(b, m.model("encode"), WithWindowSize(size[0], size[1]), WithIgnoreEnv(true))
				h.Wait("text=encode")
				img := copyCommitted(h, h.emulator.term.Screen(), h.emulator.term.Snapshot())
				rec := export.NewPalettizedRecorder()
				b.ReportAllocs()
				for b.Loop() {
					if err := rec.AddFrame(img, time.Millisecond); err != nil {
						b.Fatal(err)
					}
				}
				reportBudget(b)
			})
		}
	}
}

// BenchmarkExportRecording records a test typing 20 keys, one paint each. The
// finalize-ms metric is how long [Harness.Close] waits for the encoder to
// catch up and write the GIF: the backlog left by frames it could not encode
// as fast as the test painted them.
func BenchmarkExportRecording(b *testing.B) {
	plain, themed := benchmarkModels[0].model, benchmarkModels[1].model
	cases := []struct {
		name  string
		model func(string) tea.Model
		size  [2]int
		opts  []Option
	}{
		{"plain/80x24", plain, benchmarkSizes[0], nil},
		{"themed/80x24", themed, benchmarkSizes[0], nil},
		{"themed/240x72", themed, benchmarkSizes[1], nil},
		{"themed-cards/80x24", themed, benchmarkSizes[0], []Option{WithExportIntro(1500 * time.Millisecond), WithExportOutro(1500 * time.Millisecond)}},
		{"themed-keys/80x24", themed, benchmarkSizes[0], []Option{WithExportKeys(1500 * time.Millisecond)}},
		{"themed-cards/240x72", themed, benchmarkSizes[1], []Option{WithExportIntro(1500 * time.Millisecond), WithExportOutro(1500 * time.Millisecond)}},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			dir := b.TempDir()
			var finalize time.Duration
			var size int64
			b.ReportAllocs()
			for b.Loop() {
				tb := &iterTB{TB: b}
				h := NewHarness(tb, tc.model(""), append([]Option{
					WithWindowSize(tc.size[0], tc.size[1]),
					WithExport(ExportOn, ExportTypeGIF),
					WithExportDir(dir),
					WithIgnoreEnv(true),
				}, tc.opts...)...)
				for range 20 {
					h.Type("x")
				}
				h.QuitProgram()
				h.WaitFinished()
				start := time.Now()
				if err := h.export.Finalize(false); err != nil {
					b.Fatal(err)
				}
				finalize += time.Since(start)
				st, err := os.Stat(h.export.gifPath)
				if err != nil {
					b.Fatal(err)
				}
				size = st.Size()
				tb.done()
			}
			b.ReportMetric(float64(finalize.Milliseconds())/float64(b.N), "finalize-ms")
			b.ReportMetric(float64(size), "gif-B")
		})
	}
}
