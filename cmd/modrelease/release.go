// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/google/go-github/v93/github"
)

const (
	// defaultPublicProxy is the proxy the published hashes are checked against.
	defaultPublicProxy = "https://proxy.golang.org"
	// publishTimeout limits everything from the first signature to the last check.
	publishTimeout = 15 * time.Minute
	// confirmTimeout limits the work after publishing fails, which may be because
	// it used up all of publishTimeout.
	confirmTimeout = time.Minute
)

// pipeWait limits how long to wait for a command's output after it exits or is
// canceled, since a child like ssh can hold its pipes open indefinitely.
var pipeWait = 5 * time.Second

// errReported is returned when the output already explains the failure.
var errReported = errors.New("failure already reported")

// release is one run of the tool.
type release struct {
	flags Flags
	out   io.Writer
	debug bool
	dir   string // Directory to start in, or "" for the current one.

	// hermeticGitConfig is the global git config for everything that has to
	// match what the public proxy sees. It stays os.DevNull outside of tests,
	// so a local LFS filter can't change a module's hash.
	hermeticGitConfig string
	// publicProxy is the proxy to check after publishing, or "" to not check.
	publicProxy string
	// proxyWait is how long to wait between attempts to fetch from publicProxy.
	proxyWait time.Duration
	// github is the owner/name of the GitHub repository being released, or "" to
	// work it out from the push URL. Tests set it, since their remote is local.
	github string
	// githubAPI is the GitHub API's base URL, or "" for api.github.com.
	githubAPI string
}

// plan is what a release will do, worked out from the repository.
type plan struct {
	root, branch, head string
	pushURL            string
	prefix             string // Module path of the repository, with no major version.
	current, version   string
	modules            []*goModule // In dependency order.
	skipped            []skippedModule
	github             string         // owner/name, if the push goes to GitHub.
	runs               []workflowRun  // Workflow runs GitHub won't start, as the push has too many tags.
	api                *github.Client // Starts runs, if there are any.
}

// run does the release, up to and including the rehearsal unless --apply is set.
func (r *release) run(ctx context.Context) error {
	root, err := r.git(ctx, r.dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return errors.New("not in a git repository")
	}
	mode := "plan only, nothing will change"
	if r.flags.Apply {
		mode = "publishing"
	}
	r.printf("modrelease: %s (%s)\n", root, mode)

	p, checks := r.inspect(ctx, root)
	r.step(1, "Check the repository")
	failed := 0
	for _, c := range checks {
		if c.ok {
			r.linef("ok   %s", c.text)
		} else {
			r.linef("FAIL %s", c.text)
			failed++
		}
	}
	if failed > 0 {
		r.printf("\n%d checks failed. Nothing changed.\n", failed)
		return errReported
	}

	r.step(2, "Bump the version")
	r.linef("%s -> %s", cmp.Or(p.current, "(no version tags yet)"), p.version)

	r.step(3, fmt.Sprintf("Release %d modules, in dependency order", len(p.modules)))
	width := 0
	for _, m := range p.modules {
		width = max(width, len(m.dir))
	}
	for _, m := range p.modules {
		r.linef("%-*s  %s", width, m.dir, m.path)
	}
	for _, s := range p.skipped {
		r.linef("skip %s (%s)", s.dir, s.reason)
	}

	tmp, err := os.MkdirTemp("", "modrelease-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	r.step(4, "Update each module and rehearse it, with no workspace")
	tree, cleanup, err := r.rehearse(ctx, p, tmp)
	defer cleanup()
	if err != nil {
		r.printf("\nRehearsal failed. Nothing changed.\n")
		return err
	}

	message := "chore(release): " + p.version
	r.step(5, "Create one signed, signed-off commit")
	r.linef("%s", message)
	r.step(6, fmt.Sprintf("Create %d signed tags at that commit", len(p.modules)))
	for _, m := range p.modules {
		r.linef("%s", m.tag)
	}
	r.step(7, "Push atomically to "+r.flags.Remote)
	r.linef("refs/heads/%s and all %d tags, or nothing", p.branch, len(p.modules))
	switch {
	case p.github == "" || len(p.modules) <= githubTagEventLimit:
	case r.flags.NoDispatch:
		r.linef("GitHub creates no tag events for a push of more than %d tags, so workflows that run on tags "+
			"won't start, and --no-dispatch leaves them", githubTagEventLimit)
	case len(p.runs) > 0:
		r.linef("then start the workflow runs GitHub won't for a push of more than %d tags:", githubTagEventLimit)
		for _, run := range p.runs {
			r.linef("  %s on %s", run.file, run.tag)
			if run.readsEventName {
				r.linef("  warning: %s reads github.event_name, which is workflow_dispatch for this run, not push", run.file)
			}
		}
	}
	r.step(8, "Check the published hashes against step 4")
	r.linef("direct from git, then the public proxy and checksum database")

	if !r.flags.Apply {
		r.printf("\nNothing changed. Run again with --apply to do steps 5 to 8.\n")
		return nil
	}

	r.printf("\nPublishing:\n")
	// A release that started publishing runs to the end, so an interrupt can't
	// leave it between the push and the check of what the push did. The timeout
	// stops a stalled command from hanging it forever.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), publishTimeout)
	defer cancel()
	return r.finish(ctx, p, tree, tmp, message)
}

// finish publishes the release and checks the result.
func (r *release) finish(ctx context.Context, p *plan, tree, tmp, message string) error {
	if err := r.publish(ctx, p, tree, message); err != nil {
		return err
	}
	notStarted := r.dispatch(ctx, p)
	complete, err := r.verify(ctx, p, tmp)
	if err != nil {
		return err
	}
	var gaps []string
	if !complete {
		gaps = append(gaps, "not every hash could be checked")
	}
	if notStarted > 0 {
		gaps = append(gaps, fmt.Sprintf("%d workflow runs didn't start", notStarted))
	}
	if len(gaps) == 0 {
		r.printf("\nReleased %s.\n", p.version)
		return nil
	}
	r.printf("\nReleased %s, but %s. See the warnings above.\n", p.version, strings.Join(gaps, ", and "))
	if notStarted > 0 {
		return errReported
	}
	return nil
}

// dispatch starts the workflow runs GitHub won't, and returns how many didn't
// start. The release is out by then, so a failure only says where to retry.
func (r *release) dispatch(ctx context.Context, p *plan) (notStarted int) {
	owner, name, _ := strings.Cut(p.github, "/")
	for _, run := range p.runs {
		_, _, err := p.api.Actions.CreateWorkflowDispatchEventByFileName(ctx, owner, name, url.PathEscape(run.file),
			github.CreateWorkflowDispatchEventRequest{Ref: run.tag})
		if err != nil {
			r.linef("warning: couldn't start %s on %s, so run it on that tag from https://github.com/%s/actions/workflows/%s: %v",
				run.file, run.tag, p.github, url.PathEscape(run.file), err)
			notStarted++
		} else {
			r.linef("started %s on %s", run.file, run.tag)
		}
	}
	return notStarted
}

func (r *release) printf(format string, args ...any) {
	fmt.Fprintf(r.out, format, args...)
}

func (r *release) step(n int, title string) {
	r.printf("\n  %d. %s\n", n, title)
}

func (r *release) linef(format string, args ...any) {
	r.printf("       "+format+"\n", args...)
}

// cmdRun runs a command in dir and returns its trimmed stdout. A failure's error
// includes the end of the command's output, since that's usually the cause.
func (r *release) cmdRun(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	if r.debug {
		fmt.Fprintf(os.Stderr, "debug: (%s) %s %s\n", cmp.Or(dir, "."), name, strings.Join(args, " "))
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = pipeWait
	// exec.ErrWaitDelay means the command succeeded, and a child outlived it.
	if err := cmd.Run(); err != nil && !errors.Is(err, exec.ErrWaitDelay) {
		return strings.TrimSpace(stdout.String()), fmt.Errorf("%s %s: %w\n%s",
			name, strings.Join(args, " "), err, tailLines(stderr.String()+"\n"+stdout.String(), 40))
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (r *release) git(ctx context.Context, dir string, args ...string) (string, error) {
	return r.cmdRun(ctx, dir, nil, "git", args...)
}

// tailLines returns at most the last n lines of s.
func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.Join(lines[max(0, len(lines)-n):], "\n")
}
