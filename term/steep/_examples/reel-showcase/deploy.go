// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type stage struct {
	name string
	dur  float64 // seconds
}

var stages = []stage{
	{"build", 1.3},
	{"test", 1.5},
	{"canary", 1.5},
	{"promote", 1.3},
}

const (
	stageCanary  = 2
	stagePromote = 3
	testCount    = 48
	failingTests = 3 // in a service whose tests fail
)

type deployState int

const (
	deployRunning deployState = iota
	deployAwaiting
	deployDone
	deployFailed
	deployRolledBack
)

// deploy is one run of the pipeline for a service. It advances on frame
// ticks, so the model stays the only owner of time.
type deploy struct {
	svc        int
	name       string
	from, to   string
	fails      bool
	state      deployState
	stage      int
	started    float64
	stageStart float64
	ended      float64
	stageTimes []float64
}

func newDeploy(svc int, s service, now float64) *deploy {
	return &deploy{
		svc:        svc,
		name:       s.name,
		from:       s.version,
		to:         bumpMinor(s.version),
		fails:      s.fails,
		started:    now,
		stageStart: now,
		stageTimes: make([]float64, len(stages)),
	}
}

func bumpMinor(v string) string {
	var major, minor, patch int
	if _, err := fmt.Sscanf(v, "v%d.%d.%d", &major, &minor, &patch); err != nil {
		return v + "-next"
	}
	return fmt.Sprintf("v%d.%d.0", major, minor+1)
}

// within is how far through the current stage the deploy is, 0-1.
func (d *deploy) within(now float64) float64 {
	switch d.state {
	case deployRunning:
		return min((now-d.stageStart)/stages[d.stage].dur, 1)
	case deployAwaiting:
		return 1
	case deployDone, deployFailed, deployRolledBack:
	}
	return 0
}

// progress is the overall progress, 0-1.
func (d *deploy) progress(now float64) float64 {
	switch d.state {
	case deployDone:
		return 1
	case deployAwaiting:
		return float64(stageCanary+1) / float64(len(stages))
	case deployRunning, deployFailed, deployRolledBack:
	}
	return (float64(d.stage) + d.within(now)) / float64(len(stages))
}

// deployEvent is something that happened during [deploy.advance], for the log.
type deployEvent struct {
	level logLevel
	msg   string
}

// advance moves the deploy along to now, returning what happened.
func (d *deploy) advance(now float64) []deployEvent {
	if d.state != deployRunning {
		return nil
	}
	var events []deployEvent
	if d.fails && d.stage == 1 && d.within(now) > 0.72 {
		d.state, d.ended = deployFailed, now
		return append(events,
			deployEvent{levelError, "--- FAIL: TestInvoiceRounding (0.02s)  want=10.00 got=9.99"},
			deployEvent{levelError, fmt.Sprintf("test stage failed  passed=%d failed=%d", testCount-failingTests, failingTests)},
			deployEvent{levelWarn, "deploy aborted, " + d.from + " keeps serving"},
		)
	}
	for d.state == deployRunning && d.within(now) >= 1 {
		d.stageTimes[d.stage] = now - d.stageStart
		switch d.stage {
		case 0:
			events = append(events, deployEvent{levelInfo, "image pushed  digest=sha256:9f86d08…c3a1 size=48.2MB"})
		case 1:
			events = append(events, deployEvent{levelInfo, fmt.Sprintf("tests passed  %d/%d", testCount, testCount)})
		case stageCanary:
			d.state = deployAwaiting
			return append(events, deployEvent{levelWarn, "canary healthy at 25% traffic, awaiting approval"})
		case stagePromote:
			d.state, d.ended = deployDone, now
			return append(events, deployEvent{levelInfo, fmt.Sprintf("%s is live in all regions  took=%.1fs", d.to, now-d.started)})
		}
		d.stage++
		d.stageStart = now
		events = append(events, deployEvent{levelInfo, stages[d.stage].name + " started"})
	}
	return events
}

// approve promotes an awaiting canary.
func (d *deploy) approve(now float64) []deployEvent {
	d.state, d.stage, d.stageStart = deployRunning, stagePromote, now
	return []deployEvent{{levelInfo, "promotion approved, shifting traffic to " + d.to}}
}

// reject rolls an awaiting canary back.
func (d *deploy) reject(now float64) []deployEvent {
	d.state, d.ended = deployRolledBack, now
	return []deployEvent{{levelWarn, "canary rolled back, " + d.from + " keeps serving"}}
}

func (d *deploy) active() bool {
	return d != nil && (d.state == deployRunning || d.state == deployAwaiting)
}

// appName is the application the deploy's OSC 7501 records name.
const appName = "aurora"

// status is the root OSC 7501 record for the deploy, nil when there is none.
func (d *deploy) status(now float64) *tea.ProgramStatus {
	if d == nil {
		return nil
	}
	ps := &tea.ProgramStatus{App: appName, Title: "deploy " + d.name}
	switch d.state {
	case deployRunning:
		ps.State = tea.ProgramStateWorking
		ps.Progress, ps.HasProgress = int(d.progress(now)*100), true
		ps.Message = stages[d.stage].name
	case deployAwaiting:
		ps.State = tea.ProgramStateBlocked
		ps.Kind = tea.ProgramStatusKindQuestion
		ps.Message = "promote " + d.to + " to all regions?"
	case deployDone:
		ps.State = tea.ProgramStateDone
		ps.Title = d.name + " " + d.to + " is live"
		ps.Message = fmt.Sprintf("deployed in %.1fs", d.ended-d.started)
	case deployFailed:
		ps.State = tea.ProgramStateError
		ps.Title = "deploy " + d.name + " failed"
		ps.Message = fmt.Sprintf("%d tests failing", failingTests)
	case deployRolledBack:
		ps.State = tea.ProgramStateIdle
		ps.Title = "deploy " + d.name + " rolled back"
	}
	return ps
}

// stageState is where stage i of the deploy is.
func (d *deploy) stageState(i int) ansi.ProgramState {
	switch {
	case d.state == deployFailed && i == d.stage:
		return ansi.ProgramStateError
	case i < d.stage, d.state == deployDone:
		return ansi.ProgramStateDone
	case i == d.stage && d.state == deployAwaiting:
		return ansi.ProgramStateBlocked
	case i == d.stage && d.state == deployRunning:
		return ansi.ProgramStateWorking
	}
	return ansi.ProgramStateIdle
}

// stageStatuses returns the raw OSC 7501 sequences for one record per stage,
// which the renderer draws as colored dots after the title. The root record
// comes from [tea.View.ProgramStatus]; these extra records go out with
// [tea.Raw].
func (d *deploy) stageStatuses() string {
	var sb strings.Builder
	for i, s := range stages {
		sb.WriteString(ansi.SetProgramStatus(ansi.ProgramStatus{
			ID: "deploy." + s.name, App: appName, Title: s.name, State: d.stageState(i),
		}))
	}
	return sb.String()
}

// stageKey identifies which per-stage records are current, so they are only
// re-sent when one changes.
func (d *deploy) stageKey() string {
	if d == nil {
		return ""
	}
	return fmt.Sprintf("%s/%d/%d", d.name, d.stage, d.state)
}
