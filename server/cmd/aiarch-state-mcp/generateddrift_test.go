package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

// run033Branch is the RUN-033 shape: main is the seeded project; the branch holds the integration job's
// commit of the testgen output, then an stp attempt that amends the plan in project.json so every
// committed scenario test is stale (no longer equal to regeneration). It returns the repo, the
// seeded commit (main), the job's tip (the stp attempt's base), the stp attempt's head, a committed
// test path and the stp task's spec derived over the same project (the flag is real, not hand-set).
func run033Branch(t *testing.T) (r gitRepo, mainSHA, jobTip, head, target string, spec review.TaskSpec) {
	t.Helper()
	r, _, mainSHA, _ = driftRepo(t)
	r.git("branch", "-f", "main", mainSHA)
	files, target := scenarioTestFiles(t, r)
	jobTip = r.commit("aiarch: the job's generated output", files)

	raw, err := os.ReadFile(filepath.Join(r.root, ".aiarch/state/project.json"))
	if err != nil {
		t.Fatal(err)
	}
	amended := strings.ReplaceAll(string(raw), "Add a todo", "Add a todo item")
	if amended == string(raw) {
		t.Fatal("the plan amendment changed nothing")
	}
	head = r.commit("stp: amend the plan", map[string]string{".aiarch/state/project.json": amended})

	// The amendment must really make the committed test stale, or the test proves nothing.
	c, _ := appgen.ConsumerNamed(testGenerator)
	out, err := c.Emit(appgen.Input{Project: []byte(amended), ModulePath: "example.com/app", Config: appgen.DefaultConfig(), Tree: os.DirFS(r.root)})
	if err != nil {
		t.Fatal(err)
	}
	stale := false
	for _, f := range out {
		if f.Path == target && string(f.Content) != files[target] {
			stale = true
		}
	}
	if !stale {
		t.Fatalf("the amended plan leaves %s equal to regeneration", target)
	}

	p, ok, err := projectstate.DecodeProjectJSON([]byte(amended), "")
	if err != nil || !ok {
		t.Fatalf("decode the amended project: ok=%v err=%v", ok, err)
	}
	spec, err = deriveTaskSpec(&p, "C-list-manager", "stp")
	if err != nil {
		t.Fatal(err)
	}
	return r, mainSHA, jobTip, head, target, spec
}

// writeSpecFile writes spec, rehashed, to a temp file.
func writeSpecFile(t *testing.T, spec review.TaskSpec) string {
	t.Helper()
	spec.Hash = specHashMust(spec)
	b, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), "spec.json")
	if err := os.WriteFile(f, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

// TestGate_TheActivityNetDiffAdmitsTheJobsTestsWithoutByteEquality: admission at the activity level is
// a property of the ACTIVITY (spec.ActivityRegeneratesTests). The job's committed scenario tests stay
// in the net diff against main through every later task; an stp that amends the plan makes them
// stale until integration's job regenerates them, and the stp's own gate must still pass (RUN-033).
func TestGate_TheActivityNetDiffAdmitsTheJobsTestsWithoutByteEquality(t *testing.T) {
	r, mainSHA, jobTip, head, target, spec := run033Branch(t)
	if !spec.ActivityRegeneratesTests {
		t.Fatal("the stp task's spec of an activity with an integration task must carry ActivityRegeneratesTests")
	}
	specFile := writeSpecFile(t, spec)

	rng, err := resolveEditScopeRange(r.root, jobTip, head, mainSHA, false)
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
	if stray, err = admitActivityGeneratedTests(r.root, specFile, rng, stray); err != nil || len(stray) != 0 {
		t.Fatalf("the job's (now stale) tests were not admitted in the activity net diff: %v, %v", stray, err)
	}
	if err := r.runGate(specFile, "--base", jobTip, "--head", head, "--main", mainSHA); err != nil {
		t.Fatalf("the stp attempt over the job's committed tests must pass the gate with --main: %v", err)
	}
}

// TestGate_TheActivityNetDiffAdmitsNothingWhenTheActivityRegeneratesNoTests: the same branch under a
// spec whose flag is false: the committed test is outside the edit scope of every task.
func TestGate_TheActivityNetDiffAdmitsNothingWhenTheActivityRegeneratesNoTests(t *testing.T) {
	r, mainSHA, jobTip, head, target, spec := run033Branch(t)
	spec.ActivityRegeneratesTests = false
	specFile := writeSpecFile(t, spec)
	wantExit(t, r.runGate(specFile, "--base", jobTip, "--head", head, "--main", mainSHA), 1, "lie outside the edit scope of every task", target)
}

// TestGate_TheActivityNetDiffStillRefusesATestNoGeneratorWrites: with the flag true, a test-code path
// testgen never writes is still outside the scope at the activity level.
func TestGate_TheActivityNetDiffStillRefusesATestNoGeneratorWrites(t *testing.T) {
	r, mainSHA, jobTip, _, _, spec := run033Branch(t)
	if !spec.ActivityRegeneratesTests {
		t.Fatal("the spec's flag must be true")
	}
	specFile := writeSpecFile(t, spec)
	stray := r.commit("a test file no generator writes", map[string]string{"internal/manager/list/extra_scenarios.gen_test.go": "package list\n"})
	wantExit(t, r.runGate(specFile, "--base", jobTip, "--head", stray, "--main", mainSHA), 1, "lie outside the edit scope of every task", "extra_scenarios.gen_test.go")
}

// TestGate_TheAttemptDiffStillAdmitsGeneratedTestsOnlyByteEqual: the per-attempt check is unchanged.
// An integration attempt whose own diff adds the generated tests byte-equal to regeneration passes; one
// whose diff edits one is refused, even though the activity level would admit the path.
func TestGate_TheAttemptDiffStillAdmitsGeneratedTestsOnlyByteEqual(t *testing.T) {
	r, specFile, base, spec := driftRepo(t)
	if !spec.ActivityRegeneratesTests {
		t.Fatal("the integration spec's flag must be true")
	}
	r.git("branch", "-f", "main", base)
	files, target := scenarioTestFiles(t, r)
	jobTip := r.commit("aiarch: the job's generated output", files)
	wired := r.commit("agent wiring", map[string]string{"internal/manager/list/listmanager.go": "package list\n"})
	if err := r.runGate(specFile, "--base", jobTip, "--head", wired, "--main", base); err != nil {
		t.Fatalf("job commit + agent wiring commit must pass the gate with --main: %v", err)
	}

	edited := r.commit("hand edit", map[string]string{target: files[target] + "// tweaked by hand\n"})
	wantExit(t, r.runGate(specFile, "--base", wired, "--head", edited, "--main", base), 1, "lie outside the task's edit scope", target)

}
