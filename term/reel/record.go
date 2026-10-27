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
	// err is the result, set before done is closed.
	err error
}

// Record writes the outputs of p from s while it runs. It tails the
// session's timeline, replaying each event into a terminal of its own as it
// is appended, through the same pipeline as [Export], so the frames are
// drawn and encoded while the program runs rather than all at the end.
// [Recording.Stop] ends it on the events appended so far, and writes the
// outputs [Export] would write from the timeline at that point, asciicast
// files included.
//
// Paths are expanded when Record starts, so {duration}, which is only known
// at the end, is an error, and {timestamp} is when the timeline started, or
// now when nothing has been appended yet. A still whose mark never comes
// writes nothing, and Stop reports it as an error wrapping [ErrNoMark]; the
// other outputs are still written.
//
// Canceling ctx abandons the recording: its outputs are discarded, and Stop
// returns ctx's error.
func Record(ctx context.Context, s *Session, p Profile) (*Recording, error) {
	tl := s.Timeline()
	vars, err := pathVars(p.Vars, tl.started())
	if err != nil {
		return nil, err
	}
	rec, err := newRecorder(ctx, recordRuntime, tl, p, vars)
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
// outputs, and returns the first error. Later calls return the same result.
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
			// A ctx canceled before Stop wins, though the select may
			// have seen both.
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
			// One more pass, for the events appended until now.
			stopping = true
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
