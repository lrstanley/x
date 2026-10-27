// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel/internal/testutil"
)

func openSession(t *testing.T, cols, rows int) *Session {
	t.Helper()
	s, err := Open(t.Context(), cols, rows)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// TestRecordMatchesExport checks that recording a session while it runs
// writes what exporting its timeline afterwards does.
func TestRecordMatchesExport(t *testing.T) {
	t.Parallel()
	s := openSession(t, 12, 2)
	dir := t.TempDir()
	profile := func(name string) Profile {
		return Profile{
			Outputs: []File{
				{Path: filepath.Join(dir, name+".gif")},
				{Path: filepath.Join(dir, name+".cast")},
				{Path: filepath.Join(dir, name+"-{mark}.png"), Marks: []string{"half"}},
			},
			Rasterizer: testutil.Rasterizer{},
			FPS:        100,
		}
	}
	rec, err := Record(t.Context(), s, profile("record"))
	if err != nil {
		t.Fatal(err)
	}
	for i := range 6 {
		if _, err = fmt.Fprintf(s, "%c", 'a'+i); err != nil {
			t.Fatal(err)
		}
		if i == 3 {
			if err = s.Mark("half"); err != nil {
				t.Fatal(err)
			}
		}
		time.Sleep(15 * ms)
	}
	if err = s.Resize(10, 2); err != nil {
		t.Fatal(err)
	}
	if err = rec.Stop(); err != nil {
		t.Fatal(err)
	}
	if err = rec.Stop(); err != nil {
		t.Fatalf("second Stop = %v", err)
	}
	if err = Export(t.Context(), s.Timeline(), profile("export")); err != nil {
		t.Fatal(err)
	}
	for _, ext := range []string{".gif", ".cast", "-half.png"} {
		a, aerr := os.ReadFile(filepath.Join(dir, "record"+ext))
		b, berr := os.ReadFile(filepath.Join(dir, "export"+ext))
		if aerr != nil || berr != nil || len(a) == 0 || !bytes.Equal(a, b) {
			t.Errorf("%s: recorded and exported files differ (%v, %v)", ext, aerr, berr)
		}
	}
}

// TestRecordConcurrentWrites writes from several goroutines while recording,
// for the race detector.
func TestRecordConcurrentWrites(t *testing.T) {
	t.Parallel()
	s := openSession(t, 20, 4)
	var frames []Frame
	rec, err := Record(t.Context(), s, Profile{
		Outputs:    Files(filepath.Join(t.TempDir(), "x.gif")),
		Rasterizer: testutil.Rasterizer{},
		Effects:    []Effect{recordFrames(&frames)},
	})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for g := range 4 {
		wg.Go(func() {
			for i := range 25 {
				_, _ = fmt.Fprintf(s, "\x1b[%d;1H%d:%d", g+1, g, i)
				_ = s.Focus(i%2 == 0)
			}
		})
	}
	wg.Wait()
	if err = rec.Stop(); err != nil {
		t.Fatal(err)
	}
	if len(frames) == 0 || !strings.Contains(frames[len(frames)-1].Cells.Screen.Text(), "3:24") {
		t.Fatalf("recorded %d frames, want the last to show every write", len(frames))
	}
}

func TestRecordCancel(t *testing.T) {
	t.Parallel()
	s := openSession(t, 4, 1)
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	rec, err := Record(ctx, s, Profile{Outputs: Files(filepath.Join(dir, "x.gif"), filepath.Join(dir, "x.cast")), Rasterizer: testutil.Rasterizer{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Write([]byte("hi")); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err = rec.Stop(); !errors.Is(err, context.Canceled) {
		t.Fatalf("Stop = %v, want context.Canceled", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a canceled recording left %d files", len(entries))
	}
}

func TestRecordErrors(t *testing.T) {
	t.Parallel()
	s := openSession(t, 4, 1)
	dir := t.TempDir()
	_, err := Record(t.Context(), s, Profile{Outputs: Files(filepath.Join(dir, "{duration}.gif"))})
	if err == nil || !strings.Contains(err.Error(), "{duration} is only known once a recording ends") {
		t.Fatalf("Record with {duration} = %v", err)
	}

	// A still whose mark never comes is an error, but the rest is written.
	rec, err := Record(t.Context(), s, Profile{
		Outputs: []File{
			{Path: filepath.Join(dir, "x.gif")},
			{Path: filepath.Join(dir, "{mark}.png"), Marks: []string{"never"}},
		},
		Rasterizer: testutil.Rasterizer{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Write([]byte("hi")); err != nil {
		t.Fatal(err)
	}
	if err = rec.Stop(); !errors.Is(err, ErrNoMark) {
		t.Fatalf("Stop = %v, want ErrNoMark", err)
	}
	if _, err = os.Stat(filepath.Join(dir, "x.gif")); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dir, "never.png")); !os.IsNotExist(err) {
		t.Fatalf("the still of a missing mark was written: %v", err)
	}
}

func TestTimelineUpdated(t *testing.T) {
	t.Parallel()
	tl := newTimeline(Header{Cols: 1, Rows: 1})
	a := tl.Updated()
	if tl.Updated() != a {
		t.Fatal("Updated returned another channel before an append")
	}
	select {
	case <-a:
		t.Fatal("Updated closed before an append")
	default:
	}
	tl.append(outAt(0, "x"))
	select {
	case <-a:
	default:
		t.Fatal("Updated was not closed by an append")
	}
	if b := tl.Updated(); b == a {
		t.Fatal("Updated returned a closed channel after the append")
	}
}
