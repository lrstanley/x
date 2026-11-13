// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"cmp"
	_ "embed"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/mod/semver"
)

// maxNotesLength is a little under the most characters GitHub allows in a
// release's notes, which is 125,000.
const maxNotesLength = 120_000

// defaultFooter is the template that ends the notes without --footer.
//
//go:embed footer.tmpl.md
var defaultFooter string

// Changelog groups, in the order the notes list them.
const (
	groupBreaking = iota
	groupSecurity
	groupFeatures
	groupFixes
	groupPerf
	groupDocs
	groupMisc
	groupDeps
)

var groupTitles = [...]string{
	groupBreaking: ":warning: Breaking Changes",
	groupSecurity: ":shield: Security Fixes",
	groupFeatures: ":sparkles: New Features",
	groupFixes:    ":lady_beetle: Bug Fixes",
	groupPerf:     ":zap: Performance",
	groupDocs:     ":books: Documentation",
	groupMisc:     ":broom: Misc",
	groupDeps:     ":package: Dependency Updates",
}

var (
	// conventionalRE matches a Conventional Commits subject, like
	// "feat(sync/cache)!: add GetOrLoad".
	conventionalRE   = regexp.MustCompile(`^([A-Za-z]+)(?:\(([^()]*)\))?(!)?:\s*(.+)$`)
	breakingFooterRE = regexp.MustCompile(`(?m)^BREAKING[ -]CHANGE:[ \t]*`)
	trailerRE        = regexp.MustCompile(`^[A-Za-z][A-Za-z-]*(: | #)`)
	revertRE         = regexp.MustCompile(`(?m)^This reverts commit ([0-9a-f]{7,40})`)
	// excludeRE matches commits that aren't changes anyone needs to read about.
	excludeRE  = regexp.MustCompile(`(?i)\[(skip ci|ci skip)\]|\bauto-(applied|update)\b|^chore\(release\):`)
	typoRE     = regexp.MustCompile(`(?i)\b(typos?|spelling)\b`)
	securityRE = regexp.MustCompile(`(?i)\b(CVE-\d{4}-\d+|GHSA(-[0-9a-z]{4}){3})\b|\bvulnerab`)
	// depsRE matches the subjects of dependency bots that don't use Conventional Commits.
	depsRE = regexp.MustCompile(`(?i)^(bump \S+ from \S+ to |update (dependency|module) )`)
	// The first word of a subject that doesn't use Conventional Commits.
	featureVerbRE = regexp.MustCompile(`(?i)^(add|adds|added|implement|implements|introduce|introduces|support|supports)\b`)
	fixVerbRE     = regexp.MustCompile(`(?i)^(fix|fixes|fixed|resolve|resolves|resolved)\b`)
	// noreplyRE matches the private address GitHub gives an account, which
	// names it.
	noreplyRE = regexp.MustCompile(`(?i)^(?:\d+\+)?([a-z0-9](?:[a-z0-9-]*[a-z0-9])?)@users\.noreply\.github\.com$`)
)

// change is one commit in a release's changelog.
type change struct {
	sha, name, email string
	login            string // GitHub account of the author, or "" if unknown.
	subject          string

	kind, scope  string // From a Conventional Commits subject, or "".
	text         string // The subject, minus its type and scope.
	breaking     bool
	breakingNote string
	reverts      string // SHA, or a prefix of it, of the commit this reverts.
}

// parseChange reads a commit's subject and body.
func parseChange(sha, name, email, subject, body string) *change {
	c := &change{sha: sha, name: name, email: email, subject: subject, text: subject}
	if m := conventionalRE.FindStringSubmatch(subject); m != nil {
		c.kind, c.scope, c.breaking, c.text = strings.ToLower(m[1]), strings.TrimSpace(m[2]), m[3] != "", m[4]
	}
	if loc := breakingFooterRE.FindStringIndex(body); loc != nil {
		c.breaking, c.breakingNote = true, breakingNote(body[loc[1]:])
	}
	if c.kind == "revert" || strings.HasPrefix(subject, `Revert "`) {
		if m := revertRE.FindStringSubmatch(body); m != nil {
			c.reverts = m[1]
		}
	}
	return c
}

// breakingNote returns the text of a BREAKING CHANGE footer, which runs until a
// blank line or the next trailer, joined into one line.
func breakingNote(rest string) string {
	var lines []string
	for line := range strings.SplitSeq(strings.TrimLeft(rest, " \t\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || (len(lines) > 0 && trailerRE.MatchString(line)) {
			break
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, " ")
}

// excluded reports whether a change is left out of the changelog.
func (c *change) excluded() bool {
	return excludeRE.MatchString(c.subject) || (!c.breaking && c.kind != "feat" && typoRE.MatchString(c.text))
}

// group returns the changelog group a change goes in. A change goes in only
// one, so a breaking feature is listed as breaking.
func (c *change) group() int {
	scopes := strings.Split(strings.ToLower(c.scope), ",")
	for i := range scopes {
		scopes[i] = strings.TrimSpace(scopes[i])
	}
	switch {
	case c.breaking:
		return groupBreaking
	case c.kind == "security" || slices.Contains(scopes, "security") || securityRE.MatchString(c.text):
		return groupSecurity
	case c.kind == "deps" || slices.ContainsFunc(scopes, func(s string) bool { return s == "deps" || s == "deps-dev" }) ||
		(c.kind == "" && depsRE.MatchString(c.text)):
		return groupDeps
	}
	switch c.kind {
	case "feat", "feature":
		return groupFeatures
	case "fix", "bugfix", "hotfix":
		return groupFixes
	case "perf":
		return groupPerf
	case "docs", "doc":
		return groupDocs
	case "":
		switch {
		case featureVerbRE.MatchString(c.text):
			return groupFeatures
		case fixVerbRE.MatchString(c.text):
			return groupFixes
		}
	}
	return groupMisc
}

// author returns how the notes credit the author: a mention, so GitHub links
// the account and lists it as a contributor, or else the name.
func (c *change) author() string {
	if c.login != "" {
		return "@" + strings.TrimSuffix(c.login, "[bot]")
	}
	return c.name
}

// markdown returns the change's line in the changelog. GitHub links the SHA,
// and shows it shortened.
func (c *change) markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "* %s: ", c.sha)
	if c.scope != "" {
		fmt.Fprintf(&b, "**%s:** ", c.scope)
	}
	b.WriteString(c.text)
	if author := c.author(); author != "" {
		fmt.Fprintf(&b, " (%s)", author)
	}
	if c.breakingNote != "" && c.breakingNote != c.text {
		fmt.Fprintf(&b, "\n  * %s", c.breakingNote)
	}
	return b.String()
}

// filterChanges drops the changes nobody needs to read about, along with any
// change that's reverted in the same release, and its revert.
func filterChanges(changes []*change) []*change {
	reverted := map[*change]bool{}
	for _, revert := range changes {
		if revert.reverts == "" {
			continue
		}
		for _, c := range changes {
			if strings.HasPrefix(c.sha, revert.reverts) {
				reverted[c], reverted[revert] = true, true
			}
		}
	}
	return slices.DeleteFunc(slices.Clone(changes), func(c *change) bool { return reverted[c] || c.excluded() })
}

// releaseModule is a module in the notes' install instructions.
type releaseModule struct {
	path    string
	command bool // Its root is a main package, so it's installed rather than required.
}

// notes is everything in a release's notes.
type notes struct {
	repo, version string
	previous      string // The release the changelog starts from, or "" for the first.
	highlights    string
	changes       []*change // Oldest first.
	modules       []releaseModule
	footer        string
}

// render returns the notes as Markdown. A changelog too long for GitHub is cut
// short, and says how many changes it left out.
func (n *notes) render() string {
	var top, bottom strings.Builder
	if h := strings.TrimSpace(n.highlights); h != "" {
		top.WriteString(h + "\n\n")
	}
	top.WriteString("## Changelog\n\n")

	if n.previous != "" {
		fmt.Fprintf(&bottom, "**Full changelog**: [`%s...%s`](https://github.com/%s/compare/%s...%s)\n\n",
			n.previous, n.version, n.repo, n.previous, n.version)
	}
	bottom.WriteString(n.install())
	if f := strings.TrimSpace(n.footer); f != "" {
		bottom.WriteString(f + "\n")
	}
	return top.String() + n.changelog(maxNotesLength-top.Len()-bottom.Len()) + bottom.String()
}

// changelog returns the changes grouped under headings, in at most about budget
// characters.
func (n *notes) changelog(budget int) string {
	groups := make([][]*change, len(groupTitles))
	for _, c := range n.changes {
		groups[c.group()] = append(groups[c.group()], c)
	}
	var b strings.Builder
	if len(n.changes) == 0 {
		b.WriteString("No changes besides updated requirements")
		if n.previous != "" {
			b.WriteString(" since " + n.previous)
		}
		b.WriteString(".\n\n")
		return b.String()
	}
	const overflow = 200 // Room for the line that says what's left out.
	left := len(n.changes)
	for i, group := range groups {
		if len(group) == 0 {
			continue
		}
		// Within a group, changes to the same scope, like a module, are together.
		slices.SortStableFunc(group, func(a, b *change) int { return cmp.Compare(a.scope, b.scope) })
		heading := "### " + groupTitles[i] + "\n\n"
		// Few readers want these, so their lists start collapsed. GitHub needs
		// blank lines around Markdown inside <details>.
		var open, closing string
		if i == groupMisc || i == groupDeps {
			summary := fmt.Sprintf("%d changes", len(group))
			if len(group) == 1 {
				summary = "1 change"
			}
			open = "<details>\n<summary>" + summary + "</summary>\n\n"
			closing = "\n</details>\n"
		}
		if b.Len()+len(heading)+len(open)+len(closing)+overflow > budget {
			break
		}
		b.WriteString(heading + open)
		for _, c := range group {
			line := c.markdown() + "\n"
			if b.Len()+len(line)+len(closing)+overflow > budget {
				break
			}
			b.WriteString(line)
			left--
		}
		b.WriteString(closing + "\n")
	}
	if left > 0 {
		fmt.Fprintf(&b, "_%d more changes don't fit here, so see the full changelog._\n\n", left)
	}
	return b.String()
}

// install returns how to get each module at the release, then where its docs
// are. It pins the version rather than using @latest, so an old release's notes
// still install that release. Commands have no prompt, so GitHub's copy button
// copies something that runs.
func (n *notes) install() string {
	if len(n.modules) == 0 {
		return ""
	}
	var pinned strings.Builder
	docs := make([]string, len(n.modules))
	for i, m := range n.modules {
		if m.command {
			fmt.Fprintf(&pinned, "go install %s@%s\n", m.path, n.version)
		} else {
			fmt.Fprintf(&pinned, "go get %s@%s\n", m.path, n.version)
		}
		docs[i] = fmt.Sprintf("[%s](https://pkg.go.dev/%s@%s)", m.path, m.path, n.version)
	}

	var b strings.Builder
	b.WriteString("## :package: Install\n\n")
	// GitHub needs blank lines around Markdown inside <details>.
	collapse := len(n.modules) > 5
	if collapse {
		fmt.Fprintf(&b, "<details>\n<summary>All %d modules</summary>\n\n", len(n.modules))
	}
	b.WriteString("```sh\n" + pinned.String() + "```\n\n")
	if len(docs) == 1 {
		b.WriteString("**Docs**: " + docs[0] + "\n\n")
	} else {
		b.WriteString("**Docs**:\n\n* " + strings.Join(docs, "\n* ") + "\n\n")
	}
	if collapse {
		b.WriteString("</details>\n\n")
	}
	return b.String()
}

// previousVersion returns the release a version's changelog starts from: the
// highest root version below it. A release that isn't a prerelease skips
// prereleases, so its changelog has everything since the last full release.
func previousVersion(tags []string, version string) string {
	stable := semver.Prerelease(version) == ""
	var previous string
	for _, tag := range tags {
		if semver.Canonical(tag) != tag || semver.Compare(tag, version) >= 0 || (stable && semver.Prerelease(tag) != "") {
			continue
		}
		if semver.Compare(tag, previous) > 0 {
			previous = tag
		}
	}
	return previous
}

// footerData is what a footer template can use.
type footerData struct {
	Repo     string // owner/name.
	URL      string // https://github.com/owner/name.
	Version  string // The version being released, like v1.2.3.
	Previous string // The release the changelog starts from, or "".
	Branch   string // The branch being released.

	Prerelease bool

	// Links to the repository's community files, or "" for those it doesn't have.
	License, Contributing, Support, CodeOfConduct, Security string
}

// setCommunityFiles finds the repository's community files in its list of files
// at the root, in .github, and in docs, and links each one. GitHub
// looks in those directories, and prefers them in that order.
func (d *footerData) setCommunityFiles(files []string) {
	find := func(names ...string) string {
		for _, dir := range []string{".github/", "", "docs/"} {
			for _, file := range files {
				rest, ok := strings.CutPrefix(strings.ToLower(file), dir)
				if !ok || strings.Contains(rest, "/") {
					continue
				}
				base, _, _ := strings.Cut(rest, ".")
				if slices.Contains(names, base) {
					return d.URL + "/blob/" + d.Branch + "/" + file
				}
			}
		}
		return ""
	}
	d.License = find("license", "licence", "copying")
	d.Contributing = find("contributing")
	d.Support = find("support")
	d.CodeOfConduct = find("code_of_conduct")
	if find("security") != "" {
		d.Security = d.URL + "/security/policy"
	}
}
