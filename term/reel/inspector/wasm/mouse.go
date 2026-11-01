// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"image"
	"time"

	"github.com/lrstanley/x/term/reel"
	"github.com/lrstanley/x/term/reel/effects"
	"github.com/lrstanley/x/term/reel/vt"
)

// mouseSettle is how long after a mouse event [effects.Mouse], with its
// defaults, keeps moving: the idle pointer hides after a second and fades
// out over 250ms, outlasting a press's ripple and its glide, which waits
// for a press at most a glide. Between spans of it, the mouse holds still,
// so frames need not be drawn again as the playhead moves. TestMouse
// checks it.
const mouseSettle = 1250 * time.Millisecond

// mouseStep is the step of the mouse's [reel.Context]. Its frames are
// paused, drawn as of one moment, so any step draws them alike.
const mouseStep = 10 * time.Millisecond

// mouseSpans are the spans of time, in ms, the mouse moves in: from each
// mouse event of events' inputs, for mouseSettle, merged where they
// overlap.
func mouseSpans(events []reel.Event) [][2]float64 {
	var spans [][2]float64
	for _, ev := range events {
		in, ok := ev.(reel.Input)
		if !ok || !hasMouse(in.Data) {
			continue
		}
		start, end := ms(in.At()), ms(in.At()+mouseSettle)
		if n := len(spans); n > 0 && start <= spans[n-1][1] {
			spans[n-1][1] = end
			continue
		}
		spans = append(spans, [2]float64{start, end})
	}
	return spans
}

func hasMouse(data []byte) bool {
	for _, s := range vt.DecodeInput(data) {
		if s.Kind == vt.StrokeMouse {
			return true
		}
	}
	return false
}

// startMouse starts drawing the mouse over frames with the player's
// options, if on.
func (x *inspector) startMouse(on bool) error {
	x.mouse = nil
	if !on {
		return nil
	}
	rast, err := x.player.Rasterizer()
	if err != nil {
		return err
	}
	layout, err := x.player.Layout()
	if err != nil {
		return err
	}
	ctx := &reel.Context{Timeline: x.tl, Rasterizer: rast, Layout: layout, Step: mouseStep, Speed: 1}
	x.mouse, err = effects.Mouse().Start(ctx)
	return err
}

// withMouse returns img, the player's image, with the mouse drawn on it as
// of timeline offset at, or of the applied events if later.
func (x *inspector) withMouse(img *image.NRGBA, at time.Duration) *image.NRGBA {
	if x.mouse == nil {
		return img
	}
	f := x.player.Frame()
	x.mouse(reel.Frame{Image: img, Cells: &f, At: max(at, f.At), Delay: mouseStep, Paused: true}, func(out reel.Frame) {
		img = out.Image
	})
	return img
}
