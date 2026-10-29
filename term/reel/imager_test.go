// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package reel

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"sync"
	"testing"
)

func newTestImager(t *testing.T, s *Session) *Imager {
	t.Helper()
	im, err := NewImager(s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = im.Close() })
	return im
}

// imageCopy draws im's screen and returns a copy.
func imageCopy(t *testing.T, im *Imager) *image.NRGBA {
	t.Helper()
	img, err := im.Image()
	if err != nil {
		t.Fatal(err)
	}
	out := image.NewNRGBA(img.Rect)
	copy(out.Pix, img.Pix)
	return out
}

func TestImagerFollowsScreen(t *testing.T) {
	t.Parallel()
	s := openTest(t, 20, 2)
	// A steady cursor, so the blink phase does not change the image.
	write(t, s, "\x1b[2 qone")
	im := newTestImager(t, s)
	first := imageCopy(t, im)
	held := im.screen
	if again := imageCopy(t, im); !bytes.Equal(first.Pix, again.Pix) || im.screen != held {
		t.Fatal("drawing an unchanged screen changed the image or read the screen again")
	}

	write(t, s, "\rtwo")
	second := imageCopy(t, im)
	if bytes.Equal(first.Pix, second.Pix) || im.screen == held {
		t.Fatal("image unchanged after the screen changed")
	}

	// State around the grid, such as focus and colors, is drawn too.
	if err := s.SetDefaultBackground(color.NRGBA{R: 0x40, A: 0xff}); err != nil {
		t.Fatal(err)
	}
	if err := s.Focus(false); err != nil {
		t.Fatal(err)
	}
	if third := imageCopy(t, im); bytes.Equal(second.Pix, third.Pix) {
		t.Fatal("image unchanged after the colors and focus changed")
	}

	dst := image.NewNRGBA(second.Rect.Add(image.Pt(3, 4)))
	if err := im.ImageInto(dst); err != nil {
		t.Fatal(err)
	}
	if img, _ := im.Image(); !bytes.Equal(dst.Pix, img.Pix) {
		t.Fatal("ImageInto drew a different image than Image")
	}
}

func TestImagerClosed(t *testing.T) {
	t.Parallel()
	s := openTest(t, 4, 1)
	im := newTestImager(t, s)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := im.Image(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Image after Session.Close = %v, want ErrClosed", err)
	}
	if err := im.Close(); err != nil {
		t.Fatal(err)
	}
	if err := im.Close(); err != nil {
		t.Fatalf("second Close = %v", err)
	}
	if err := im.ImageInto(image.NewNRGBA(image.Rect(0, 0, 1, 1))); err == nil {
		t.Fatal("ImageInto after Close succeeded")
	}
}

func TestImagerConcurrent(t *testing.T) {
	t.Parallel()
	s := openTest(t, 24, 3)
	im := newTestImager(t, s)
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			if _, err := im.Image(); err != nil {
				t.Error(err)
			}
		})
		wg.Go(func() {
			if err := s.Type("x"); err != nil {
				t.Error(err)
			}
		})
		wg.Go(func() {
			if _, err := s.Write([]byte("y")); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
}
