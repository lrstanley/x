// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package observe_test

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"entgo.io/ent/entc"
	"entgo.io/ent/entc/gen"
	"entgo.io/ent/entc/load"
	"github.com/lrstanley/x/entx/observe"
)

func TestNew(t *testing.T) {
	t.Parallel()

	var _ entc.Extension = observe.New(nil)
	var _ entc.Extension = observe.New(&observe.Config{})
	var _ entc.Extension = observe.New(&observe.Config{Nodes: []string{"User"}})

	ext := observe.New(nil)
	if len(ext.Templates()) != 1 {
		t.Fatalf("Templates() = %d, want 1", len(ext.Templates()))
	}
	if len(ext.Hooks()) != 1 {
		t.Fatalf("Hooks() = %d, want 1", len(ext.Hooks()))
	}
}

func fixturesPath() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "fixtures")
}

// entcLoadMu serializes entc schema loads/generates; parallel loads of the same
// package race on the shared .entc cache directory.
var entcLoadMu sync.Mutex

func loadSchema(t *testing.T, rel string) *load.SchemaSpec {
	t.Helper()
	entcLoadMu.Lock()
	defer entcLoadMu.Unlock()
	spec, err := (&load.Config{Path: filepath.Join(fixturesPath(), rel)}).Load()
	if err != nil {
		t.Fatalf("load schema %q: %v", rel, err)
	}
	return spec
}

func runExtension(t *testing.T, schema *load.SchemaSpec, cfg *observe.Config) (*gen.Graph, error) {
	t.Helper()
	ext := observe.New(cfg)
	gconfig := &gen.Config{
		Hooks:     ext.Hooks(),
		Templates: ext.Templates(),
	}
	gconfig.Schema = schema.PkgPath
	if gconfig.Package == "" {
		gconfig.Package = path.Dir(schema.PkgPath)
	}
	var err error
	gconfig.Storage, err = gen.NewStorage("sql")
	if err != nil {
		return nil, err
	}
	graph, err := gen.NewGraph(gconfig, schema.Schemas...)
	if err != nil {
		return nil, err
	}
	var g gen.Generator = gen.GenerateFunc(func(*gen.Graph) error { return nil })
	for _, v := range slices.Backward(graph.Hooks) {
		g = v(g)
	}
	if err := g.Generate(graph); err != nil {
		return graph, err
	}
	return graph, nil
}

func observedNames(cfg *observe.Config, graph *gen.Graph) []string {
	nodes := observe.ObservedNodes(cfg, graph)
	names := make([]string, len(nodes))
	for i, n := range nodes {
		names[i] = n.Name
	}
	return names
}

var schemaOnce = sync.OnceValue(func() *load.SchemaSpec {
	entcLoadMu.Lock()
	defer entcLoadMu.Unlock()
	spec, err := (&load.Config{Path: filepath.Join(fixturesPath(), "schema")}).Load()
	if err != nil {
		panic(fmt.Sprintf("load schema: %v", err))
	}
	return spec
})

func TestValidate_allowList(t *testing.T) {
	t.Parallel()

	cfg := &observe.Config{Nodes: []string{"User"}}
	graph, err := runExtension(t, schemaOnce(), cfg)
	if err != nil {
		t.Fatalf("runExtension: %v", err)
	}
	names := observedNames(cfg, graph)
	if !slices.Equal(names, []string{"User"}) {
		t.Fatalf("observed = %v, want [User]", names)
	}
}

func TestValidate_nilConfigObservesAll(t *testing.T) {
	t.Parallel()

	graph, err := runExtension(t, schemaOnce(), nil)
	if err != nil {
		t.Fatalf("runExtension: %v", err)
	}
	names := observedNames(nil, graph)
	slices.Sort(names)
	want := []string{"Document", "Note", "User"}
	if !slices.Equal(names, want) {
		t.Fatalf("observed = %v, want %v", names, want)
	}
}

func TestValidate_unknownNode(t *testing.T) {
	t.Parallel()

	_, err := runExtension(t, schemaOnce(), &observe.Config{Nodes: []string{"Missing"}})
	if err == nil || !strings.Contains(err.Error(), "unknown schema") {
		t.Fatalf("error = %v, want unknown schema", err)
	}
}

func TestValidate_explicitViewFails(t *testing.T) {
	t.Parallel()

	schema := loadSchema(t, "schema_view")
	_, err := runExtension(t, schema, &observe.Config{Nodes: []string{"CleanUser"}})
	if err == nil || !strings.Contains(err.Error(), "view") {
		t.Fatalf("error = %v, want view", err)
	}
}

func TestValidate_viewSkippedByDefault(t *testing.T) {
	t.Parallel()

	schema := loadSchema(t, "schema_view")
	graph, err := runExtension(t, schema, nil)
	if err != nil {
		t.Fatalf("runExtension: %v", err)
	}
	names := observedNames(nil, graph)
	if slices.Contains(names, "CleanUser") {
		t.Fatalf("observed includes CleanUser: %v", names)
	}
	if !slices.Contains(names, "User") {
		t.Fatalf("observed missing User: %v", names)
	}
}

func TestValidate_explicitCompositeFails(t *testing.T) {
	t.Parallel()

	schema := loadSchema(t, "schema_composite")
	_, err := runExtension(t, schema, &observe.Config{Nodes: []string{"Friendship"}})
	if err == nil || !strings.Contains(err.Error(), "composite") {
		t.Fatalf("error = %v, want composite", err)
	}
}

func TestValidate_compositeSkippedByDefault(t *testing.T) {
	t.Parallel()

	schema := loadSchema(t, "schema_composite")
	graph, err := runExtension(t, schema, nil)
	if err != nil {
		t.Fatalf("runExtension: %v", err)
	}
	names := observedNames(nil, graph)
	if slices.Contains(names, "Friendship") {
		t.Fatalf("observed includes Friendship: %v", names)
	}
	if !slices.Contains(names, "User") {
		t.Fatalf("observed missing User: %v", names)
	}
}

func findNode(t *testing.T, g *gen.Graph, name string) *gen.Type {
	t.Helper()
	for _, n := range g.Nodes {
		if n.Name == name {
			return n
		}
	}
	t.Fatalf("node %q not found", name)
	return nil
}

func edgeNames(edges []*gen.Edge) []string {
	out := make([]string, len(edges))
	for i, e := range edges {
		out[i] = e.Name
	}
	return out
}

func cloneAnnotations(in gen.Annotations) gen.Annotations {
	if in == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func injectAnnotation(t *testing.T, g *gen.Graph, schemaPath string, ant observe.Annotation) {
	t.Helper()
	parts := strings.Split(schemaPath, ".")
	n := findNode(t, g, parts[0])
	if len(parts) < 2 {
		n.Annotations = cloneAnnotations(n.Annotations)
		cur := observe.GetAnnotation(n)
		merged, _ := cur.Merge(ant).(observe.Annotation)
		n.Annotations[merged.Name()] = merged
		return
	}
	for _, e := range n.Edges {
		if e.Name == parts[1] {
			e.Annotations = cloneAnnotations(e.Annotations)
			cur := observe.GetAnnotation(e)
			merged, _ := cur.Merge(ant).(observe.Annotation)
			e.Annotations[merged.Name()] = merged
			return
		}
	}
	for _, f := range n.Fields {
		if f.Name == parts[1] {
			f.Annotations = cloneAnnotations(f.Annotations)
			cur := observe.GetAnnotation(f)
			merged, _ := cur.Merge(ant).(observe.Annotation)
			f.Annotations[merged.Name()] = merged
			return
		}
	}
	t.Fatalf("failed to find field or edge %q on %q", parts[1], parts[0])
}

func generateObserveGo(t *testing.T, schemaRel string) string {
	t.Helper()
	dir := t.TempDir()
	entDir := filepath.Join(dir, "ent")
	entcLoadMu.Lock()
	err := entc.Generate(
		filepath.Join(fixturesPath(), schemaRel),
		&gen.Config{
			Target:  entDir,
			Package: "github.com/lrstanley/x/entx/observe/fixtures/ent",
		},
		entc.Extensions(observe.New(nil)),
	)
	entcLoadMu.Unlock()
	if err != nil {
		t.Fatalf("entc.Generate: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(entDir, "observe.go"))
	if err != nil {
		t.Fatalf("read observe.go: %v", err)
	}
	return string(b)
}

func TestObservedEdges_schemaAllEdgesOptOut(t *testing.T) {
	t.Parallel()

	graph, err := runExtension(t, schemaOnce(), nil)
	if err != nil {
		t.Fatalf("runExtension: %v", err)
	}
	user := findNode(t, graph, "User")
	got := edgeNames(observe.ObservedEdges(user))
	if !slices.Equal(got, []string{"documents"}) {
		t.Fatalf("ObservedEdges(User) = %v, want [documents]", got)
	}
	doc := findNode(t, graph, "Document")
	if len(observe.ObservedEdges(doc)) != 0 {
		t.Fatalf("ObservedEdges(Document) = %v, want none", edgeNames(observe.ObservedEdges(doc)))
	}
}

func TestObservedEdges_selectiveInclude(t *testing.T) {
	t.Parallel()

	graph, err := runExtension(t, schemaOnce(), nil)
	if err != nil {
		t.Fatalf("runExtension: %v", err)
	}
	// Disable schema eager-load; keep documents via explicit include; notes stays opted out.
	injectAnnotation(t, graph, "User", observe.WithEagerLoad(false))
	injectAnnotation(t, graph, "User.documents", observe.WithEagerLoad(true))
	got := edgeNames(observe.ObservedEdges(findNode(t, graph, "User")))
	if !slices.Equal(got, []string{"documents"}) {
		t.Fatalf("ObservedEdges(User) = %v, want [documents]", got)
	}
}

func TestGenerate_eagerLoadIncludesOptOut(t *testing.T) {
	t.Parallel()

	src := generateObserveGo(t, "schema")
	// User: WithEagerLoad(true) + notes opted out → only documents.
	if !strings.Contains(src, "func observeEagerUser") {
		t.Fatal("missing observeEagerUser")
	}
	eagerUser := src[strings.Index(src, "func observeEagerUser"):]
	if end := strings.Index(eagerUser, "\nfunc "); end > 0 {
		eagerUser = eagerUser[:end]
	}
	if !strings.Contains(eagerUser, ".WithDocuments()") {
		t.Fatalf("observeEagerUser missing WithDocuments:\n%s", eagerUser)
	}
	if strings.Contains(eagerUser, ".WithNotes()") {
		t.Fatalf("observeEagerUser should omit WithNotes:\n%s", eagerUser)
	}
	// Document has an author edge but no observe annotations → no WithAuthor.
	eagerDoc := src[strings.Index(src, "func observeEagerDocument"):]
	if end := strings.Index(eagerDoc, "\nfunc "); end > 0 {
		eagerDoc = eagerDoc[:end]
	}
	if strings.Contains(eagerDoc, ".WithAuthor()") {
		t.Fatalf("observeEagerDocument should omit WithAuthor:\n%s", eagerDoc)
	}
}

func TestValidate_badAnnotationPlacementCodegen(t *testing.T) {
	t.Parallel()

	graph, err := runExtension(t, schemaOnce(), nil)
	if err != nil {
		t.Fatalf("runExtension: %v", err)
	}
	injectAnnotation(t, graph, "User.name", observe.WithEagerLoad(true))
	if err := observe.ValidateAnnotations(graph.Nodes...); err == nil || !strings.Contains(err.Error(), "EagerLoad") {
		t.Fatalf("error = %v, want EagerLoad placement failure", err)
	}
}
