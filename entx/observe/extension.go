// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package observe

import (
	"fmt"
	"path"
	"slices"
	"text/template"

	"entgo.io/ent/entc"
	"entgo.io/ent/entc/gen"
)

// Ensure Extension implements [entc.Extension].
var _ entc.Extension = (*Extension)(nil)

// Extension generates observe hooks into the target ent package.
type Extension struct {
	entc.DefaultExtension
	config *Config
}

// New returns an observe codegen extension that generates mutation observe
// hooks (ObserverEvent, ObserverHandler, Observe*, Observe*ID, Observe*Fields)
// into the target ent package as observe.go. Generated code does not import
// this module.
//
// A nil config is valid and observes every eligible schema.
func New(cfg *Config) *Extension {
	if cfg == nil {
		cfg = &Config{}
	}
	return &Extension{config: cfg}
}

func (e *Extension) Templates() []*gen.Template {
	return []*gen.Template{
		gen.MustParse(gen.NewTemplate("observe").
			Funcs(template.FuncMap{
				"observedNodes":  e.observedNodes,
				"observeImports": e.observeImports,
				"observeEdges":   ObservedEdges,
			}).
			ParseFS(templateFS, "template/observe.tmpl")),
	}
}

func (e *Extension) Hooks() []gen.Hook {
	return []gen.Hook{e.validate}
}

// validate rejects unobservable schemas before the next generator runs.
func (e *Extension) validate(next gen.Generator) gen.Generator {
	return gen.GenerateFunc(func(g *gen.Graph) error {
		if err := e.validateGraph(g); err != nil {
			return err
		}
		return next.Generate(g)
	})
}

// validateGraph checks that configured or auto-selected schemas can receive
// observe hooks. With a non-empty Nodes allow-list, unknown names and
// views/composite IDs are hard errors; otherwise views and composite IDs are
// skipped and only unrenderable ID types fail codegen. Annotation placement is
// always validated across the full graph.
func (e *Extension) validateGraph(g *gen.Graph) error {
	if err := ValidateAnnotations(g.Nodes...); err != nil {
		return err
	}

	byName := make(map[string]*gen.Type, len(g.Nodes))
	for _, n := range g.Nodes {
		byName[n.Name] = n
	}

	if len(e.config.Nodes) > 0 {
		for _, name := range e.config.Nodes {
			n, ok := byName[name]
			if !ok {
				return fmt.Errorf("observe: unknown schema %q in Config.Nodes", name)
			}
			if err := nodeObserveError(n); err != nil {
				return err
			}
		}
		return nil
	}

	// MutableNodes already excludes views. Soft-skip composites / missing
	// single-field IDs; only unrenderable ID types fail codegen.
	for _, n := range g.MutableNodes() {
		if !n.HasOneFieldID() {
			continue
		}
		if err := nodeObserveError(n); err != nil {
			return err
		}
	}
	return nil
}

// nodeObserveError returns a descriptive error when n cannot receive observe
// hooks. Views, composite IDs, and IDs with no renderable Go type all fail.
// Allow-list validation looks at the full graph so explicitly requested views
// are caught even though they are absent from MutableNodes.
func nodeObserveError(n *gen.Type) error {
	if n.IsView() {
		return fmt.Errorf("observe: schema %q is a view and cannot be observed", n.Name)
	}
	if n.HasCompositeID() || !n.HasOneFieldID() {
		return fmt.Errorf("observe: schema %q has a composite ID and cannot be observed", n.Name)
	}
	if !idTypeRenderable(n) {
		return fmt.Errorf("observe: schema %q has an ID type with no renderable Go type", n.Name)
	}
	return nil
}

// idTypeRenderable reports whether n's single-field ID has a Go type string
// that templates can emit (non-empty and not "invalid").
func idTypeRenderable(n *gen.Type) bool {
	if n.ID == nil || n.ID.Type == nil {
		return false
	}
	s := n.ID.Type.String()
	return s != "" && s != "invalid"
}

// ObservedNodes returns schemas that receive generated observe helpers.
// A nil cfg is treated as an empty config (observe every eligible schema).
func ObservedNodes(cfg *Config, g *gen.Graph) []*gen.Type {
	if cfg == nil {
		cfg = &Config{}
	}
	var out []*gen.Type
	allow := cfg.Nodes
	for _, n := range g.MutableNodes() {
		if nodeObserveError(n) != nil {
			continue
		}
		if len(allow) > 0 && !slices.Contains(allow, n.Name) {
			continue
		}
		out = append(out, n)
	}
	return out
}

func (e *Extension) observedNodes(g *gen.Graph) []*gen.Type {
	return ObservedNodes(e.config, g)
}

// observeImports returns unique import paths needed by generated observe code:
// each observed node's ID type package (when external) and its entity package
// under the graph root.
func (e *Extension) observeImports(g *gen.Graph) []string {
	seen := make(map[string]struct{})
	var paths []string
	add := func(p string) {
		if p == "" {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		paths = append(paths, p)
	}
	for _, n := range ObservedNodes(e.config, g) {
		add(n.ID.Type.PkgPath)
		add(path.Join(g.Package, n.PackageDir()))
	}
	return paths
}
