// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"context"
	"sync"
)

// Recording is a [Record] in progress.
type Recording struct {
	stop chan struct{}
	done chan struct{}
	once sync.Once
	// err is set before done is closed.
	err error
}

// Record writes the outputs of p from s while it runs, replaying each event
// as it is appended through the same pipeline as [Export], so frames are
// drawn and encoded live rather than at the end. [Recording.Stop] ends it
// on the events appended so far and writes what [Export] would for the
// timeline at that point.
//
// Paths are expanded when Record starts, so {duration} is an error, and
// {timestamp} is when the timeline started (or now, if it is empty). A
// still whose mark never comes is not written, and Stop reports it as a
// [NoMarkError]; other outputs are still written. Effects run with
// [Context.Live] set. Asciicast outputs leave out descriptions of effects
// whose marks never came.
//
// Canceling ctx abandons the recording: its outputs are discarded, and Stop
// returns ctx's error.
func Record(ctx context.Context, s *Session, p Profile) (*Recording, error) {
	tl := s.Timeline()
	vars, err := pathVars(p.Vars, tl.started())
	if err != nil {
		return nil, err
	}
	rec, err := newRecorder(ctx, recordRuntime, tl, true, p, vars)
	if err != nil {
		return nil, err
	}
	r := &Recording{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(r.done)
		r.err = r.run(ctx, tl, rec)
	}()
	return r, nil
}

// Stop ends the recording on the events appended so far, writes its
// outputs, and returns the first error. It is safe to call repeatedly, and
// always returns the same result.
func (r *Recording) Stop() error {
	r.once.Do(func() { close(r.stop) })
	<-r.done
	return r.err
}

// run replays the events of tl as they are appended, until the recording
// stops or ctx is canceled.
func (r *Recording) run(ctx context.Context, tl *Timeline, rec *recorder) error {
	done := false
	defer func() {
		if !done {
			rec.abort()
		}
	}()
	n, stopping := 0, false
	for {
		updated := tl.Updated()
		events := tl.snapshot()
		for ; n < len(events); n++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := rec.apply(events[n]); err != nil {
				return err
			}
		}
		if stopping {
			// Cancellation wins even if the select picked stop.
			if err := ctx.Err(); err != nil {
				return err
			}
			fixed := tl.fixed(tl.header, n)
			if err := rec.end(fixed.Duration()); err != nil {
				return err
			}
			done = true
			return rec.commit(fixed)
		}
		select {
		case <-updated:
		case <-r.stop:
			// One more pass for events appended until now.
			stopping = true
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
