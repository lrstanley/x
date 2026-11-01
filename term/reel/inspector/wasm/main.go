// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build js && wasm

// Command wasm is reel compiled for the browser: it reads a cast, replays
// it, and draws its frames with reel itself, for the reel inspector. It sets
// globalThis.reel to an object of functions, each returning an object with
// either an "error" string or its result:
//
//	load(data: Uint8Array, scale?: number) -> {json}  // the cast's header and events
//	frame(i: number, options: string) -> {json, width, height, pixels}
//	detail(i: number) -> {json}                       // an event's bytes and tokens
//	unload() -> {}                                  // releases the current player
//	prepare(ms: number) -> {more}                     // idle work: checkpoints for seeking back
//
// frame(i) shows the terminal just after event i, or before every event when
// i is -1. pixels is the frame's RGBA image, when image is true, with the
// mouse as of the options' at ms when the display has it, and the JSON has
// the screen's cells when cells is true.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"syscall/js"
	"time"
)

var current *inspector

func main() {
	js.Global().Set("reel", js.ValueOf(map[string]any{
		"load":    fn(loadJS),
		"frame":   fn(frameJS),
		"detail":  fn(detailJS),
		"unload":  fn(unloadJS),
		"prepare": fn(prepareJS),
	}))
	select {}
}

// fn wraps f, turning its error or panic into an "error" field.
func fn(f func(args []js.Value) (map[string]any, error)) js.Func {
	return js.FuncOf(func(_ js.Value, args []js.Value) (out any) {
		defer func() {
			if r := recover(); r != nil {
				out = js.ValueOf(map[string]any{wordError: fmt.Sprint("panic: ", r)})
			}
		}()
		res, err := f(args)
		if err != nil {
			return js.ValueOf(map[string]any{wordError: err.Error()})
		}
		return js.ValueOf(res)
	})
}

func jsonOf(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return map[string]any{"json": string(b)}, nil
}

func loadJS(args []js.Value) (map[string]any, error) {
	data := make([]byte, args[0].Get("length").Int())
	js.CopyBytesToGo(data, args[0])
	scale := 1.0
	if len(args) > 1 && args[1].Type() == js.TypeNumber {
		scale = args[1].Float()
	}
	x, err := load(context.Background(), data, scale)
	if err != nil {
		return nil, err
	}
	if current != nil {
		_ = current.player.Close()
	}
	current = x
	return jsonOf(x.summary())
}

func frameJS(args []js.Value) (map[string]any, error) {
	if current == nil {
		return nil, errors.New("no cast loaded")
	}
	var opts struct {
		Image   bool    `json:"image"`
		Cells   bool    `json:"cells"`
		Scale   float64 `json:"scale"`
		Display display `json:"display"`
		// At is the time in ms to draw the mouse as of, from the event's on.
		At float64 `json:"at"`
	}
	if err := json.Unmarshal([]byte(args[1].String()), &opts); err != nil {
		return nil, err
	}
	if opts.Scale <= 0 {
		return nil, fmt.Errorf("scale %v is not positive", opts.Scale)
	}
	if opts.Scale != current.scale || opts.Display != current.display {
		if err := current.configure(opts.Scale, opts.Display); err != nil {
			return nil, err
		}
	}
	if err := current.seek(args[0].Int()); err != nil {
		return nil, err
	}
	out, err := jsonOf(current.frame(opts.Cells))
	if err != nil || !opts.Image {
		return out, err
	}
	img, err := current.player.Image()
	if err != nil {
		return nil, err
	}
	img = current.withMouse(img, time.Duration(opts.At*float64(time.Millisecond)))
	pixels := js.Global().Get("Uint8Array").New(len(img.Pix))
	js.CopyBytesToJS(pixels, img.Pix)
	out["width"], out["height"], out["pixels"] = img.Rect.Dx(), img.Rect.Dy(), pixels
	return out, nil
}

func detailJS(args []js.Value) (map[string]any, error) {
	if current == nil {
		return nil, errors.New("no cast loaded")
	}
	d, err := current.detail(args[0].Int())
	if err != nil {
		return nil, err
	}
	return jsonOf(d)
}

func unloadJS(_ []js.Value) (map[string]any, error) {
	x := current
	current = nil
	if x != nil {
		return map[string]any{}, x.player.Close()
	}
	return map[string]any{}, nil
}

func prepareJS(args []js.Value) (map[string]any, error) {
	if current == nil {
		return map[string]any{"more": false}, nil
	}
	more, err := current.prepare(time.Duration(args[0].Float() * float64(time.Millisecond)))
	return map[string]any{"more": more}, err
}
