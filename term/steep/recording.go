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
	exportOff exportPolicy = iota
	exportOnFailure
	exportAlways
)

// String returns the policy's name in "steep.start" annotations.
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
	// StillPNG is a PNG of the terminal at the mark, drawn with the profile's
	// Render options and only its [reel.StillEffect] effects.
	StillPNG StillKind = 1 << iota
	// StillSnapshot is a ".snap" file of [Harness.View] at the mark.
	StillSnapshot
)

// failurePrefix starts the label of the mark of each failure the harness
// reports, followed by its number, counted from 1.
const failurePrefix = "failure-"

// isFailureMark reports whether label is that of the mark of a failure:
// failurePrefix and a number, in digits alone.
func isFailureMark(label string) bool {
	n, ok := strings.CutPrefix(label, failurePrefix)
	return ok && n != "" && strings.Trim(n, "0123456789") == ""
}

// Path variables steep adds to [reel.Profile.Vars].
const (
	varName   = "name"
	varStatus = "status"
)

const (
	statusPassed = "passed"
	statusFailed = "failed"
)

// Result is a test's result, which an export passes to effects as
// [reel.Context.Meta], such as for [Outro].
type Result struct {
	// Name is the test's name, [testing.TB.Name].
	Name string
	// Failed is whether the test had failed, or was panicking, by the time
	// [Harness.Close] ran.
	Failed bool
	// Failures are the failure messages the harness reported, oldest first,
	// followed by "test panicked" for a test that panicked. The harness marks
	// each but "test panicked" as "failure-n" (see [Harness.Mark]). Failures
	// the test reports through its own [testing.TB] are not included.
	Failures []string
	// Duration is how long the harness ran, from [NewHarness] to
	// [Harness.Close].
	Duration time.Duration
}

// captureEvents are the default events of [WithStills].
var captureEvents = []HookEvent{HookInput, HookTerminal, HookProgram, HookWait, HookReady, HookClose}

// WithRecording sets what an export writes as a [reel.Profile]. It does not
// export by itself (see [WithExportAlways] and [WithExportOnFailure]). Calls
// merge with [reel.Profile.Merge]. The profile's Render options also draw
// [Harness.Image] and [Harness.ImageInto].
//
// Besides reel's path variables and the profile's Vars, output paths may use
// {name}, the test's name with everything but letters and digits replaced by
// "_", and {status}, "passed" or "failed". Vars of the same names override
// them. Relative paths are under [WithArtifacts]. Nil Outputs write
// "{name}_{status}.gif" and "{name}_{status}.cast".
//
// At export, [reel.Context.Meta] is the test's *[Result]. A mark the
// profile looks for that the test never placed does not stop the export
// (see [reel.Export]): for a failed test it is logged, and for a passing
// test it is a test error, except for "failure-n" marks, so a profile can
// pause on "failure-1" whatever the result. The recording ends where
// [Harness.Close] starts, so the program's repaint as it exits is left out.
//
// The STEEP_EXPORT_* and STEEP_RENDERER_* variables patch the profile (see
// the package documentation).
func WithRecording(p reel.Profile) Option {
	return func(cfg *options) {
		cfg.recording = cfg.recording.Merge(p)
	}
}

// WithArtifacts sets the directory relative output paths are written under,
// created when needed. A relative dir is relative to the test's package
// directory. Without it, files go to [testing.TB.ArtifactDir], which is only
// kept with "go test -artifacts".
//
// Environment: STEEP_EXPORT_DIR (overrides this option unless [WithIgnoreEnv]
// is set).
func WithArtifacts(dir string) Option {
	return func(cfg *options) {
		cfg.artifacts = dir
	}
}

// WithExportOnFailure exports the recording (see [WithRecording]) at
// [Harness.Close] if the test has failed by then. Without an export policy,
// nothing is written.
//
// Tests that panic or call [testing.TB.FailNow] still export. A test killed
// by "go test -timeout", or a panic on another goroutine, writes nothing.
//
// Environment: STEEP_EXPORT_MODE (see the package documentation).
func WithExportOnFailure() Option {
	return func(cfg *options) {
		cfg.policy = exportOnFailure
	}
}

// WithExportAlways exports the recording (see [WithRecording]) at
// [Harness.Close], whether the test passed or failed, except for a skipped
// test. See [WithExportOnFailure] for caveats and the environment override.
func WithExportAlways() Option {
	return func(cfg *options) {
		cfg.policy = exportAlways
	}
}

// WithStills writes stills of kinds at the marks of events (see
// [Harness.Mark]), one file per mark: "{name}_{status}_{mark}.png" for
// [StillPNG] and "{name}_{status}_{mark}.snap" for [StillSnapshot], such as
// "TestLogin_failed_input-2.png". Without events, it uses input, terminal,
// program, wait, ready, and close. Calls accumulate.
//
// Stills are only written when the recording is exported. For a single PNG
// of the last frame, add a ".png" output to the profile instead.
func WithStills(kinds StillKind, events ...HookEvent) Option {
	return func(cfg *options) {
		if len(events) == 0 {
			events = captureEvents
		}
		cfg.stills = addStills(cfg.stills, kinds, events)
	}
}

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

// stillPath is the path of a still, without its extension.
const stillPath = "{name}_{status}_{mark}"

// hookMark is a mark the harness placed for a hook event.
type hookMark struct {
	label  string
	stills StillKind
	// snap is the screen's text at the mark, for a [StillSnapshot].
	snap string
}

// Mark places a [reel.Mark] labeled label in the session's timeline, which
// [reel.Timeline.Between] can cut a recording at. The label must not be
// empty.
//
// The harness marks every hook event once its action is done, before its
// After hooks run, labeled with the event's name and count, such as
// "input-1" or "wait-1". While exporting, input, terminal, and program
// actions are marked once the program has painted its response: when its
// output has then been quiet for the [WithCheck] interval, or, if it writes
// nothing, after the [WithSettle] period (at least 50ms). A very large
// check interval therefore slows those actions. [Harness.Close] places its
// mark as it starts, where the recording ends.
//
// The harness also marks each failure it reports as it reports it,
// "failure-1", "failure-2", and so on, numbered as in [Result.Failures],
// such as for [effects.PauseAtMarks] to pause on a test's first failure.
// Use other labels for a test's own marks.
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

// markEvent marks event (see [Harness.Mark]) and returns the label, or ""
// once the session is closed.
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
// policy says. A skipped test that did not fail exports nothing. A failure
// to export is a test error, except for missing marks (see [WithRecording]).
func (h *Harness) exportRecording(end string) {
	h.tb.Helper()
	if h.cfg.policy == exportOff {
		return
	}
	failed, panicked := h.testStatus()
	if !failed && (h.cfg.policy == exportOnFailure || h.tb.Skipped()) {
		return
	}
	missing, err := reel.MissingMarks(h.writeRecording(end, failed, panicked))
	if err != nil {
		h.tb.Errorf("steep: export: %v", err)
	}
	for _, label := range missing {
		switch {
		case failed:
			h.tb.Logf("steep: export: the recording has no mark %q", label)
		case !isFailureMark(label):
			h.tb.Errorf("steep: export: the recording looks for a mark %q, which the test never placed", label)
		}
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

// exportTimeline exports the timeline up to the mark labeled end, or all of
// it for "".
func (h *Harness) exportTimeline(end string, p reel.Profile) error {
	tl, err := h.session.Timeline().Between("", end)
	if err != nil {
		// Without its end, nothing is written, so this is no mark the
		// outputs can do without (see [reel.MissingMarks]).
		return fmt.Errorf("the end of the recording: %v", err) //nolint:errorlint // Not a missing mark to export without.
	}
	// The test's context is canceled by the time cleanups run.
	return reel.Export(context.Background(), tl, p)
}

// marksUpTo returns the hook marks up to and including the one labeled end,
// or all of them if there is none.
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

// testStatus returns whether the test has failed or is panicking, and
// whether it is panicking.
func (h *Harness) testStatus() (failed, panicked bool) {
	panicked = panicking()
	return panicked || h.tb.Failed(), panicked
}

// panicking reports whether the calling goroutine is panicking. A panicking
// test runs its cleanups before it is marked failed, so [testing.TB.Failed]
// does not tell yet. A deferred function that recovers a panic is still
// panicking until it returns.
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

// normalizeTestName returns name for {name}: every character but letters
// and digits becomes "_", trimmed from the ends.
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
