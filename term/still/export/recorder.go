// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package export

import (
	"errors"
	"image"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// Frame is one frame of a recording: what [WithChannel] receives, and what an
// [Encoder] is given.
type Frame struct {
	Image image.Image
	// Delay is how long the frame plays. Sent to a [Recorder], zero means the
	// time since the previous frame was read. Given to an [Encoder], it is
	// always positive.
	Delay time.Duration
	// Isolated palettizes this frame on its own palette. It adds no colors to
	// the shared recording palette, and consecutive isolated frames share one
	// palette so they stay comparable to each other. GIF stores that palette
	// as a local color table on each of those frames, and other formats
	// ignore it. Terminal frames leave it false.
	Isolated bool
	// Done, when set, is called from the recorder's goroutine once the encoder
	// has returned from the frame. Image is then handed back to the sender,
	// which may reuse it, so the sender must not share it with anything else.
	// An [Encoder] is never given a Done.
	Done func()
}

var (
	errNilFrame   = errors.New("export: nil frame")
	errEmptyFrame = errors.New("export: empty frame bounds")
)

// Encoder writes frames into one output, such as [GIFEncoder] or
// [VideoEncoder]. A [Recorder] calls it from a single goroutine, so encoders
// need not be safe for concurrent use.
type Encoder interface {
	// WriteFrame adds the next frame, which plays for f.Delay. f.Image is only
	// valid until WriteFrame returns, so an encoder that keeps it must copy it.
	WriteFrame(f Frame) error
	// Close finishes the output. With no frames written, it writes nothing.
	Close() error
	// Discard abandons the output, leaving any existing destination alone.
	Discard() error
}

// Recorder reads frames from [WithChannel], or captures them with
// [WithFrameRate], and writes them to an [Encoder] with resolved delays. Every
// encoder fed the same frames plays on the same timeline: a zero
// [Frame.Delay] becomes the time since the previous frame was read, and each
// delay is rounded to whole steps of the delay grid ([WithDelayGrid] or the
// [WithFrameRate] FPS), or of 10ms without one, at least one step.
type Recorder struct {
	enc  Encoder
	opts options
	// step is the delay grid.
	step time.Duration
	// last is when the previous frame was read.
	last time.Time
	// done is closed to stop reading.
	done chan struct{}
	wg   sync.WaitGroup
	// err is the first invalid frame or encoder error. Only the reader writes
	// it, and finish reads it after the reader exits.
	err    error
	frames atomic.Int64

	once   sync.Once
	result error
}

// Record starts recording into enc, which it owns from then on: when Record
// fails, it discards enc. It needs exactly one of [WithChannel] or
// [WithFrameRate], and reads [WithDelayGrid]. Other options are ignored, so
// the option list of the encoder can be shared. Call [Recorder.Close] to
// finish the output.
func Record(enc Encoder, opts ...Option) (*Recorder, error) {
	if enc == nil {
		return nil, errors.New("export: nil encoder")
	}
	cfg, err := collectOptions(validateRecorder, opts...)
	if err != nil {
		return nil, errors.Join(err, enc.Discard())
	}
	r := &Recorder{
		enc:  enc,
		opts: cfg,
		step: delayStep(cfg),
		last: time.Now(),
		done: make(chan struct{}),
	}
	if cfg.channelSet {
		r.wg.Go(func() { readFrames(cfg.channel, r.done, r.write) })
	} else {
		r.wg.Go(r.tick)
	}
	return r, nil
}

// delayStep is the delay grid of o: max(1, 100/fps) centiseconds.
func delayStep(o options) time.Duration {
	var fps int
	switch {
	case o.delayGridSet:
		fps = o.delayGrid
	case o.frameRateSet:
		fps = o.frameRate
	default:
		return 10 * time.Millisecond
	}
	return time.Duration(max(1, 100/fps)) * 10 * time.Millisecond
}

// Frames returns how many frames have been written to the encoder.
func (r *Recorder) Frames() int {
	return int(r.frames.Load())
}

// Close stops reading, writes the frames already buffered in the channel, and
// closes the encoder, which finishes the output. It does not close the
// channel. If a frame was invalid or the encoder failed, the output is
// discarded instead and Close returns that error. Later calls (of Close or
// [Recorder.Discard]) return the same result.
func (r *Recorder) Close() error {
	return r.finish(false)
}

// Discard stops reading like [Recorder.Close], then discards the output.
func (r *Recorder) Discard() error {
	return r.finish(true)
}

func (r *Recorder) finish(discard bool) error {
	r.once.Do(func() {
		close(r.done)
		r.wg.Wait()
		switch {
		case r.err != nil:
			r.result = errors.Join(r.err, r.enc.Discard())
		case discard:
			r.result = r.enc.Discard()
		default:
			r.result = r.enc.Close()
		}
	})
	return r.result
}

// readFrames passes each frame from ch to write until ch closes or done is
// closed. Once done is closed, frames already buffered are still passed on: a
// sender that fills ch and then closes the recorder (rather than closing ch
// first) would otherwise lose them, since select picks randomly between done
// and a ready channel.
func readFrames(ch <-chan Frame, done <-chan struct{}, write func(Frame)) {
	for {
		select {
		case <-done:
			for {
				select {
				case f, ok := <-ch:
					if !ok {
						return
					}
					write(f)
				default:
					return
				}
			}
		case f, ok := <-ch:
			if !ok {
				return
			}
			write(f)
		}
	}
}

// tick captures a [WithFrameRate] frame on every tick until done is closed.
// Ticks that arrive while the frame function runs are dropped.
func (r *Recorder) tick() {
	interval := time.Second / time.Duration(r.opts.frameRate)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-r.done:
			return
		case <-ticker.C:
			f := Frame{Image: r.opts.frameRateFn()}
			if r.opts.frameRateStrict {
				f.Delay = interval
			}
			r.write(f)
		}
	}
}

// write sends f to the encoder, then releases it. Invalid frames and frames
// after an error are skipped, but still released, so the sender is never left
// waiting on an image.
func (r *Recorder) write(f Frame) {
	if f.Done != nil {
		defer f.Done()
	}
	f.Delay, f.Done = r.nextDelay(f.Delay), nil
	if r.err != nil {
		return
	}
	switch {
	case f.Image == nil:
		r.err = errNilFrame
	case f.Image.Bounds().Empty():
		r.err = errEmptyFrame
	default:
		if r.err = r.enc.WriteFrame(f); r.err == nil {
			r.frames.Add(1)
		}
	}
}

// nextDelay resolves the delay for the frame just read: explicit when
// positive, otherwise the time since the previous read, then rounded to the
// delay grid.
func (r *Recorder) nextDelay(explicit time.Duration) time.Duration {
	now := time.Now()
	delay := explicit
	if delay <= 0 {
		delay = now.Sub(r.last)
	}
	r.last = now
	steps := max(1, math.Round(float64(delay)/float64(r.step)))
	return time.Duration(steps) * r.step
}
