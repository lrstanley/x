// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"context"
	"errors"
	"image"
	"slices"
	"time"

	"github.com/lrstanley/x/term/reel/render"
)

// Player steps through a timeline's events in a terminal, replaying them as
// [Export] does, so a viewer can look at the terminal after any event.
// Seeking forward applies the events in between; seeking back replays the
// timeline from its first event, or from a checkpoint [Player.Prepare]
// made. A Player is not safe for concurrent use. Close it when done.
type Player struct {
	ctx    context.Context
	header Header
	events []Event
	opts   []render.Option

	r    *replay
	rend *render.Renderer
	img  *image.NRGBA

	// Backward seeks consume terminals parked in spares; build replays
	// toward the next missing checkpoint.
	spares []*replay
	build  *replay
}

// NewPlayer returns a Player of all of tl's events, before the first one is
// applied. Its images are drawn with a renderer of opts, after the palette
// of tl's header, as [Export] draws them.
func NewPlayer(ctx context.Context, tl *Timeline, opts ...render.Option) (*Player, error) {
	p := &Player{ctx: ctx, header: tl.header, events: tl.snapshot(), opts: renderOptions(tl.header, opts)}
	if err := p.rewind(); err != nil {
		return nil, err
	}
	return p, nil
}

// rewind replaces the terminal with a fresh one, closed first so the pool
// can reuse it. The player holds its terminal for as long as its caller
// chooses, so it opens it from the unlimited pool.
func (p *Player) rewind() error {
	if p.r != nil {
		_ = p.r.close()
		p.r = nil
	}
	r, err := newReplay(p.ctx, recordRuntime, p.header, 0, 0)
	if err != nil {
		return err
	}
	p.r = r
	return nil
}

// Len returns the number of events in the timeline.
func (p *Player) Len() int { return len(p.events) }

// Pos returns the number of events applied.
func (p *Player) Pos() int {
	if p.r == nil {
		return 0
	}
	return p.r.n
}

// Seek applies the timeline's first n events, clamped to [0, Len]. If it
// fails, the other methods must not be called until a Seek succeeds.
func (p *Player) Seek(n int) error {
	n = min(max(n, 0), len(p.events))
	if p.r == nil || n < p.r.n {
		if r := p.takeSpare(n); r != nil {
			if p.r != nil {
				_ = p.r.close()
			}
			p.r = r
		} else if err := p.rewind(); err != nil {
			return err
		}
	}
	for p.r.n < n {
		if err := p.ctx.Err(); err != nil {
			return err
		}
		if err := p.r.apply(p.events[p.r.n]); err != nil {
			return err
		}
	}
	return nil
}

// takeSpare removes and returns the parked terminal furthest along without
// passing n, or nil if none is.
func (p *Player) takeSpare(n int) *replay {
	best := -1
	for i, r := range p.spares {
		if r.n <= n && (best < 0 || r.n > p.spares[best].n) {
			best = i
		}
	}
	if best < 0 {
		return nil
	}
	r := p.spares[best]
	p.spares = slices.Delete(p.spares, best, best+1)
	return r
}

// Prepare works for up to budget toward keeping limit terminals parked at
// evenly spaced checkpoints of the timeline, so a Seek back replays from
// the nearest one at or before it rather than from the start. It reports
// whether there is more to do, such as after a Seek took over a
// checkpoint, so it can be called again when the caller is idle. Each
// checkpoint holds a terminal's memory; a limit of zero closes them all.
func (p *Player) Prepare(limit int, budget time.Duration) (more bool, err error) {
	targets := checkpoints(len(p.events), limit)
	// Drop terminals parked anywhere the checkpoints no longer are.
	p.spares = slices.DeleteFunc(p.spares, func(r *replay) bool {
		if slices.Contains(targets, r.n) {
			return false
		}
		_ = r.close()
		return true
	})
	deadline := time.Now().Add(budget)
	for _, target := range targets {
		if slices.ContainsFunc(p.spares, func(r *replay) bool { return r.n == target }) {
			continue
		}
		if p.build != nil && p.build.n > target {
			_ = p.build.close()
			p.build = nil
		}
		if p.build == nil {
			if p.build, err = newReplay(p.ctx, recordRuntime, p.header, 0, 0); err != nil {
				return false, err
			}
		}
		for p.build.n < target {
			if time.Now().After(deadline) {
				return true, nil
			}
			if err = p.build.apply(p.events[p.build.n]); err != nil {
				_ = p.build.close()
				p.build = nil
				return false, err
			}
		}
		p.spares = append(p.spares, p.build)
		p.build = nil
	}
	if p.build != nil {
		_ = p.build.close()
		p.build = nil
	}
	return false, nil
}

// checkpoints returns limit event counts spaced evenly through n events,
// leaving out the start, which needs none.
func checkpoints(n, limit int) []int {
	var out []int
	for k := 1; k <= limit; k++ {
		// Targets are ordered, so only the previous one can be a duplicate.
		if c := k * n / (limit + 1); c > 0 && (len(out) == 0 || out[len(out)-1] != c) {
			out = append(out, c)
		}
	}
	return out
}

// Frame returns the terminal's screen and state after the applied events,
// at the time of the last of them.
func (p *Player) Frame() CellFrame { return p.r.current(p.r.now) }

// Screen returns the terminal's screen after the applied events, as
// [Session.Screen] does.
func (p *Player) Screen() *Screen { return screenOf(p.r.term, p.r.focused) }

func (p *Player) renderer() (*render.Renderer, error) {
	if p.rend == nil {
		rend, err := render.New(p.opts...)
		if err != nil {
			return nil, err
		}
		p.rend = rend
	}
	return p.rend, nil
}

// SetOptions replaces the options images are drawn with, such as to turn
// window chrome or the scrollbar on, from the next [Player.Image]. The
// header's palette still applies first, as for [NewPlayer].
func (p *Player) SetOptions(opts ...render.Option) error {
	if p.rend != nil {
		if err := p.rend.Close(); err != nil {
			return err
		}
		p.rend = nil
	}
	p.opts = renderOptions(p.header, opts)
	p.img = nil
	return nil
}

// Layout returns where [Player.Image] places the screen after the applied
// events in its image.
func (p *Player) Layout() (render.Layout, error) {
	rend, err := p.renderer()
	if err != nil {
		return render.Layout{}, err
	}
	cols, rows := p.r.term.Size()
	return rend.Layout(cols, rows), nil
}

// Image draws the terminal after the applied events. The image is reused
// by the next call to Image.
func (p *Player) Image() (*image.NRGBA, error) {
	if _, err := p.renderer(); err != nil {
		return nil, err
	}
	f := p.Frame()
	r := p.rend.Layout(f.Screen.Width(), f.Screen.Height()).Image
	if p.img == nil || p.img.Rect != r {
		p.img = image.NewNRGBA(r)
	}
	p.rend.Draw(p.img, f.Screen, f.State, f.At)
	return p.img, nil
}

// Close releases the player's terminals and renderer.
func (p *Player) Close() error {
	var err error
	if p.r != nil {
		err = p.r.close()
	}
	for _, r := range p.spares {
		err = errors.Join(err, r.close())
	}
	p.spares = nil
	if p.build != nil {
		err = errors.Join(err, p.build.close())
		p.build = nil
	}
	if p.rend != nil {
		err = errors.Join(err, p.rend.Close())
	}
	return err
}
