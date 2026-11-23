package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
)

var workspaceScope = review.SpecEditScope{
	Components: []review.SpecScopeComponent{{ID: "dhe", Paths: []string{
		"server/internal/engine/designhealth/**",
		"@plat/framework-go/methodcheck/**",
	}}},
}

// workspaceRepo is a workspace repo's checkout on the activity branch, with its base captured.
func workspaceRepo(t *testing.T) (gitRepo, string) {
	t.Helper()
	w := newGitRepoOn(t, "main")
	base := w.git("rev-parse", "HEAD")
	w.git("checkout", "-q", "-b", "activity/C-x")
	return w, base
}

func (w gitRepo) rangeOf(base string) workspaceRange {
	return workspaceRange{ID: "plat", Dir: w.root, Base: base, Head: w.git("rev-parse", "HEAD"), Main: w.git("rev-parse", "main")}
}

// TestEditScope_WorkspaceRepoPathsAreJudgedQualified: a workspace repo's change is judged as
// "@<repo>/<path>" against the same scope, so the repo is chosen by id and an in-project path
// of the same spelling is a different path.
func TestEditScope_WorkspaceRepoPathsAreJudgedQualified(t *testing.T) {
	r := newGitRepo(t)
	base := r.git("rev-parse", "HEAD")
	r.commit("own code", map[string]string{"server/internal/engine/designhealth/a.go": "package x\n"})
	w, wbase := workspaceRepo(t)
	w.commit("rule", map[string]string{"framework-go/methodcheck/rules_uc_door.go": "package methodcheck\n"})
	w.commit("stray", map[string]string{"framework-go/arch/stray.go": "package arch\n"})

	spec := writeSpec(t, workspaceScope)
	rng, err := resolveEditScopeRange(r.root, base, r.git("rev-parse", "HEAD"), r.git("rev-parse", "main"), false)
	if err != nil {
		t.Fatal(err)
	}
	got, err := editScopeViolations(r.root, spec, rng, w.rangeOf(wbase))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"@plat/framework-go/arch/stray.go"}; !slices.Equal(got, want) {
		t.Fatalf("violations = %v, want %v", got, want)
	}
	net, err := activityScopeViolations(r.root, spec, rng, w.rangeOf(wbase))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"@plat/framework-go/arch/stray.go"}; !slices.Equal(net, want) {
		t.Fatalf("activity violations = %v, want %v", net, want)
	}
}

// TestEditScope_WorkspaceRepoGlobAdmitsOnlyItsRepo: the same relative path in the project's own
// repo is not admitted by a workspace glob.
func TestEditScope_WorkspaceRepoGlobAdmitsOnlyItsRepo(t *testing.T) {
	r := newGitRepo(t)
	base := r.git("rev-parse", "HEAD")
	r.commit("wrong repo", map[string]string{"framework-go/methodcheck/x.go": "package x\n"})
	spec := writeSpec(t, workspaceScope)
	if got := r.violationsAgainst(spec, base, r.git("rev-parse", "HEAD"), r.git("rev-parse", "main")); !slices.Equal(got, []string{"framework-go/methodcheck/x.go"}) {
		t.Fatalf("violations = %v", got)
	}
}

// TestEditScope_AProjectPathSpelledLikeAWorkspacePathIsNotAdmitted: the repo is part of the
// path's type, not a prefix of its text, so a file of the PROJECT's own repo literally named
// "@plat/framework-go/methodcheck/x.go" is not the workspace repo's file and the workspace glob
// does not admit it (it would otherwise merge to the project's own main).
func TestEditScope_AProjectPathSpelledLikeAWorkspacePathIsNotAdmitted(t *testing.T) {
	r := newGitRepo(t)
	base := r.git("rev-parse", "HEAD")
	r.commit("escape", map[string]string{
		"@plat/framework-go/methodcheck/x.go": "package x\n",
		".aiarch/state/project.json":          "{}\n",
	})
	spec := writeSpec(t, workspaceScope)
	got := r.violationsAgainst(spec, base, r.git("rev-parse", "HEAD"), r.git("rev-parse", "main"))
	if want := []string{"@plat/framework-go/methodcheck/x.go"}; !slices.Equal(got, want) {
		t.Fatalf("violations = %v, want %v (the state directory stays admitted)", got, want)
	}
}

// TestEditScope_WorkspaceStateDirIsNotTheProjectsState: the always-admitted state directory is the
// project's own; the same path in a workspace repo is judged like any other.
func TestEditScope_WorkspaceStateDirIsNotTheProjectsState(t *testing.T) {
	r := newGitRepo(t)
	base := r.git("rev-parse", "HEAD")
	r.commit("own code", map[string]string{"server/internal/engine/designhealth/a.go": "package x\n"})
	w, wbase := workspaceRepo(t)
	w.commit("stray state", map[string]string{".aiarch/state/project.json": "{}\n"})
	spec := writeSpec(t, workspaceScope)
	rng, err := resolveEditScopeRange(r.root, base, r.git("rev-parse", "HEAD"), r.git("rev-parse", "main"), false)
	if err != nil {
		t.Fatal(err)
	}
	got, err := editScopeViolations(r.root, spec, rng, w.rangeOf(wbase))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"@plat/.aiarch/state/project.json"}; !slices.Equal(got, want) {
		t.Fatalf("violations = %v, want %v", got, want)
	}
}

// TestEditScope_AMalformedWorkspaceGlobIsRefused: "@/x" names no repo, so it must never be read
// as the project's own "x".
func TestEditScope_AMalformedWorkspaceGlobIsRefused(t *testing.T) {
	r := newGitRepo(t)
	base := r.git("rev-parse", "HEAD")
	r.commit("x", map[string]string{"x/a.go": "package x\n"})
	spec := writeSpec(t, review.SpecEditScope{Extra: []string{"@/x/**"}})
	rng, err := resolveEditScopeRange(r.root, base, r.git("rev-parse", "HEAD"), r.git("rev-parse", "main"), false)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := editScopeViolations(r.root, spec, rng); err == nil {
		t.Fatalf("a glob naming no repo was read: violations %v", got)
	}
}

// TestRunCommandCheckInAWorkspaceRepo: a "@<repo>/<dir>" check dir runs in that repo's
// checkout, and one naming a repo the venue did not hand over fails the check.
func TestRunCommandCheckInAWorkspaceRepo(t *testing.T) {
	root, plat := t.TempDir(), t.TempDir()
	var ran string
	r := checkRunner{root: root, outDir: t.TempDir(), now: time.Now, workspaces: map[string]string{"plat": plat},
		exec: func(_ context.Context, dir string, _ []string, _ ...string) ([]byte, error) {
			ran = dir
			return nil, nil
		}}
	dir, missing := "@plat/framework-go/methodcheck", "@nope/x"
	res := r.Run(context.Background(), []review.SpecCheck{
		{ID: "command:in", Kind: "command", Source: review.SpecCheckSource{Argv: []string{"x"}, Dir: &dir}, TimeoutSec: 5},
		{ID: "command:missing", Kind: "command", Source: review.SpecCheckSource{Argv: []string{"x"}, Dir: &missing}, TimeoutSec: 5},
	})
	if res[0].Status != "pass" || ran != filepath.Join(plat, "framework-go/methodcheck") {
		t.Fatalf("in-repo check: status %s ran in %q", res[0].Status, ran)
	}
	if res[1].Status != "fail" {
		t.Fatalf("a check naming an unknown workspace repo: status %s, want fail", res[1].Status)
	}
}

// TestWorkspaceRootsFromEnv reads the venue's id → checkout map.
func TestWorkspaceRootsFromEnv(t *testing.T) {
	b, _ := json.Marshal(map[string]string{"plat": "/w/plat"})
	t.Setenv(workspaceRootsEnv, string(b))
	if got := workspaceRootsFromEnv(); got["plat"] != "/w/plat" || len(got) != 1 {
		t.Fatalf("got %v", got)
	}
	t.Setenv(workspaceRootsEnv, "")
	if got := workspaceRootsFromEnv(); len(got) != 0 {
		t.Fatalf("empty env: got %v", got)
	}
	_ = os.Getenv
}
