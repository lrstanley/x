// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package canvas

import (
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// Measure returns the size of s, a string with ANSI styles: the width of its
// widest line and its number of lines. It does not parse s into cells.
func Measure(s string, method ansi.Method) (w, h int) {
	if s == "" {
		return 0, 0
	}
	for line := range strings.Lines(s) {
		w = max(w, method.StringWidth(strings.TrimRight(line, "\r\n")))
		h++
	}
	if strings.HasSuffix(s, "\n") {
		h++
	}
	return w, h
}

// Parse returns a canvas holding s, a string with ANSI styles and
// hyperlinks, sized as [Measure] reports.
func Parse(s string, method ansi.Method) *Canvas {
	w, h := Measure(s, method)
	return parseSized(s, w, h, method)
}

func parseSized(s string, w, h int, method ansi.Method) *Canvas {
	c := New(w, h, method)
	uv.NewStyledString(s).Draw(c, c.Bounds())
	return c
}

// Cache keeps strings measured and parsed between frames, so a component
// whose view did not change is not measured or parsed again. Canvases it
// returns are shared and must not be changed.
type Cache struct {
	method  ansi.Method
	entries map[string]*cacheEntry
	frame   uint64
	cells   int // cells held by all parsed entries
}

type cacheEntry struct {
	w, h   int
	canvas *Canvas // nil until parsed
	used   uint64
}

// cacheIdleFrames is how many frames an unused entry is kept for, so content
// that comes and goes, like a spinner's frames, stays parsed.
const cacheIdleFrames = 64

// NewCache returns an empty cache that measures and parses with method.
func NewCache(method ansi.Method) *Cache {
	return &Cache{method: method, entries: make(map[string]*cacheEntry)}
}

func (c *Cache) entry(s string) *cacheEntry {
	e, ok := c.entries[s]
	if !ok {
		e = &cacheEntry{}
		e.w, e.h = Measure(s, c.method)
		c.entries[s] = e
	}
	e.used = c.frame
	return e
}

// Size returns the size of s, as [Measure] does.
func (c *Cache) Size(s string) (w, h int) {
	e := c.entry(s)
	return e.w, e.h
}

// Get returns s parsed, as [Parse] does.
func (c *Cache) Get(s string) *Canvas {
	e := c.entry(s)
	if e.canvas == nil {
		e.canvas = parseSized(s, e.w, e.h, c.method)
		c.cells += e.w * e.h
	}
	return e.canvas
}

// EndFrame marks the end of a frame of frameCells cells. Entries unused for a
// while are dropped, and when the cache holds much more than a few frames'
// worth of cells, such as when a large view changes on every frame, every
// entry not used in this frame is dropped.
func (c *Cache) EndFrame(frameCells int) {
	limit := 4*frameCells + 1<<16
	if c.cells > limit || c.frame%cacheIdleFrames == 0 {
		for s, e := range c.entries {
			if e.used < c.frame && (c.cells > limit || e.used+cacheIdleFrames < c.frame) {
				if e.canvas != nil {
					c.cells -= e.w * e.h
				}
				delete(c.entries, s)
			}
		}
	}
	c.frame++
}
