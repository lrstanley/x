// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"cmp"
	"context"
	"fmt"
	"go/build"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/template"

	"github.com/google/go-github/v93/github"
	"golang.org/x/mod/semver"
)

// maxCommitPages limits how many pages of commits are read from the API to find
// the authors' accounts.
const maxCommitPages = 10

// client returns the plan's GitHub API client, creating it the first time.
func (r *release) client(p *plan) (*github.Client, error) {
	if p.api == nil {
		api, err := r.githubClient()
		if err != nil {
			return nil, err
		}
		p.api = api
	}
	return p.api, nil
}

// checkGitHubRelease checks that the GitHub release can be created, and writes
// its notes, so the plan can show them.
func (r *release) checkGitHubRelease(ctx context.Context, p *plan, tags []string, remote map[string]string, pass, fail func(string, ...any)) {
	if !r.flags.GitHubRelease {
		return
	}
	if p.github == "" {
		fail("--github-release needs %s to be a GitHub repository", r.flags.Remote)
		return
	}
	api, err := r.client(p)
	if err != nil {
		fail("can't create the GitHub release: %v", err)
		return
	}
	owner, name, _ := strings.Cut(p.github, "/")
	if _, _, err = api.Repositories.Get(ctx, owner, name); err != nil {
		fail("couldn't look up %s on GitHub, to create its release: %v", p.github, err)
		return
	}

	n, err := r.releaseNotes(ctx, p, tags, remote)
	if err != nil {
		fail("%v", err)
		return
	}
	if err = r.resolveLogins(ctx, api, p, remote[n.previous], n.changes); err != nil {
		p.notesWarning = fmt.Sprintf("authors are credited by name, since GitHub couldn't say which accounts they are: %v", err)
	}
	p.notes = n.render()
	pass("can create GitHub release %s, with %d changes since %s", p.version, len(n.changes), cmp.Or(n.previous, "the first commit"))
}

// releaseNotes works out everything in the notes except the authors' accounts.
func (r *release) releaseNotes(ctx context.Context, p *plan, tags []string, remote map[string]string) (*notes, error) {
	n := &notes{repo: p.github, version: p.version, previous: previousVersion(tags, p.version)}

	revs := p.head
	if n.previous != "" {
		base := remote[n.previous]
		if _, err := r.git(ctx, p.root, "cat-file", "-e", base+"^{commit}"); err != nil {
			return nil, fmt.Errorf("the commit %s is at isn't in the local history, which the release notes come from. "+
				"Run git fetch --tags %s", n.previous, r.flags.Remote)
		}
		revs = base + ".." + p.head
	}
	const sep, end = "\x1f", "\x1e"
	out, err := r.git(ctx, p.root, "log", "--no-merges", "--reverse", "--format=%H"+sep+"%an"+sep+"%ae"+sep+"%s"+sep+"%b"+end, revs)
	if err != nil {
		return nil, fmt.Errorf("couldn't read the commits for the release notes: %w", err)
	}
	var changes []*change
	for record := range strings.SplitSeq(out, end) {
		fields := strings.Split(strings.TrimLeft(record, "\n"), sep)
		if len(fields) == 5 {
			changes = append(changes, parseChange(fields[0], fields[1], fields[2], fields[3], fields[4]))
		}
	}
	n.changes = filterChanges(changes)

	for _, m := range p.modules {
		pkg, importErr := build.ImportDir(filepath.Join(p.root, filepath.FromSlash(m.dir)), 0)
		n.modules = append(n.modules, releaseModule{path: m.path, command: importErr == nil && pkg.IsCommand()})
	}
	slices.SortFunc(n.modules, func(a, b releaseModule) int { return strings.Compare(a.path, b.path) })

	if r.flags.Highlights != "" {
		data, readErr := os.ReadFile(r.flags.Highlights)
		if readErr != nil {
			return nil, fmt.Errorf("couldn't read --highlights: %w", readErr)
		}
		n.highlights = string(data)
	}
	if n.footer, err = r.footer(ctx, p, n); err != nil {
		return nil, err
	}
	return n, nil
}

// footer renders the footer template, from --footer or the default.
func (r *release) footer(ctx context.Context, p *plan, n *notes) (string, error) {
	text, source := defaultFooter, "the default footer"
	if r.flags.Footer != "" {
		data, err := os.ReadFile(r.flags.Footer)
		if err != nil {
			return "", fmt.Errorf("couldn't read --footer: %w", err)
		}
		text, source = string(data), r.flags.Footer
	}
	tmpl, err := template.New("footer").Option("missingkey=error").Parse(text)
	if err != nil {
		return "", fmt.Errorf("parsing %s: %w", source, err)
	}

	data := &footerData{
		Repo:       p.github,
		URL:        "https://github.com/" + p.github,
		Version:    p.version,
		Previous:   n.previous,
		Branch:     p.branch,
		Prerelease: semver.Prerelease(p.version) != "",
	}
	files, err := r.git(ctx, p.root, "ls-files", "--", ":(glob)*", ":(glob).github/*", ":(glob)docs/*")
	if err != nil {
		return "", fmt.Errorf("couldn't list the community files for the footer: %w", err)
	}
	data.setCommunityFiles(strings.Split(files, "\n"))

	var b strings.Builder
	if err = tmpl.Execute(&b, data); err != nil {
		return "", fmt.Errorf("rendering %s: %w", source, err)
	}
	return b.String(), nil
}

// resolveLogins finds the GitHub account of each change's author. A noreply
// address names it, and otherwise the API knows which account made a commit.
// Commits it doesn't cover get the account of another commit from the same
// address. Only the commits since base are read, or a limited number when
// there's no base.
func (r *release) resolveLogins(ctx context.Context, api *github.Client, p *plan, base string, changes []*change) error {
	byEmail := map[string]string{}
	missing := map[string]*change{}
	for _, c := range changes {
		if m := noreplyRE.FindStringSubmatch(c.email); m != nil {
			c.login = m[1]
			byEmail[strings.ToLower(c.email)] = c.login
		} else {
			missing[c.sha] = c
		}
	}

	owner, name, _ := strings.Cut(p.github, "/")
	opts := &github.CommitsListOptions{SHA: p.head, PerPage: 100}
	var err error
pages:
	for page := 0; page < maxCommitPages && len(missing) > 0; page++ {
		var list []*github.RepositoryCommit
		var resp *github.Response
		if list, resp, err = api.Repositories.ListCommits(ctx, owner, name, opts); err != nil {
			break
		}
		for _, rc := range list {
			if rc.GetSHA() == base {
				break pages
			}
			login := rc.GetAuthor().GetLogin()
			if login == "" {
				continue
			}
			if c := missing[rc.GetSHA()]; c != nil {
				c.login = login
				delete(missing, rc.GetSHA())
			}
			if email := strings.ToLower(rc.GetCommit().GetAuthor().GetEmail()); byEmail[email] == "" {
				byEmail[email] = login
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	for _, c := range missing {
		c.login = byEmail[strings.ToLower(c.email)]
	}
	return err
}

// createGitHubRelease creates the release on GitHub, and reports whether it
// did. The tags are out by then, so a failure saves the notes and says how to
// create it by hand.
func (r *release) createGitHubRelease(ctx context.Context, p *plan) bool {
	owner, name, _ := strings.Cut(p.github, "/")
	prerelease := semver.Prerelease(p.version) != ""
	created, _, err := p.api.Repositories.CreateRelease(ctx, owner, name, github.CreateReleaseRequest{
		TagName:    p.version,
		Name:       new(p.version),
		Body:       new(p.notes),
		Prerelease: new(prerelease),
		MakeLatest: new(strconv.FormatBool(!prerelease)), // A prerelease isn't the latest release.
	})
	if err == nil {
		r.linef("%s %s", styleOK.Render("created"), cmp.Or(created.GetHTMLURL(), "GitHub release "+p.version))
		return true
	}

	command := fmt.Sprintf("gh release create %s --repo %s --verify-tag --title %s", p.version, p.github, p.version)
	if prerelease {
		command += " --prerelease"
	}
	file, saveErr := r.saveNotes(p)
	if saveErr != nil {
		r.linef("%s couldn't create the GitHub release %s, or save its notes (%v), so create it with %s: %v",
			warning, p.version, saveErr, command, err)
		return false
	}
	r.linef("%s couldn't create the GitHub release %s, so create it with %s: %v",
		warning, p.version, command+" --notes-file "+file, err)
	return false
}

// saveNotes writes the release notes to a new file, and returns its path.
func (r *release) saveNotes(p *plan) (string, error) {
	f, err := os.CreateTemp("", "modrelease-"+p.version+"-*.md")
	if err != nil {
		return "", err
	}
	if _, err = f.WriteString(p.notes); err != nil {
		_ = f.Close()
		return "", err
	}
	return f.Name(), f.Close()
}
