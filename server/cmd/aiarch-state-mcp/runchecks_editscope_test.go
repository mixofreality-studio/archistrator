package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// passExec passes every check process, so the only red a run can show is the edit-scope gate.
func passExec(context.Context, string, []string, ...string) ([]byte, error) {
	return []byte("ok\n"), nil
}

// gatedCheckout is a committed own-project checkout on main, switched to the activity branch.
type gatedCheckout struct {
	t    *testing.T
	root string
	main string
}

func newGatedCheckout(t *testing.T) gatedCheckout {
	t.Helper()
	root := seedOwnCheckout(t)
	inRepo(t, root, "git", "init", "-q", "-b", "main")
	inRepo(t, root, "git", "config", "commit.gpgsign", "false")
	inRepo(t, root, "git", "add", "-A")
	inRepo(t, root, "git", "commit", "-q", "-m", "seed")
	g := gatedCheckout{t: t, root: root, main: g1(t, root, "rev-parse", "HEAD")}
	inRepo(t, root, "git", "checkout", "-q", "-b", "activity/C-agentic-job-access")
	return g
}

func g1(t *testing.T, root string, args ...string) string {
	t.Helper()
	return strings.TrimSpace(inRepo(t, root, append([]string{"git"}, args...)...))
}

// commit commits one file on the activity branch and returns the new head.
func (g gatedCheckout) commit(rel string) string {
	g.t.Helper()
	writeRepoFile(g.t, g.root, rel, []byte("package x\n"))
	inRepo(g.t, g.root, "git", "add", "--", rel)
	inRepo(g.t, g.root, "git", "commit", "-q", "-m", "edit "+rel)
	return g1(g.t, g.root, "rev-parse", "HEAD")
}

func (g gatedCheckout) rangeEnv(base string) string {
	b, err := json.Marshal(map[string]string{"base": base, "main": g.main})
	if err != nil {
		g.t.Fatal(err)
	}
	return string(b)
}

func (g gatedCheckout) session(envRange string) *Session {
	return &Session{Mode: jobModeConstruct, StateRoot: g.root, ActivityID: "C-agentic-job-access", TaskID: "construction",
		ComponentID: "agentic-job-access", git: runGit, checkExec: passExec, EditScopeRange: envRange}
}

// editScopeResult is the "edit-scope" failure of out, nil when there is none.
func editScopeResult(out runChecksOutput) *runChecksFailure {
	i := slices.IndexFunc(out.Results, func(f runChecksFailure) bool { return f.ID == editScopeCheckID })
	if i < 0 {
		return nil
	}
	return &out.Results[i]
}

const (
	inScopePath    = "server/internal/resourceaccess/agenticjob/x.go"
	outOfScopePath = "server/internal/elsewhere/x.go"
)

// a: a committed head changing a path outside the task's scope is red and named; without the
// env there is no edit-scope result at all.
func TestRunChecksVerb_EditScopeRedForAPathOutsideTheTask(t *testing.T) {
	g := newGatedCheckout(t)
	g.commit(outOfScopePath)

	_, off := mustRunChecks(t, connectInMemory(t, g.session("")), map[string]any{})
	if editScopeResult(off) != nil {
		t.Fatalf("an ungated session must add nothing: %+v", off)
	}

	cs := connectInMemory(t, g.session(g.rangeEnv(g.main)))
	for _, args := range []map[string]any{{}, {"failedOnly": true}} {
		text, out := mustRunChecks(t, cs, args)
		es := editScopeResult(out)
		if es == nil || es.Status != checkFail || es.Title != editScopeCheckTitle || !strings.Contains(es.Tail, outOfScopePath) {
			t.Fatalf("args %v: want a failing edit-scope result naming %s: %+v", args, outOfScopePath, out)
		}
		if out.Failed != off.Failed+1 || !strings.Contains(text, outOfScopePath) {
			t.Fatalf("args %v: the failure must be counted and shown: %s", args, text)
		}
	}
	if _, err := os.Stat(filepath.Join(g.root, checksOutDir, resultsFile)); err != nil {
		t.Fatal(err)
	}
	var recorded []CheckResult
	b, _ := os.ReadFile(filepath.Join(g.root, checksOutDir, resultsFile))
	if err := json.Unmarshal(b, &recorded); err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(recorded, func(r CheckResult) bool { return r.CheckId == editScopeCheckID }) {
		t.Fatal("the edit-scope result must not be written into the results file")
	}
}

// b: RUN-033's shape. An earlier attempt left a path outside every task's scope; this attempt's
// base is already past it, so only the activity-level net diff sees it.
func TestRunChecksVerb_EditScopeRedForAnEarlierAttemptsStray(t *testing.T) {
	g := newGatedCheckout(t)
	base := g.commit(outOfScopePath)
	g.commit(inScopePath)

	_, out := mustRunChecks(t, connectInMemory(t, g.session(g.rangeEnv(base))), map[string]any{})
	es := editScopeResult(out)
	if es == nil || es.Status != checkFail || out.Failed < 1 {
		t.Fatalf("want a failing edit-scope result: %+v", out)
	}
	if !strings.Contains(es.Tail, "outside the edit scope of every task of this activity") || !strings.Contains(es.Tail, outOfScopePath) {
		t.Fatalf("want the activity-level message naming the stray: %s", es.Tail)
	}
	if strings.Contains(es.Tail, "outside the task's edit scope") {
		t.Fatalf("this attempt changed nothing outside its scope: %s", es.Tail)
	}
}

// c: a head within scope passes the pseudo-check, which counts as one more pass.
func TestRunChecksVerb_EditScopeGreenWithinScope(t *testing.T) {
	g := newGatedCheckout(t)
	g.commit(inScopePath)

	_, off := mustRunChecks(t, connectInMemory(t, g.session("")), map[string]any{})
	_, on := mustRunChecks(t, connectInMemory(t, g.session(g.rangeEnv(g.main))), map[string]any{})
	if editScopeResult(on) != nil || on.Failed != off.Failed || on.Passed != off.Passed+1 {
		t.Fatalf("an in-scope head must pass as one more check: off %+v, on %+v", off, on)
	}
}

// d: a malformed range, or one git cannot resolve, is an error result, never a silent pass.
func TestRunChecksVerb_EditScopeMalformedRangeIsAnError(t *testing.T) {
	g := newGatedCheckout(t)
	g.commit(inScopePath)
	for name, env := range map[string]string{
		"not json":      "{nope",
		"short base":    `{"base":"abc","main":""}`,
		"unknown field": `{"base":"` + g.main + `","extra":1}`,
		"missing base":  `{"base":"` + strings.Repeat("0", 40) + `","main":""}`,
	} {
		_, out := mustRunChecks(t, connectInMemory(t, g.session(env)), map[string]any{})
		es := editScopeResult(out)
		if es == nil || es.Status != checkError || es.Tail == "" {
			t.Fatalf("%s: want an error result: %+v", name, out)
		}
	}
}

// e: the subcommand and the verb give the same message on one fixture.
func TestEditScope_SubcommandAndVerbAgree(t *testing.T) {
	g := newGatedCheckout(t)
	base := g.commit(outOfScopePath)
	g.commit(inScopePath)
	g.commit("server/cmd/other/y.go")

	_, out := mustRunChecks(t, connectInMemory(t, g.session(g.rangeEnv(base))), map[string]any{})
	es := editScopeResult(out)
	if es == nil {
		t.Fatalf("want a failing edit-scope result: %+v", out)
	}

	p, err := readCheckoutProject(g.root)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := deriveTaskSpec(&p, "C-agentic-job-access", "construction")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(t.TempDir(), "spec.json")
	if err := os.WriteFile(specPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(g.root)
	err = runEditScope([]string{"--spec", specPath, "--base", base, "--head", "HEAD", "--main", g.main})
	wantExit(t, err, 1, "server/cmd/other/y.go", outOfScopePath)
	if err.Error() != es.Tail {
		t.Fatalf("the subcommand and the verb disagree:\nsubcommand: %s\nverb:       %s", err.Error(), es.Tail)
	}
}
