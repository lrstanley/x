// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"errors"
	"image"
	"sync"
	"time"

	"github.com/lrstanley/x/term/reel/render"
	"github.com/lrstanley/x/term/reel/vt"
)

// errImagerClosed is returned by [Imager] methods called after
// [Imager.Close].
var errImagerClosed = errors.New("reel: imager is closed")

// Imager draws a [Session]'s screen as it is now, such as for a test to
// look at, with a renderer of its own. Blinking text and cursors are drawn
// at their phase for the time since the timeline's first event, as a
// recording draws them. It keeps the last screen it read until the screen
// changes, so drawing an unchanged screen again copies nothing. Methods are
// safe for concurrent use; draws run one at a time. Close it when done:
// after [Session.Close], its methods return [ErrClosed].
type Imager struct {
	s *Session
	// opened is when the imager was made, the blink clock while the
	// timeline has no events.
	opened time.Time

	mu     sync.Mutex
	rend   *render.Renderer
	screen *vt.Buffer
	img    *image.NRGBA
	closed bool
}

// NewImager returns an Imager of s's screen, drawn with a renderer of
// opts, as [render.New] makes.
func NewImager(s *Session, opts ...render.Option) (*Imager, error) {
	rend, err := render.New(opts...)
	if err != nil {
		return nil, err
	}
	return &Imager{s: s, opened: time.Now(), rend: rend}, nil
}

// read returns the screen and its state as of now, reusing the last screen
// read while its generation is current. im.mu is held.
func (im *Imager) read() (*vt.Buffer, vt.State, error) {
	s := im.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() {
		return nil, vt.State{}, ErrClosed
	}
	st := s.term.State()
	st.Focused = s.Focused()
	if im.screen == nil || st.Generation == 0 || im.screen.Generation() != st.Generation {
		im.screen = s.term.Screen()
	}
	return im.screen, st, nil
}

// at returns the blink clock: the time since the timeline's first event.
func (im *Imager) at() time.Duration {
	start := im.s.timeline.started()
	if start.IsZero() {
		start = im.opened
	}
	return time.Since(start)
}

// Image draws the screen and returns the image, which the next call to
// Image reuses: copy it to keep it, or use [Imager.ImageInto].
func (im *Imager) Image() (*image.NRGBA, error) {
	im.mu.Lock()
	defer im.mu.Unlock()
	if im.closed {
		return nil, errImagerClosed
	}
	scr, st, err := im.read()
	if err != nil {
		return nil, err
	}
	r := im.rend.Layout(scr.Width(), scr.Height()).Image
	if im.img == nil || im.img.Rect != r {
		im.img = image.NewNRGBA(r)
	}
	im.rend.Draw(im.img, scr, st, im.at())
	return im.img, nil
}

// ImageInto draws the screen into dst at dst.Rect.Min. dst must be at least
// as large as the image [Imager.Image] returns.
func (im *Imager) ImageInto(dst *image.NRGBA) error {
	im.mu.Lock()
	defer im.mu.Unlock()
	if im.closed {
		return errImagerClosed
	}
	scr, st, err := im.read()
	if err != nil {
		return err
	}
	im.rend.Draw(dst, scr, st, im.at())
	return nil
}

// Close releases the renderer and the screen it holds. Later calls return
// an error. Close is safe to call more than once.
func (im *Imager) Close() error {
	im.mu.Lock()
	defer im.mu.Unlock()
	if im.closed {
		return nil
	}
	im.closed, im.screen, im.img = true, nil, nil
	return im.rend.Close()
}
