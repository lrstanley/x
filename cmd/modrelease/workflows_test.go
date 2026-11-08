// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestGitHubRepo(t *testing.T) {
	for url, want := range map[string]string{
		"git@github.com:lrstanley/steep.git":       "lrstanley/steep",
		"git@github.com:lrstanley/steep":           "lrstanley/steep",
		"ssh://git@github.com/lrstanley/steep.git": "lrstanley/steep",
		"ssh://git@github.com:22/lrstanley/steep":  "lrstanley/steep",
		"https://github.com/lrstanley/steep.git":   "lrstanley/steep",
		"https://user@GitHub.com/lrstanley/steep/": "lrstanley/steep",
		"https://github.com/lrstanley/my.repo.git": "lrstanley/my.repo",
		"https://gitlab.com/lrstanley/steep.git":   "",
		"https://github.com.evil/lrstanley/steep":  "",
		"https://github.com/lrstanley":             "",
		"file:///tmp/github.com/lrstanley/steep":   "",
	} {
		if got := githubRepo(url); got != want {
			t.Errorf("githubRepo(%q) = %q, want %q", url, got, want)
		}
	}
}

func TestTagFilter(t *testing.T) {
	for _, tt := range []struct {
		patterns []any
		ignore   bool
		tag      string
		want     bool
	}{
		{[]any{"v*"}, false, "v1.2.3", true},
		{[]any{"v*"}, false, "reel/v1.2.3", false},
		{[]any{"**"}, false, "cmd/reel/v1.2.3", true},
		{[]any{"*/v*"}, false, "reel/v1.2.3", true},
		{[]any{"*/v*"}, false, "cmd/reel/v1.2.3", false},
		{[]any{"v[0-9]+.*"}, false, "v12.0.0", true},
		{[]any{"v[0-9]+.*"}, false, "vx.0.0", false},
		{[]any{"v1.?"}, false, "v1", true},
		{[]any{"v*", "!v*-*"}, false, "v1.2.3-rc.0", false},
		{[]any{"v*", "!v*-*", "v*-rc.*"}, false, "v1.2.3-rc.0", true},
		{[]any{`v\*`}, false, "v*", true},
		{[]any{`v\*`}, false, "v1", false},
		{[]any{"reel/*"}, true, "reel/v1.2.3", false},
		{[]any{"reel/*"}, true, "v1.2.3", true},
		{nil, false, "v1.2.3", false},
	} {
		f, err := newTagFilter(tt.patterns, tt.ignore)
		if err != nil {
			t.Fatalf("%v: %v", tt.patterns, err)
		}
		if got := f.match(tt.tag); got != tt.want {
			t.Errorf("%v (ignore %v) on %s = %v, want %v", tt.patterns, tt.ignore, tt.tag, got, tt.want)
		}
	}
	if _, err := newTagFilter("v[0-9", false); err == nil {
		t.Error("an unclosed [ compiled")
	}
}

func TestParseWorkflow(t *testing.T) {
	for _, tt := range []struct {
		name, on     string
		startsOn     string // A tag the workflow should start on, or "".
		dispatchable bool
	}{
		{"required input", "on:\n  push:\n  workflow_dispatch:\n    inputs:\n      target:\n        required: true", "anything", false},
		{"required input with a default", "on:\n  push:\n  workflow_dispatch:\n    inputs:\n      target:\n        required: true\n        default: x", "anything", true},
		{"one event", "on: push", "anything", false},
		{"list", "on: [push, workflow_dispatch]", "anything", true},
		{"no filters", "on:\n  push:\n  workflow_dispatch: {}", "anything", true},
		{"tags", "on:\n  push:\n    tags: [v*]", "v1.0.0", false},
		{"one tag pattern", "on:\n  push:\n    tags: v*", "v1.0.0", false},
		{"tags and branches", "on:\n  push:\n    branches: [master]\n    tags: [v*]", "v1.0.0", false},
		{"tags-ignore", "on:\n  push:\n    tags-ignore: [reel/*]", "v1.0.0", false},
		{"branches only", "on:\n  push:\n    branches: [master]", "", false},
		{"branches-ignore only", "on:\n  push:\n    branches-ignore: [wip]", "", false},
		{"paths only", "on:\n  push:\n    paths: [go.mod]", "anything", false},
		{"no push", "on:\n  pull_request:\n  workflow_dispatch:", "", true},
	} {
		w, err := parseWorkflow("w.yml", []byte(tt.on+"\njobs: {}\n"))
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if dispatchable := w.undispatchable == ""; dispatchable != tt.dispatchable {
			t.Errorf("%s: dispatchable = %v, want %v (%s)", tt.name, dispatchable, tt.dispatchable, w.undispatchable)
		}
		switch {
		case tt.startsOn == "" && w.tags.match("v1.0.0"):
			t.Errorf("%s: starts on tags", tt.name)
		case tt.startsOn != "" && !w.tags.match(tt.startsOn):
			t.Errorf("%s: doesn't start on %s", tt.name, tt.startsOn)
		}
	}
}

// fakeGitHub is a GitHub API for the fixture's repository, where every workflow
// is active. It records the dispatches it gets, as file@ref, and fails them when
// failDispatches is set.
type fakeGitHub struct {
	failDispatches bool

	mu         sync.Mutex
	dispatches []string
}

func (g *fakeGitHub) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Header.Get("Authorization") != "Bearer test-token" {
		http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
		return
	}
	rest, ok := strings.CutPrefix(req.URL.Path, "/repos/lrstanley/fixture/actions/workflows/")
	file, dispatch := strings.CutSuffix(rest, "/dispatches")
	switch {
	case ok && !dispatch && req.Method == http.MethodGet:
		fmt.Fprint(w, `{"state":"active"}`)
	case ok && dispatch && req.Method == http.MethodPost:
		var body struct {
			Ref string `json:"ref"`
		}
		if err := json.UnmarshalRead(req.Body, &body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		g.mu.Lock()
		g.dispatches = append(g.dispatches, file+"@"+body.Ref)
		g.mu.Unlock()
		if g.failDispatches {
			http.Error(w, `{"message":"Server Error"}`, http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	}
}

// newDispatchFixture is a fixture on GitHub with four modules, so its release
// has more tags than GitHub creates events for.
func newDispatchFixture(t *testing.T, release string, failDispatches bool) (*fixture, *fakeGitHub) {
	t.Helper()
	f := newFixture(t)
	f.github = "lrstanley/fixture"
	api := &fakeGitHub{failDispatches: failDispatches}
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	f.githubAPI = server.URL + "/"
	t.Setenv("GH_TOKEN", "test-token")
	t.Setenv("GITHUB_TOKEN", "")

	f.writeRepo("tools/go.mod", "module "+fixtureModule+"/tools\n\ngo 1.25\n")
	f.writeRepo("tools/tools.go", "package tools\n")
	f.writeRepo(".github/workflows/release.yml", release)
	f.writeRepo(".github/workflows/test.yml", "on:\n  push:\n    branches: [master]\njobs: {}\n")
	f.commitAndPush("add tools and workflows")
	return f, api
}

const dispatchableRelease = "on:\n  push:\n    tags: [v*]\n  workflow_dispatch: {}\njobs: {}\n"

func TestDispatchPlan(t *testing.T) {
	f, api := newDispatchFixture(t, dispatchableRelease, false)

	out := f.mustRun(Flags{Bump: "v0.1.0"})
	for _, want := range []string{
		"ok   the GitHub API can start the 1 workflow runs GitHub won't for a push of 4 tags",
		"then start the workflow runs GitHub won't for a push of more than 3 tags:",
		"release.yml on v0.1.0",
		"8. Check the published hashes",
		"steps 5 to 8",
	} {
		assertContains(t, out, want)
	}
	if len(api.dispatches) > 0 {
		t.Errorf("a plan started workflow runs: %v", api.dispatches)
	}
}

func TestDispatchChecks(t *testing.T) {
	f, _ := newDispatchFixture(t, "on:\n  push:\n    tags: [v*]\njobs: {}\n", false)

	out, err := f.run(Flags{Bump: "v0.1.0"})
	if !errors.Is(err, errReported) {
		t.Fatalf("got %v, want a failed check\n%s", err, out)
	}
	assertContains(t, out, "FAIL release.yml runs on these tags, but GitHub won't start it for a push of 4 tags, "+
		"and it has no workflow_dispatch trigger")

	out = f.mustRun(Flags{Bump: "v0.1.0", NoDispatch: true})
	assertContains(t, out, "--no-dispatch leaves them")

	f.writeRepo(".github/workflows/release.yml", dispatchableRelease)
	f.commitAndPush("make release.yml dispatchable")
	t.Setenv("GH_TOKEN", "")
	out, err = f.run(Flags{Bump: "v0.1.0"})
	if !errors.Is(err, errReported) {
		t.Fatalf("got %v, want a failed check without a token\n%s", err, out)
	}
	assertContains(t, out, "FAIL can't look up release.yml on GitHub, which starts it after the push: set GH_TOKEN or GITHUB_TOKEN")

	t.Setenv("GITHUB_TOKEN", "wrong")
	out, err = f.run(Flags{Bump: "v0.1.0"})
	if !errors.Is(err, errReported) {
		t.Fatalf("got %v, want a failed check with a bad token\n%s", err, out)
	}
	assertContains(t, out, "FAIL couldn't look up release.yml on GitHub")
}

func TestDispatchAfterPush(t *testing.T) {
	f, api := newDispatchFixture(t, dispatchableRelease+"# if: github.event_name == 'push'\n", false)

	out := f.mustRun(Flags{Bump: "v0.1.0", Apply: true})
	assertContains(t, out, "warning: release.yml reads github.event_name")
	assertContains(t, out, "started release.yml on v0.1.0")
	assertContains(t, out, "Released v0.1.0.")
	if want := []string{"release.yml@v0.1.0"}; !slices.Equal(api.dispatches, want) {
		t.Errorf("dispatches: got %v, want %v", api.dispatches, want)
	}
}

func TestDispatchFailureAfterPush(t *testing.T) {
	f, _ := newDispatchFixture(t, dispatchableRelease, true)

	out, err := f.run(Flags{Bump: "v0.1.0", Apply: true})
	if !errors.Is(err, errReported) {
		t.Fatalf("got %v, want the release to report the run that didn't start\n%s", err, out)
	}
	assertContains(t, out, "so run it on that tag from https://github.com/lrstanley/fixture/actions/workflows/release.yml")
	assertContains(t, out, "Released v0.1.0, but 1 workflow runs didn't start.")
	if got := len(f.remoteTagNames()); got != 4 {
		t.Errorf("got %d tags on the remote, want all 4", got)
	}
}
