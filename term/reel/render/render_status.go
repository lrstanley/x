// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render

import (
	"math"
	"time"

	idraw "github.com/lrstanley/x/term/reel/render/internal/draw"
	"github.com/lrstanley/x/term/reel/vt/screen"
)

// OSC 7501 program status is drawn only with window chrome: the most urgent
// record replaces the window title (render_status_title.go) and colors a thin
// outline just outside the window edge (render_status_outline.go), and every
// other record adds a dot after the title. Without a title bar there is no
// calm place for the text, so nothing is drawn.

const (
	// statusMaxDots is the most records shown as dots after the title.
	statusMaxDots = 6

	// statusBreath is one blocked pulse, in and out. Its brightness snaps to
	// statusBreathLevels steps, so a recording holds each for a few frames,
	// identical frames merge, and the frame cache redraws only on a step.
	statusBreath       = 2 * time.Second
	statusBreathLevels = 6
)

// statusView is what the program status draws: the record that drives the
// title and outline, the states of the dots after the title, and the breath
// step of a blocked outline. The zero value draws nothing. It is comparable,
// so the frame cache can tell when it changes.
type statusView struct {
	shown bool
	drv   screen.ProgramStatus
	ndots int
	dots  [statusMaxDots]screen.ProgramState
	// breath is the blocked outline's brightness step, 0 through
	// statusBreathLevels, at the frame's offset. It is 0 unless drv is
	// blocked.
	breath int8
}

// statusEnabled reports whether program status can be drawn at all.
func (d *Renderer) statusEnabled() bool {
	return d.opts.WindowChrome && d.opts.ProgramStatus
}

// urgency is s, since [screen.ProgramState] is ordered by urgency, or idle
// for a state this renderer does not know.
func urgency(s screen.ProgramState) screen.ProgramState {
	if s > screen.ProgramError {
		return screen.ProgramIdle
	}
	return s
}

// shownStatus is the program status drawn for st at at. The driver is the
// most urgent record, the earliest on ties, and the dots are the others, most
// urgent first, in record order on ties.
func (d *Renderer) shownStatus(st screen.State, at time.Duration) statusView {
	if !d.statusEnabled() || len(st.ProgramStatuses) == 0 {
		return statusView{}
	}
	recs := st.ProgramStatuses
	best := 0
	for i := range recs {
		if urgency(recs[i].State) > urgency(recs[best].State) {
			best = i
		}
	}
	v := statusView{shown: true, drv: recs[best]}
	if v.breathes() {
		v.breath = breathStep(at)
	}
	for s := screen.ProgramError; v.ndots < len(v.dots); s-- {
		for i := range recs {
			if i != best && urgency(recs[i].State) == s && v.ndots < len(v.dots) {
				v.dots[v.ndots] = recs[i].State
				v.ndots++
			}
		}
		if s == screen.ProgramIdle {
			break
		}
	}
	return v
}

// animates reports whether v looks different at every frame: a working
// record with no progress spins and moves comets around the outline.
func (v statusView) animates() bool {
	return v.shown && v.drv.State == screen.ProgramWorking && v.drv.Progress < 0
}

// breathes reports whether v's outline breathes: a blocked record, which
// steps through statusBreathLevels brightness levels as the offset moves.
func (v statusView) breathes() bool {
	return v.shown && v.drv.State == screen.ProgramBlocked
}

// breathStep is the blocked outline's brightness step at offset at.
func breathStep(at time.Duration) int8 {
	u := float64(idraw.Cycle(at, statusBreath)) / float64(statusBreath)
	b := easeInOut(0.5 - 0.5*math.Cos(2*math.Pi*u))
	return int8(math.Round(b * statusBreathLevels))
}
