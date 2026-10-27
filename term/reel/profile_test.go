// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"maps"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/reel/sink"
)

func TestProfileMerge(t *testing.T) {
	t.Parallel()
	rast := &fakeRasterizer{}
	base := Profile{
		Outputs: Files("a.gif"),
		Render:  []render.Option{render.WithScale(2)},
		Encode:  []sink.Option{sink.WithLoopCount(1)},
		FPS:     10,
		Speed:   2,
		Hold:    time.Second,
		Title:   "base",
		Meta:    1,
		Vars:    map[string]string{"a": "1", "b": "2"},
	}
	vars := maps.Clone(base.Vars)
	got := base.Merge(Profile{
		Outputs:    Files("b.mp4", "c.cast"),
		Rasterizer: rast,
		FPS:        50,
		Hold:       -1,
		Meta:       "m",
		Vars:       map[string]string{"b": "3", "c": "4"},
	})
	want := base
	want.Outputs, want.Rasterizer, want.FPS, want.Hold, want.Meta = Files("b.mp4", "c.cast"), rast, 50, -1, "m"
	want.Vars = map[string]string{"a": "1", "b": "3", "c": "4"}
	// Render and Encode hold funcs, which DeepEqual only matches when nil.
	if len(got.Render) != 1 || len(got.Encode) != 1 {
		t.Fatalf("unset slices were replaced: %+v", got)
	}
	got.Render, got.Encode, want.Render, want.Encode = nil, nil, nil, nil
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Merge =\n%+v\nwant\n%+v", got, want)
	}
	if !maps.Equal(base.Vars, vars) {
		t.Fatal("Merge changed the receiver's Vars")
	}
	if got = (Profile{}).Merge(Profile{Vars: map[string]string{"x": "y"}}); got.Vars["x"] != "y" {
		t.Fatalf("Merge into nil Vars = %v", got.Vars)
	}
	// A set slice replaces, an empty but non-nil one too.
	if got = base.Merge(Profile{Outputs: []File{}}); len(got.Outputs) != 0 {
		t.Fatalf("Outputs = %v, want replaced by the empty slice", got.Outputs)
	}
}

func TestProfileSettings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		p    Profile
		want settings
	}{
		{Profile{}, settings{step: 50 * ms, speed: 1, hold: time.Second}},
		{Profile{FPS: 30, Speed: 1.5, Hold: 2 * time.Second}, settings{step: 30 * ms, speed: 1.5, hold: 2 * time.Second}},
		{Profile{FPS: 100, Hold: -1}, settings{step: 10 * ms, speed: 1}},
		{Profile{FPS: 1}, settings{step: time.Second, speed: 1, hold: time.Second}},
	} {
		tc.p.Outputs = Files("x.gif")
		got, err := tc.p.settings()
		if err != nil || got != tc.want {
			t.Errorf("%+v: settings = %+v, %v; want %+v", tc.p, got, err, tc.want)
		}
	}
}

func TestExpandPath(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("x", 3600))
	vars, err := pathVars(map[string]string{"name": "n/x"}, start)
	if err != nil {
		t.Fatal(err)
	}
	vars[varDuration] = "4.2s"
	got, err := expandPath("out/{name}_{duration}_{timestamp}.gif", vars)
	if err != nil || got != "out/n/x_4.2s_20260102T020405.gif" {
		t.Fatalf("expandPath = %q, %v", got, err)
	}
	if got, err = expandPath("plain.gif", vars); err != nil || got != "plain.gif" {
		t.Fatalf("expandPath = %q, %v", got, err)
	}
	for path, want := range map[string]string{
		"{nope}.gif":     "unknown variable {nope}",
		"{}.gif":         "unknown variable {}",
		"{name.gif":      "unclosed {",
		"{mark}.gif":     "{mark} is only set in an output with Marks",
		"{duration}.gif": "{duration} is only known once a recording ends",
	} {
		delete(vars, varDuration)
		if _, err = expandPath(path, vars); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("expandPath(%q) = %v, want an error containing %q", path, err, want)
		}
	}
	if vars, err = pathVars(nil, time.Time{}); err != nil || vars[varTimestamp] == "" {
		t.Fatalf("pathVars without a start = %v, %v; want now", vars, err)
	}
}
