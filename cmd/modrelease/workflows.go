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
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/google/go-github/v93/github"
)

// githubTagEventLimit is the most tags one push can have for GitHub to still
// create their events. Past it, no workflow starts for any of them.
const githubTagEventLimit = 3

var githubRepoRE = regexp.MustCompile(`(?i)^(?:(?:https?|ssh)://(?:[^@/]+@)?github\.com(?::\d+)?/|[^@/:]+@github\.com:)([\w.-]+/[\w.-]+?)(?:\.git)?/?$`)

// githubRepo returns the owner/name of the GitHub repository a git URL points
// at, or "" if it isn't one.
func githubRepo(gitURL string) string {
	if m := githubRepoRE.FindStringSubmatch(gitURL); m != nil {
		return m[1]
	}
	return ""
}

// workflowRun is a run of a workflow that one of the release's tags would start,
// if GitHub created the tag's event.
type workflowRun struct {
	file, tag      string
	readsEventName bool // It may act on the push event, which a dispatched run isn't.
}

// githubClient returns a GitHub API client, with the token gh and GitHub Actions
// use. The plan's client is shared, so use release.client instead.
func (r *release) githubClient() (*github.Client, error) {
	token := cmp.Or(os.Getenv("GH_TOKEN"), os.Getenv("GITHUB_TOKEN"))
	if token == "" {
		return nil, errors.New("set GH_TOKEN or GITHUB_TOKEN to a GitHub token for the repository, such as $(gh auth token)")
	}
	opts := []github.ClientOptionsFunc{github.WithAuthToken(token), github.WithTimeout(time.Minute)}
	if r.githubAPI != "" {
		opts = append(opts, github.WithURLs(&r.githubAPI, &r.githubAPI))
	}
	return github.NewClient(opts...)
}

// checkWorkflows works out which workflow runs GitHub won't start because the
// push has too many tags, and checks that the API can start each one instead.
func (r *release) checkWorkflows(ctx context.Context, p *plan, pass, fail func(string, ...any)) {
	if r.flags.NoDispatch || p.github == "" || len(p.modules) <= githubTagEventLimit {
		return
	}
	workflows, err := readWorkflows(p.root)
	if err != nil {
		fail("couldn't read the workflows, to find those the tags start: %v", err)
		return
	}
	owner, name, _ := strings.Cut(p.github, "/")
	api, apiErr := r.client(p)
	ok := true
	for _, w := range workflows {
		matched := len(p.runs)
		for _, m := range p.modules {
			if w.tags.match(m.tag) {
				p.runs = append(p.runs, workflowRun{file: w.file, tag: m.tag, readsEventName: w.readsEventName})
			}
		}
		if len(p.runs) == matched {
			continue
		}
		if w.undispatchable != "" {
			ok = false
			fail("%s runs on these tags, but GitHub won't start it for a push of %d tags, and %s. "+
				"Fix that, or use --no-dispatch", w.file, len(p.modules), w.undispatchable)
			continue
		}
		if apiErr != nil {
			ok = false
			fail("can't look up %s on GitHub, which starts it after the push: %v", w.file, apiErr)
			continue
		}
		found, _, lookupErr := api.Actions.GetWorkflowByFileName(ctx, owner, name, url.PathEscape(w.file))
		switch {
		case lookupErr != nil:
			ok = false
			fail("couldn't look up %s on GitHub, which starts it after the push: %v", w.file, lookupErr)
		case found.GetState() != "active":
			ok = false
			fail("%s is %s on GitHub, so it can't be started after the push", w.file, found.GetState())
		}
	}
	if ok && len(p.runs) > 0 {
		pass("the GitHub API can start the %d workflow runs GitHub won't for a push of %d tags", len(p.runs), len(p.modules))
	}
}

// workflow is the part of a GitHub Actions workflow that decides whether a tag
// starts it.
type workflow struct {
	file           string
	tags           tagFilter
	undispatchable string // Why the API can't start it, or "" if it can.
	readsEventName bool
}

// readWorkflows reads the repository's GitHub Actions workflows, if it has any.
func readWorkflows(root string) ([]workflow, error) {
	dir := filepath.Join(root, ".github", "workflows")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var workflows []workflow
	for _, entry := range entries {
		if ext := filepath.Ext(entry.Name()); entry.IsDir() || (ext != ".yml" && ext != ".yaml") {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(dir, entry.Name()))
		if readErr != nil {
			return nil, readErr
		}
		w, parseErr := parseWorkflow(entry.Name(), data)
		if parseErr != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), parseErr)
		}
		workflows = append(workflows, w)
	}
	return workflows, nil
}

// parseWorkflow reads a workflow's triggers. The on key can be one event, a
// list of them, or a map of each to its filters.
func parseWorkflow(file string, data []byte) (workflow, error) {
	var doc struct {
		On any `yaml:"on"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return workflow{}, err
	}
	w := workflow{file: file, readsEventName: bytes.Contains(data, []byte("github.event_name"))}

	events := map[string]any{}
	switch on := doc.On.(type) {
	case string:
		events[on] = nil
	case []any:
		for _, event := range on {
			events[fmt.Sprint(event)] = nil
		}
	case map[string]any:
		events = on
	}
	w.undispatchable = whyNotDispatchable(events)
	push, ok := events["push"]
	if !ok {
		return w, nil
	}

	// With only branch filters, a push of a tag doesn't start it, and with no
	// filters at all, every push does.
	filters, _ := push.(map[string]any)
	tags, hasTags := filters["tags"]
	ignore, hasIgnore := filters["tags-ignore"]
	_, hasBranches := filters["branches"]
	_, hasBranchesIgnore := filters["branches-ignore"]
	var err error
	switch {
	case hasTags:
		w.tags, err = newTagFilter(tags, false)
	case hasIgnore:
		w.tags, err = newTagFilter(ignore, true)
	case !hasBranches && !hasBranchesIgnore:
		w.tags = tagFilter{ignore: true} // Ignores nothing, so matches every tag.
	}
	return w, err
}

// whyNotDispatchable says why a dispatch, which passes no inputs, can't start
// a workflow with these events, or returns "" if it can.
func whyNotDispatchable(events map[string]any) string {
	dispatch, ok := events["workflow_dispatch"]
	if !ok {
		return "it has no workflow_dispatch trigger to start it with"
	}
	config, _ := dispatch.(map[string]any)
	inputs, _ := config["inputs"].(map[string]any)
	for _, name := range slices.Sorted(maps.Keys(inputs)) {
		input, _ := inputs[name].(map[string]any)
		if _, hasDefault := input["default"]; fmt.Sprint(input["required"]) == "true" && !hasDefault {
			return fmt.Sprintf("its workflow_dispatch input %s is required, with no default", name)
		}
	}
	return ""
}

// tagFilter is the tags or tags-ignore filter of a push trigger. Its zero value
// matches no tags.
type tagFilter struct {
	patterns []tagPattern
	ignore   bool // Matching tags don't start it, rather than do.
}

type tagPattern struct {
	re     *regexp.Regexp
	negate bool
}

// newTagFilter compiles a filter, which is one pattern or a list of them.
func newTagFilter(value any, ignore bool) (tagFilter, error) {
	var patterns []string
	switch v := value.(type) {
	case string:
		patterns = []string{v}
	case []any:
		for _, pattern := range v {
			patterns = append(patterns, fmt.Sprint(pattern))
		}
	}
	f := tagFilter{ignore: ignore}
	for _, pattern := range patterns {
		rest, negate := strings.CutPrefix(pattern, "!")
		re, err := compileFilterPattern(rest)
		if err != nil {
			return tagFilter{}, fmt.Errorf("tag filter %q: %w", pattern, err)
		}
		f.patterns = append(f.patterns, tagPattern{re: re, negate: negate})
	}
	return f, nil
}

// match reports whether a push of the tag starts the workflow. As on GitHub, a
// later pattern overrides an earlier one, so a negated pattern can exclude tags
// an earlier one included.
func (f tagFilter) match(tag string) bool {
	matched := false
	for _, p := range f.patterns {
		if p.re.MatchString(tag) {
			matched = !p.negate
		}
	}
	return matched != f.ignore
}

// compileFilterPattern compiles one of GitHub's filter patterns, where * matches
// anything but a slash, ** matches anything, ? and + repeat the character before
// them as in a regular expression, [] is a character class, and \ escapes.
func compileFilterPattern(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch c := pattern[i]; c {
		case '*':
			if strings.HasPrefix(pattern[i:], "**") {
				b.WriteString(".*")
				i++
			} else {
				b.WriteString("[^/]*")
			}
		case '?', '+':
			b.WriteByte(c)
		case '[':
			end := strings.IndexByte(pattern[i:], ']')
			if end < 0 {
				return nil, errors.New("unclosed [")
			}
			b.WriteString(pattern[i : i+end+1])
			i += end
		case '\\':
			if i+1 < len(pattern) {
				i++
				b.WriteString(regexp.QuoteMeta(pattern[i : i+1]))
			}
		default:
			b.WriteString(regexp.QuoteMeta(pattern[i : i+1]))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}
