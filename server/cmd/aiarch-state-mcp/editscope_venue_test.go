package main

// editscope_venue_test.go pins ISS-033-6 parts 2a/2b at the venue: an attempt whose checks are green
// and whose only fault is an out-of-scope commit (RUN-033's stp attempts 2-11) settles as a refused
// scope with a progress fingerprint, and the local venue hands the agent's run_checks the very range
// the job's gate judges, so green in session means green at the job.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

const strayPath = "stray/outside.go"

// strayAgent commits strayPath in the project's worktree (outside every task's scope) and records
// the range the venue handed it in the file rangeOut.
func strayAgent(rangeOut string) string {
	return `printf '%s' "$AIARCH_EDIT_SCOPE_RANGE" > ` + rangeOut + `
mkdir -p stray; printf 'package stray\n' > ` + strayPath + `; git add -- ` + strayPath + `; commit || true`
}

// allChecksPass fails the test unless the CheckRun the srs attempt recorded holds only passes: the
// scenario is a refused scope with the checks green (the srs task's one check is rule:project).
func allChecksPass(t *testing.T, repo string) {
	t.Helper()
	run, ok := checkRunOnBranch(t, repo, "activity/"+freshActivity, freshActivity, "srs")
	if !ok || len(run.Results) == 0 {
		t.Fatal("the branch holds no recorded check results")
	}
	for _, r := range run.Results {
		if r.Status != "pass" {
			t.Fatalf("check %s is %q: the scenario needs green checks", r.CheckId, r.Status)
		}
	}
}

// A refused scope with green checks is not an incomplete verdict: the attempt carries a progress
// fingerprint, and a second identical attempt after a refresh from main carries the same one, so the
// walk can settle the loop as no progress.
func TestEditScopeVenue_ARefusedScopeWithGreenChecksSettlesWithAStableFingerprint(t *testing.T) {
	repo := bornRepo(t, freshState(t))
	ag := installAgent(t)
	ag.does(t, strayAgent(filepath.Join(t.TempDir(), "range.json")))
	access, err := agenticjob.NewLocalExecAgenticJobAccess(repo, "todomvc", stateBinary(t), projectstate.StateToolVersion(), 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	observe := func(n int) agenticjob.PipelineObservation {
		obs := submitSRSAttempt(t, access, repo, n)
		if obs.Phase != agenticjob.PhaseFailed {
			t.Fatalf("attempt %d: phase %d (%s), want failed", n, obs.Phase, obs.Diagnostic)
		}
		return obs
	}
	check := func(n int, obs agenticjob.PipelineObservation) {
		refused, ok := causeOf(obs).(*agenticjob.ToolRefusedByPolicy)
		if !ok || !slices.Contains(refused.Paths, strayPath) {
			t.Fatalf("attempt %d: cause %T %+v, want a refused scope naming %s (%s)", n, causeOf(obs), causeOf(obs), strayPath, obs.Diagnostic)
		}
		if obs.ProgressFingerprint == nil || *obs.ProgressFingerprint == "" {
			t.Fatalf("attempt %d: a refused scope with green checks left no progress fingerprint", n)
		}
	}
	// Attempt 1 leaves the stray on the branch; the later attempts re-affirm it, so their specs are
	// hashed over the state the branch now holds.
	observe(1)
	first := observe(2)
	check(2, first)
	allChecksPass(t, repo)

	// Main moves on with work the agent never touched, and the walk's refresh merges it in.
	writeRepoFile(t, repo, "docs/unrelated-main-change.md", []byte("someone else's work\n"))
	inRepo(t, repo, "git", "add", "docs/unrelated-main-change.md")
	inRepo(t, repo, "git", "-c", "user.name=other", "-c", "user.email=other@example.com", "commit", "-q", "-m", "main moves on")
	if obs := jobOf(t, access, freshActivity, agenticjob.DispatchJobRefresh); obs.Phase != agenticjob.PhaseSucceeded {
		t.Fatalf("the refresh did not succeed: %s", obs.Diagnostic)
	}
	second := observe(3)
	check(3, second)
	if *first.ProgressFingerprint != *second.ProgressFingerprint {
		t.Errorf("the fingerprint changed (%s then %s) though the agent's output did not", *first.ProgressFingerprint, *second.ProgressFingerprint)
	}
}

var fullSHAPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// The local venue stamps the gate's range on the agent's rig in the shape run_checks decodes, and
// that range makes the agent's in-session run_checks refuse the same out-of-scope commit the job's
// gate refuses.
func TestEditScopeVenue_TheAgentsRigCarriesTheRangeRunChecksJudges(t *testing.T) {
	repo, plat, _, access, spec, ag := workspaceConstruction(t)
	rangeOut := filepath.Join(t.TempDir(), "range.json")
	ag.does(t, strayAgent(rangeOut))
	if obs := constructionOf(t, access, spec, 1); obs.Phase != agenticjob.PhaseFailed {
		t.Fatalf("attempt 1: %+v", obs)
	}
	raw, err := os.ReadFile(rangeOut) //nolint:gosec // G304: a file the test's own script wrote.
	if err != nil || strings.TrimSpace(string(raw)) == "" {
		t.Fatalf("the agent was handed no AIARCH_EDIT_SCOPE_RANGE (%v)", err)
	}
	var in editScopeSessionRange
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		t.Fatalf("run_checks cannot decode the range the venue stamped: %v\n%s", err, raw)
	}
	if !fullSHAPattern.MatchString(in.Base) || !fullSHAPattern.MatchString(in.Main) || len(in.Workspaces) != 1 || in.Workspaces[0].ID != "plat" {
		t.Fatalf("the stamped range is not the gate's: %s", raw)
	}

	// Replay the agent's run_checks: its worktree holds the branch the job gated, and the workspace
	// worktree the job removed is replaced by one on the same branch.
	own := filepath.Join(t.TempDir(), "own")
	inRepo(t, repo, "git", "worktree", "add", "-q", own, "activity/"+freshActivity)
	platWT := filepath.Join(t.TempDir(), "plat")
	inRepo(t, plat, "git", "worktree", "add", "-q", platWT, "activity/"+freshActivity)
	in.Workspaces[0].Dir = platWT
	replay, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	s := &Session{Mode: jobModeConstruct, StateRoot: own, ActivityID: freshActivity, TaskID: "construction",
		ComponentID: freshComponent, git: runGit, checkExec: passExec, EditScopeRange: string(replay)}
	_, out := mustRunChecks(t, connectInMemory(t, s), map[string]any{})
	es := editScopeResult(out)
	if es == nil || es.Status != checkFail || !strings.Contains(es.Tail, strayPath) {
		t.Fatalf("the in-session run_checks does not refuse %s: %+v", strayPath, out)
	}
}
