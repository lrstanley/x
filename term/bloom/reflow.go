// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package bloom

import (
	"image"
	"time"

	"github.com/lrstanley/x/term/bloom/anim"
)

type layoutAnimation struct{ opts []anim.Option }

type layoutFrameMsg struct{ root *Root }

type rectAnimation struct {
	values [4]anim.Value
	seen   bool
}

// layoutMotion belongs only to a retained Root; pooled renderers stay stateless.
type layoutMotion struct {
	now           func() time.Time
	states        map[any]*rectAnimation
	at            time.Time
	size          image.Point
	snap, running bool
}

func (m *layoutMotion) begin(w, h int) {
	size := image.Pt(w, h)
	m.snap, m.size, m.running = size != m.size, size, false
	m.at = time.Time{}
	for _, s := range m.states {
		s.seen = false
	}
}

// rect returns where the box with ID id goes this frame, on its way to
// target. Its position animates relative to origin, where the nearest
// animated box it is in is on screen, so that it moves with that box rather
// than trailing behind it.
func (m *layoutMotion) rect(id any, cfg *layoutAnimation, target image.Rectangle, origin image.Point) image.Rectangle {
	if m.at.IsZero() {
		m.at = m.now()
	}
	rel := target.Min.Sub(origin)
	coords := [4]int{rel.X, rel.Y, max(target.Dx(), 0), max(target.Dy(), 0)}
	s := m.states[id]
	if s == nil {
		s = &rectAnimation{}
		for i, c := range coords {
			s.values[i] = anim.NewValue(float64(c), cfg.opts...)
		}
		if m.states == nil {
			m.states = make(map[any]*rectAnimation)
		}
		m.states[id] = s
	}
	s.seen = true
	for i, c := range coords {
		v := &s.values[i]
		if m.snap {
			v.Set(float64(c))
		} else {
			// Moving parents can change a child's target every frame. Sample its
			// existing motion before retargeting so it keeps making progress.
			if v.Target() != float64(c) {
				v.Advance(m.at)
			}
			v.SetTarget(float64(c), m.at)
		}
		if v.Advance(m.at) {
			m.running = true
		}
		coords[i] = v.Int()
	}
	at := image.Pt(coords[0], coords[1]).Add(origin)
	return image.Rectangle{Min: at, Max: at.Add(image.Pt(max(coords[2], 0), max(coords[3], 0)))}
}

func (m *layoutMotion) end() {
	for id, s := range m.states {
		if !s.seen {
			delete(m.states, id)
		}
	}
}
