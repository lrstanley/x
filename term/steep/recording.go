// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package steep

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/lrstanley/x/term/reel"
)

// exportPolicy is when a harness exports its recording.
type exportPolicy int

const (
	// exportOff keeps the timeline in memory and writes nothing.
	exportOff exportPolicy = iota
	// exportOnFailure exports at [Harness.Close] when the test failed.
	exportOnFailure
	// exportAlways exports at [Harness.Close].
	exportAlways
)

// String returns the name of the policy, as the "export" of a "steep.start"
// annotation: "off", "failure", or "always".
func (p exportPolicy) String() string {
	switch p {
	case exportOnFailure:
		return "failure"
	case exportAlways:
		return "always"
	default:
		return "off"
	}
}

// StillKind is a kind of still [WithStills] writes at a hook event's mark.
// Kinds combine with |.
type StillKind uint8

const (
	// StillPNG is a PNG of the terminal at the mark, drawn with the
	// recording's [reel.Profile.Render] options, through the effects that
	// run on stills alone (see [reel.StillEffect]).
	StillPNG StillKind = 1 << iota
	// StillSnapshot is the screen's text at the mark, as [Harness.View]
	// returned it then, in a ".snap" file.
	StillSnapshot
)

// Path variables steep adds to [reel.Profile.Vars].
const (
	varName   = "name"
	varStatus = "status"
)

// Values of {status}.
const (
	statusPassed = "passed"
	statusFailed = "failed"
)

// Result is a test's result, which an export passes to effects as
// [reel.Context.Meta], such as for [Outro].
type Result struct {
	// Name is the test's name, [testing.TB.Name].
	Name string
	// Failed is whether the test failed, or was panicking, by the time
	// [Harness.Close] ran.
	Failed bool
	// Failures are the failure messages the harness reported, oldest first:
	// failed assertions, waits, and snapshots, and harness errors such as
	// timeouts, followed by "test panicked" for a test that panicked.
	// Failures a test reports itself, through its own [testing.TB], are not
	// included.
	Failures []string
	// Duration is how long the harness ran, from [NewHarness] to
	// [Harness.Close].
	Duration time.Duration
}

// captureEvents are the hook events [WithStills] uses when it is given none,
// those of actions that change what the terminal shows or wait for it.
var captureEvents = []HookEvent{HookInput, HookTerminal, HookProgram, HookWait, HookReady, HookClose}

// WithRecording sets what an export writes: the outputs, renderer, encoder
// options, effects, and timing of a [reel.Profile]. It does not export by
// itself: see [WithExportAlways] and [WithExportOnFailure]. Calls merge,
// later ones winning (see [reel.Profile.Merge]).
//
// The profile's Render options also draw [Harness.Image] and
// [Harness.ImageInto], whether or not the harness exports.
//
// Output paths may hold reel's path variables ({duration}, {timestamp}, and
// {mark}), the profile's Vars, and two of steep's: {name}, the test's name
// with every character but letters and digits replaced by "_" (so a subtest's
// slashes do not make directories), and {status}, "passed" or "failed". A
// {name} or {status} in the profile's Vars replaces steep's, such as to give
// a recording a stable name. A relative path is relative to the directory
// [WithArtifacts] sets. A profile with nil Outputs writes
// "{name}_{status}.gif" and "{name}_{status}.cast".
//
// At export, [reel.Context.Meta] is the test's *[Result], replacing the
// profile's, which [Outro] reads to show PASS or FAIL, and for a failed test,
// to hold the screen it failed on for at least 3s before the card.
//
// The recording is the [reel.Timeline] of the session up to the start of
// [Harness.Close], so a program's repaint as it exits is left out.
//
// Environment (each applied unless [WithIgnoreEnv] is set): STEEP_EXPORT_TYPE
// replaces the outputs and stills, as a comma-separated list of gif, mp4,
// webm, webp, and cast, which write "{name}_{status}.<type>", png and
// snapshot, which write [StillPNG] and [StillSnapshot] stills at the events
// [WithStills] uses by default, and all, which is gif, cast, png, and
// snapshot. Unknown names are ignored. STEEP_EXPORT_FPS and
// STEEP_EXPORT_SPEED replace FPS and Speed.
// STEEP_EXPORT_QUALITY (0 to 100), STEEP_EXPORT_MAX_FRAMES,
// STEEP_EXPORT_MAX_BYTES, and STEEP_EXPORT_OPTIMIZE (none, frames, dirty,
// quantize, or all, comma-separated) are appended to the profile's Encode
// options, and STEEP_RENDERER_SCALE and STEEP_RENDERER_FONT_FAMILY to its
// Render options, so they win over options of the same kind without
// dropping the others.
func WithRecording(p reel.Profile) Option {
	return func(cfg *options) {
		cfg.recording = cfg.recording.Merge(p)
	}
}

// WithArtifacts sets the directory an export writes relative output paths
// under, created when needed. A relative dir is relative to the test's
// package directory. Without it, files go to [testing.TB.ArtifactDir], which
// only keeps them when tests run with "go test -artifacts" (see
// "-outputdir"): otherwise it is a temporary directory, removed when the test
// ends.
//
// Environment: STEEP_EXPORT_DIR (overrides this option unless [WithIgnoreEnv]
// is set).
func WithArtifacts(dir string) Option {
	return func(cfg *options) {
		cfg.artifacts = dir
	}
}

// WithExportOnFailure exports the recording (see [WithRecording]) at
// [Harness.Close] if the test has failed by then, and writes nothing when it
// passes. Without an export policy, nothing is written: the [reel.Timeline]
// is kept in memory, for [Harness.Timeline], since a failing test already
// prints the screen as text, and artifacts written into a package directory
// may not be ignored by version control.
//
// [Harness.Close] runs as a cleanup of the test, which also runs when the
// test panics or calls [testing.TB.FailNow], so those export too. A test
// killed by "go test -timeout", or a panic on a goroutine of its own, ends
// the process before cleanups run, and writes nothing.
//
// Environment: STEEP_EXPORT_MODE sets the policy: on (or always, true, 1)
// for [WithExportAlways], failure (or on-failure) for this one, and off (or
// false, 0) for none. It overrides this option unless [WithIgnoreEnv] is set.
func WithExportOnFailure() Option {
	return func(cfg *options) {
		cfg.policy = exportOnFailure
	}
}

// WithExportAlways exports the recording (see [WithRecording]) at
// [Harness.Close], whether the test passed or failed. See
// [WithExportOnFailure] for when an export runs, and the environment
// override.
func WithExportAlways() Option {
	return func(cfg *options) {
		cfg.policy = exportAlways
	}
}

// WithStills writes stills of kinds at the marks of events (see
// [Harness.Mark] for how each hook event is marked), one file per mark:
// "{name}_{status}_{mark}.png" for [StillPNG], and
// "{name}_{status}_{mark}.snap" for [StillSnapshot], such as
// "TestLogin_failed_input-2.png" for the second input. Without events, it
// covers the events of actions that change what the terminal shows or wait
// for it: input, terminal, program, wait, ready, and close. Calls
// accumulate.
//
// Stills are written with the recording, so only when an export policy is on
// (see [WithExportOnFailure]). For a single PNG of the last frame, add a
// ".png" output to the profile instead.
func WithStills(kinds StillKind, events ...HookEvent) Option {
	return func(cfg *options) {
		if len(events) == 0 {
			events = captureEvents
		}
		cfg.stills = addStills(cfg.stills, kinds, events)
	}
}

// addStills returns stills with kinds added at events.
func addStills(stills map[HookEvent]StillKind, kinds StillKind, events []HookEvent) map[HookEvent]StillKind {
	if kinds == 0 {
		return stills
	}
	if stills == nil {
		stills = make(map[HookEvent]StillKind, len(events))
	}
	for _, ev := range events {
		stills[ev] |= kinds
	}
	return stills
}

// stillPath is the path of a still, before its extension.
const stillPath = "{name}_{status}_{mark}"

// hookMark is a mark the harness placed for a hook event.
type hookMark struct {
	label string
	// stills are the kinds of still written at the mark.
	stills StillKind
	// snap is the screen's text at the mark, for a [StillSnapshot].
	snap string
}

// Mark places a [reel.Mark] labeled label in the session's timeline, which
// [reel.Timeline.Between] can cut a recording at. The label must not be
// empty.
//
// The harness marks every hook event once its action is done, just before
// its After hooks run, labeled with the event's name and its count so far,
// such as "input-1", "input-2", and "wait-1", so each one can be told apart
// in an asciicast file and cut at. While an export policy is on, an input,
// terminal, or program action is marked once the program has painted its
// response (see [WithSettle]). [Harness.Close] places its mark as it starts,
// where the recording ends. Use other labels for marks of a test's own.
func (h *Harness) Mark(label string) *Harness {
	h.tb.Helper()
	h.check(h.session.Mark(label))
	return h
}

// Session returns the [reel.Session] the program runs in.
func (h *Harness) Session() *reel.Session { return h.session }

// Timeline returns the session's [reel.Timeline], which grows while the
// program runs.
func (h *Harness) Timeline() *reel.Timeline { return h.session.Timeline() }

// markEvent marks event in the timeline (see [Harness.Mark]), and returns
// the label, or "" once the session is closed.
func (h *Harness) markEvent(event HookEvent) string {
	h.tb.Helper()
	h.marksMu.Lock()
	defer h.marksMu.Unlock()
	label := event.String() + "-" + strconv.Itoa(h.markCount[event]+1)
	err := h.session.Mark(label)
	if errors.Is(err, reel.ErrClosed) {
		return ""
	}
	if err != nil {
		h.check(err)
		return ""
	}
	h.markCount[event]++
	m := hookMark{label: label}
	if h.cfg.policy != exportOff {
		m.stills = h.cfg.stills[event]
		if m.stills&StillSnapshot != 0 {
			m.snap = h.View()
		}
	}
	h.marks = append(h.marks, m)
	return label
}

// exportRecording exports the recording up to the mark labeled end, as the
// policy says. A skipped test that did not fail exports nothing. It reports
// a failure to export as a test error.
func (h *Harness) exportRecording(end string) {
	h.tb.Helper()
	if h.cfg.policy == exportOff {
		return
	}
	failed, panicked := h.testStatus()
	if !failed && (h.cfg.policy == exportOnFailure || h.tb.Skipped()) {
		return
	}
	if err := h.writeRecording(end, failed, panicked); err != nil {
		h.tb.Errorf("steep: export: %v", err)
	}
}

func (h *Harness) writeRecording(end string, failed, panicked bool) error {
	h.tb.Helper()
	status := statusPassed
	if failed {
		status = statusFailed
	}
	p := h.cfg.recording
	p.Vars = map[string]string{varName: normalizeTestName(h.tb.Name()), varStatus: status}
	maps.Copy(p.Vars, h.cfg.recording.Vars)
	result := &Result{
		Name:     h.tb.Name(),
		Failed:   failed,
		Failures: h.failures.messages(),
		Duration: time.Since(h.start),
	}
	if panicked {
		result.Failures = append(result.Failures, "test panicked")
	}
	p.Meta = result

	marks := h.marksUpTo(end)
	outputs := slices.Clone(p.Outputs)
	if outputs == nil {
		outputs = reel.Files("{name}_{status}.gif", "{name}_{status}.cast")
	}
	var pngs []string
	for _, m := range marks {
		if m.stills&StillPNG != 0 {
			pngs = append(pngs, m.label)
		}
	}
	if len(pngs) > 0 {
		outputs = append(outputs, reel.File{Path: stillPath + ".png", Marks: pngs})
	}
	dir := h.cfg.artifacts
	resolve := func(path string) string {
		if filepath.IsAbs(path) {
			return path
		}
		if dir == "" {
			dir = h.tb.ArtifactDir()
		}
		return filepath.Join(dir, path)
	}
	for i := range outputs {
		outputs[i].Path = resolve(outputs[i].Path)
	}
	p.Outputs = outputs

	var errs []error
	if len(p.Outputs) > 0 {
		errs = append(errs, h.exportTimeline(end, p))
	}
	for _, m := range marks {
		if m.stills&StillSnapshot == 0 {
			continue
		}
		name := strings.NewReplacer(
			"{"+varName+"}", p.Vars[varName],
			"{"+varStatus+"}", p.Vars[varStatus],
			"{mark}", m.label,
		).Replace(stillPath)
		errs = append(errs, writeSnapshot(resolve(name+".snap"), m.snap))
	}
	return errors.Join(errs...)
}

// exportTimeline exports the session's timeline up to the mark labeled end,
// or all of it without one.
func (h *Harness) exportTimeline(end string, p reel.Profile) error {
	tl, err := h.session.Timeline().Between("", end)
	if err != nil {
		return err
	}
	// The test's context is canceled by the time cleanups run.
	return reel.Export(context.Background(), tl, p)
}

// marksUpTo returns the hook marks placed up to the one labeled end, it
// included, or every one without it.
func (h *Harness) marksUpTo(end string) []hookMark {
	h.marksMu.Lock()
	defer h.marksMu.Unlock()
	for i, m := range h.marks {
		if m.label == end {
			return slices.Clone(h.marks[:i+1])
		}
	}
	return slices.Clone(h.marks)
}

func writeSnapshot(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		return fmt.Errorf("snapshot %q: %w", path, err)
	}
	return nil
}

// testStatus returns whether the test has failed, or is panicking, and
// whether it is panicking. A test that panics runs its cleanups before it is
// marked failed (see [panicking]).
func (h *Harness) testStatus() (failed, panicked bool) {
	panicked = panicking()
	return panicked || h.tb.Failed(), panicked
}

// panicking reports whether the calling goroutine is panicking. A test that
// panics runs its cleanups, [Harness.Close] among them, before the testing
// package marks it failed, so [testing.TB.Failed] does not tell yet. A
// deferred function that recovers a panic is still panicking until it
// returns, so a Close it calls reports a failure too.
func panicking() bool {
	pc := make([]uintptr, 128)
	frames := runtime.CallersFrames(pc[:runtime.Callers(2, pc)])
	for {
		f, more := frames.Next()
		if f.Function == "runtime.gopanic" {
			return true
		}
		if !more {
			return false
		}
	}
}

// normalizeTestName replaces every character of a test name but letters and
// digits with "_", and trims them from the ends, for {name}.
func normalizeTestName(name string) string {
	s := strings.Trim(strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return '_'
	}, name), "_")
	if s == "" {
		return "test"
	}
	return s
}
