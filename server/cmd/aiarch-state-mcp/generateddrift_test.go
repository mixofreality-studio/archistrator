package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/appgen"
	conformance "github.com/mixofreality-studio/archistrator-platform/framework-go-conformance"

	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// driftRepo is a repo seeded with the todomvc conformance project, the generated tree its
// C-list-manager integration task runs the generators for, and the task's spec; it returns the
// repo, the spec file and the base commit.
func driftRepo(t *testing.T) (gitRepo, string, string, review.TaskSpec) {
	t.Helper()
	raw, err := conformance.Fixture("todomvc")
	if err != nil {
		t.Fatal(err)
	}
	p, ok, err := projectstate.DecodeProjectJSON(raw, "")
	if err != nil || !ok {
		t.Fatalf("decode the todomvc fixture: ok=%v err=%v", ok, err)
	}
	spec, err := deriveTaskSpec(&p, "C-list-manager", "integration")
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Generated) == 0 {
		t.Fatal("the integration task's spec names no generated path")
	}
	files := map[string]string{
		".aiarch/state/project.json": string(raw),
		"go.mod":                     "module example.com/app\n\ngo 1.25\n",
	}
	in := appgen.Input{Project: raw, ModulePath: "example.com/app", Config: appgen.DefaultConfig(), Only: spec.Contracts}
	names := []string{"modelgen"}
	for _, g := range spec.Generate {
		names = append(names, g.Title)
	}
	for _, name := range names {
		c, _ := appgen.ConsumerNamed(name)
		run := in
		if name != "modelgen" {
			run.Only = nil
		}
		out, err := c.Emit(run)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, f := range out {
			files[f.Path] = string(f.Content)
		}
	}
	// Every contract package holds hand-written code, so the tree counts as built.
	for _, sc := range p.ServiceContracts {
		if sc.GoPackage != "" {
			files[sc.GoPackage+"/impl.go"] = "package " + filepath.Base(sc.GoPackage) + "\n"
		}
	}
	r := newGitRepo(t)
	base := r.commit("seed the project and its generated tree", files)
	b, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	specFile := filepath.Join(t.TempDir(), "spec.json")
	if err := os.WriteFile(specFile, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return r, specFile, base, spec
}

// TestGate_GeneratedPathMustEqualRegeneration: a generated path inside the scope's globs is still
// held byte-equal to what its generator writes. A hand edit of cmd/server/main.gen.go is drift;
// a change that leaves the generated tree as regenerated passes.
func TestGate_GeneratedPathMustEqualRegeneration(t *testing.T) {
	r, specFile, base, spec := driftRepo(t)
	const target = "cmd/server/main.gen.go"
	if !slices.Contains(spec.Generated, target) {
		t.Fatalf("the spec does not name %s as generated: %v", target, spec.Generated)
	}

	// An in-scope hand-written change beside the generated tree: no drift.
	ok := r.commit("hand-written code", map[string]string{"internal/manager/list/listmanager.go": "package list\n"})
	if err := r.runGate(specFile, "--base", base, "--head", ok, "--no-main"); err != nil {
		t.Fatalf("a change that leaves the generated files as regenerated must pass: %v", err)
	}

	// One hand-edited byte in a generated file, inside the scope's globs.
	raw, err := os.ReadFile(filepath.Join(r.root, target))
	if err != nil {
		t.Fatal(err)
	}
	head := r.commit("hand edit", map[string]string{target: string(raw) + "// tweaked by hand\n"})
	wantExit(t, r.runGate(specFile, "--base", base, "--head", head, "--no-main"), 1, "EDIT-GENERATED-DRIFT", target)
}
