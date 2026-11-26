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

// scenarioTestFiles is the Go scenario tests testgen writes over the repo's project document
// (path -> bytes) and one of their paths.
func scenarioTestFiles(t *testing.T, r gitRepo) (map[string]string, string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(r.root, ".aiarch/state/project.json"))
	if err != nil {
		t.Fatal(err)
	}
	c, ok := appgen.ConsumerNamed(testGenerator)
	if !ok {
		t.Fatalf("no consumer %q", testGenerator)
	}
	out, err := c.Emit(appgen.Input{Project: raw, ModulePath: "example.com/app", Config: appgen.DefaultConfig(), Tree: os.DirFS(r.root)})
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	var target string
	for _, f := range out {
		if projectstate.IsTestCodePath(f.Path) && filepath.Ext(f.Path) == ".go" {
			files[f.Path] = string(f.Content)
			target = f.Path
		}
	}
	if target == "" {
		t.Fatal("testgen wrote no Go scenario test over the todomvc fixture")
	}
	return files, target
}

// TestGate_JobGeneratedScenarioTestsAreAdmittedWhenRegenerated: an integration task's job writes the
// scenario tests and COMMITS them before the agent runs (the job's own commit). The scope
// refuses every test-code path, so the gate admits exactly the ones whose committed bytes equal what
// testgen writes; a hand edit of one, or a test file the generator never writes, is still outside
// the scope.
func TestGate_JobGeneratedScenarioTestsAreAdmittedWhenRegenerated(t *testing.T) {
	r, specFile, base, _ := driftRepo(t)
	files, target := scenarioTestFiles(t, r)

	head := r.commit("the job's generated tests", files)
	if err := r.runGate(specFile, "--base", base, "--head", head, "--no-main"); err != nil {
		t.Fatalf("generated scenario tests byte-equal to regeneration must be admitted: %v", err)
	}

	edited := r.commit("hand edit", map[string]string{target: files[target] + "// tweaked by hand\n"})
	wantExit(t, r.runGate(specFile, "--base", base, "--head", edited, "--no-main"), 1, "lie outside the task's edit scope", target)

	stray := r.commit("a test file no generator writes", map[string]string{"internal/manager/list/extra_scenarios.gen_test.go": "package list\n"})
	wantExit(t, r.runGate(specFile, "--base", base, "--head", stray, "--no-main"), 1, "lie outside the task's edit scope", "extra_scenarios.gen_test.go")
}

// TestGate_TheActivityNetDiffAdmitsTheJobsTestsOnlyByteEqualToRegeneration: the branch of an
// integration activity holds the job's commit of the testgen output, then the agent's wiring
// commit. The activity-level net diff against main (--main) holds the job's tests, which the scope
// refuses as test code; they are admitted because they equal regeneration, and the same branch
// after the agent hand-edits one of them is refused. The per-attempt diff (base = the tip after
// the job's commit) never holds them.
func TestGate_TheActivityNetDiffAdmitsTheJobsTestsOnlyByteEqualToRegeneration(t *testing.T) {
	r, specFile, base, _ := driftRepo(t)
	r.git("branch", "-f", "main", base) // main is the seeded project; the branch adds the job's and the agent's commits
	files, target := scenarioTestFiles(t, r)
	jobTip := r.commit("aiarch: the job's generated output", files)
	wired := r.commit("agent wiring", map[string]string{"internal/manager/list/listmanager.go": "package list\n"})

	rng, err := resolveEditScopeRange(r.root, jobTip, wired, base, false)
	if err != nil {
		t.Fatal(err)
	}
	stray, err := activityScopeViolations(r.root, specFile, rng)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(stray, target) {
		t.Fatalf("the net diff against main does not hold the job's test %s as outside the scope: %v", target, stray)
	}
	if stray, err = admitRegeneratedTests(r.root, specFile, rng, stray); err != nil || len(stray) != 0 {
		t.Fatalf("the job's tests, equal to regeneration, were not admitted in the net diff: %v, %v", stray, err)
	}
	if err := r.runGate(specFile, "--base", jobTip, "--head", wired, "--main", base); err != nil {
		t.Fatalf("job commit + agent wiring commit must pass the gate with --main: %v", err)
	}

	edited := r.commit("hand edit", map[string]string{target: files[target] + "// tweaked by hand\n"})
	rng, err = resolveEditScopeRange(r.root, wired, edited, base, false)
	if err != nil {
		t.Fatal(err)
	}
	if stray, err = activityScopeViolations(r.root, specFile, rng); err != nil {
		t.Fatal(err)
	}
	if stray, err = admitRegeneratedTests(r.root, specFile, rng, stray); err != nil || !slices.Contains(stray, target) {
		t.Fatalf("the hand-edited test %s was admitted in the net diff: %v, %v", target, stray, err)
	}
	wantExit(t, r.runGate(specFile, "--base", wired, "--head", edited, "--main", base), 1, "lie outside the edit scope of every task", target)
}
