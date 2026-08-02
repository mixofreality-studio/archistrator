package agenticjob

// SERVICE TEST PLAN (STP) — agenticJobAccess (C-CP-R rework, GitHub
// Actions). Per [[the-method-testing]], the STP enumerates every way to demonstrate
// the component does NOT work; written before/with the code; black-box at the RA's
// public verbs, faking ONLY the external GitHub Actions boundary (the ghActionsClient
// seam). NO live GitHub, NO BDD.
//
//   PRE-CONDITION / CONTRACT-MISUSE:
//     U1  New rejects a nil actions client                          → ContractMisuse
//     U2  Submit rejects an empty idempotencyKey                    → ContractMisuse
//     U3  Submit rejects a spec with no steps                       → ContractMisuse
//     U4  Submit rejects a duplicate step name                      → ContractMisuse
//     U5  Submit rejects an empty step name                         → ContractMisuse
//     U6  Submit rejects a dangling edge                            → ContractMisuse
//     U7  Observe / Cancel reject a zero PipelineHandle             → ContractMisuse
//     U8  Observe / Cancel reject a malformed PipelineHandle        → ContractMisuse
//
//   HAPPY-PATH / MAPPING:
//     U9  Submit happy path: dispatch once, return a non-zero handle addressing the
//         created run; the dispatch carried the idempotency-token input
//     U10 Observe QUEUED → PhasePending; IN_PROGRESS → PhaseRunning
//     U11 Observe TERMINAL-SUCCESS → PhaseSucceeded, no diagnostic
//     U12 Observe TERMINAL-FAILURE → PhaseFailed, neutral diagnostic (no GH lexeme)
//     U13 Observe TERMINAL-CANCELLED → PhaseCancelled
//     U14 Observe NOT-FOUND (unknown run) → fwra.NotFound
//
//   ERROR-KIND MAPPING:
//     U15 Submit Auth (seam Auth on list/dispatch) propagates fwra.Auth (terminal)
//     U16 Submit Transient (seam Transient) propagates (retryable)
//
//   CANCEL:
//     U17 Cancel RUNNING forwards to the seam; nil error
//     U18 Cancel already-gone (seam NotFound) → no-op SUCCESS
//
//   IDEMPOTENCY CONVERGENCE (THE HARD EXIT GATE — analogous to C-PA-R's ref-CAS gate):
//     G1  Re-submit after the run exists: a second submit with the SAME key returns
//         an EQUAL handle and does NOT dispatch again (probe short-circuit).
//     G2  Two CONCURRENT submits with the SAME key converge on the SAME handle and
//         leave exactly ONE effective (non-cancelled) run — proving the lowest-id
//         canonical selection + sibling-cancel converges without an atomic dedup.
//     G3  Re-submit after completion returns the same handle (still converges).
//
//   VALUE SEMANTICS / MAPPING UNITS:
//     U19 dedupToken determinism; mapPhase table; handle round-trip.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// SERVICE TEST PLAN (STP) — the LOCAL-EXECUTOR realisation (the localexec
// section of agenticjobaccess.go, WORKTREE-PER-ACTIVITY rework). Per
// [[the-method-testing]], black-box at the RA's public verbs. The only external
// boundary faked is the `claude` binary itself (a PATH shim script, the SAME
// pattern framework-go-infrastructure-llm/claudecli_test.go uses for the SAME
// reason: no real subscription touched in CI); every git operation runs against
// a REAL throwaway local repo via the actual `git` binary — no fake for that
// seam.
//
//   PRE-CONDITION / CONTRACT-MISUSE:
//     L1  New rejects empty repoURL / empty stateMCPBin / a stateMCPBin that
//         does not exist on disk / a NON-LOCAL repoURL (worktrees need a local
//         filesystem path — this realisation no longer clones over the wire)
//     L2  Submit rejects an empty idempotencyKey
//     L3  Submit rejects a spec with no steps (validateSpec, shared with the
//         GitHub-Actions realisation above)
//     L4  Submit rejects an empty ActivityID
//     L5  Submit rejects a spec with no DispatchInputs["command"]
//     L6  Observe / Cancel reject a zero / malformed PipelineHandle
//     L7  Observe a well-formed handle with NO in-memory record (a RESTART-LOST run)
//         → TERMINAL PhaseFailed observation + recovery diagnostic (F-R1: terminate,
//         never loop); a KNOWN still-running handle still reports Running
//
//   HAPPY-PATH / DISPATCH SHAPE:
//     LH1 Submit spawns claude with --dangerously-skip-permissions, --mcp-config,
//         --output-format json, -p "/<command> <component> <activity>", cwd on a
//         git WORKTREE of the shared repo checked out onto activity/<id> (no
//         clone, no push — commits advance the shared repo's refs directly); the
//         --mcp-config file carries the exact AIARCH_* envelope (project/
//         component/activity/branch/state-root) pointing at cmd/aiarch-state-mcp;
//         the Tier-2 sandbox --settings file's filesystem allowWrite covers the
//         worktree dir AND the shared repo's git dir (worktree commits write
//         .git/worktrees metadata + shared objects/refs)
//     LH2 On a clean claude exit, the shim's commit has advanced activity/<id>
//         in the SHARED repo, Observe reports PhaseSucceeded, and the worktree
//         is removed (no lingering `git worktree list` entry)
//     LH3 A SECOND phase dispatch for the SAME activity (different idempotencyKey)
//         re-attaches to the EXISTING activity/<id> branch — both commits present,
//         nothing lost
//
//   EXIT-CODE MAPPING (Step 3):
//     LF1 Non-zero claude exit → PhaseFailed, diagnostic names the exit code
//     LF2 claude exceeds the run timeout → PhaseFailed, "timed out" diagnostic,
//         and the subprocess is actually gone (no orphan)
//     LF3 CLEAN claude exit that did NOT advance the activity branch ref (no
//         commit made) → PhaseFailed, never a fake success — the durable
//         post-condition (work landed on the activity branch) does not hold
//
//   WORKTREE LIFECYCLE:
//     LW1 The worktree is removed on the cancel path too (SIGTERM'd run)
//     LW2 New prunes STALE worktree metadata left by a crashed prior process
//         (`git worktree prune` on executor startup)
//
//   CANCEL:
//     LC1 Cancel while running → SIGTERM's the subprocess; Observe converges to
//         PhaseCancelled (never PhaseFailed) even though Wait() itself errors
//     LC2 Cancel of an unknown / already-terminal handle → no-op SUCCESS
//
//   IDEMPOTENCY CONVERGENCE:
//     LG1 Two submits with the SAME idempotencyKey return the SAME handle and
//         spawn claude exactly ONCE (probe short-circuit, no dispatch storm)
//
//   VALUE SEMANTICS / MAPPING UNITS:
//     L8  classifyLocalExecFailure: exit-code text, stderr tail bounding
//     L9  localTokenFromHandle round-trip + rejects a foreign "run/<id>" shape
//
//   LOCAL MERGE JOB (local-merge-and-policy Commit 1; DispatchInputs["job"]="merge"):
//     LM1 A merge-job submit merges activity/<id> into main with a REAL --no-ff
//         merge commit (two parents), deletes the activity branch, and Observe
//         reports PhaseSucceeded
//     LM2 A merge CONFLICT reports PhaseFailed with a "merge conflict"
//         diagnostic and leaves the shared repo UNTOUCHED (main unmoved, the
//         activity branch still present — no partial merge)
//     LM3 An already-merged activity branch (prior attempt crashed before the
//         delete) is idempotent: no second merge commit, the branch delete
//         completes, PhaseSucceeded
//     LM4 A merge-job submit for a branch that does not exist reports
//         PhaseFailed with a diagnostic naming the branch
//     LM5 Two merge-job submits with the SAME idempotencyKey converge on the
//         SAME handle/run (no double merge)
//
//   DESIGN-JOB ARM (local-first-init-funnel Task 8; a NON-EMPTY job_mode discriminates,
//   NO ActivityID — the local counterpart of the seated aiarch-design.yml draft job):
//     LD1 A FIRST-of-session design submit (job_mode ∈ {draft,critique,answer}) whose
//         session branch does not yet exist CREATES it off main (the local stand-in for
//         the cloud's server-side beginSession/OpenBranch — the branch-staging rail is
//         dormant locally), worktrees on it (so the tree sees main's committed state),
//         spawns claude with the BARE "-p /<command>" prompt (no component/activity args),
//         and the --mcp-config envelope is the EXACT aiarch-design.yml set —
//         PROJECT_ID/ARTIFACT_KIND/JOB_MODE/TARGET_BRANCH/STATE_ROOT, with NO
//         AIARCH_COMPONENT_ID/ACTIVITY_ID; the drafted commit advances the session
//         branch, Observe reports PhaseSucceeded, the worktree is removed
//     LD2 A design-job submit missing DispatchInputs["command"]      → ContractMisuse
//     LD3 A design-job submit missing DispatchInputs["target_branch"] → ContractMisuse
//     LD4 A MID-session design submit whose session branch already exists re-attaches to
//         its TIP (never recreated/reset): the prior session commits are preserved and
//         the drafted commit lands on top
//     LD5 Two design-job submits with the SAME idempotencyKey converge on the
//         SAME handle and spawn claude exactly ONCE (no re-dispatch)
//
//   SEAT-ASSETS (both arms; the .claude prompt-surface materialization the local arm
//   was missing — the "Unknown command: /<command>" no-commit root cause):
//     LS1 The executor runs `aiarch-state-mcp seat-assets --dest <workDir>` BEFORE it
//         spawns claude (the local mirror of both seated workflows' materialize step),
//         so the slash command resolves — proven behaviorally: a recording state-mcp
//         stub writes a marker into --dest and the claude shim requires that marker
//         before committing, so PhaseSucceeded ⟺ seat ran first in claude's worktree,
//         and the recorded argv proves the `seat-assets --dest <workDir>` shape

// ---------------------------------------------------------------------------
// fakeActions — the seam stand-in. Models GitHub's NON-dedup dispatch: each
// dispatch creates a NEW run (monotonic id) carrying the run-name; list-by-name
// returns every run with that name. Concurrency-safe so the convergence gate can
// race two submits. No live GitHub, ever.
// ---------------------------------------------------------------------------

type fakeRun struct {
	id         int64
	name       string
	status     string
	conclusion string
	htmlURL    string
}

type fakeActions struct {
	mu     sync.Mutex
	nextID int64
	runs   []fakeRun

	dispatchCount int
	cancelled     map[int64]bool

	// lastDispatchInputs records the exact input map the seam was asked to dispatch
	// (the RA-merged token + the caller's extra DispatchInputs), so the additive
	// pass-through can be asserted. The RA-controlled idempotency token is merged in
	// by the SEAM's concrete realisation (actions_http_client.go), not here; the fake
	// records what the RA forwarded across the seam interface (extras + token).
	lastDispatchToken  string
	lastDispatchInputs map[string]string

	// lastTarget records the ghTarget the seam was asked to dispatch/observe/cancel
	// against, so the per-project-design-dispatch retargeting can be asserted (a zero
	// target == the configured construction repo default; a non-zero target == the
	// per-project repo + aiarch-design.yml).
	lastDispatchTarget ghTarget
	lastGetTarget      ghTarget
	lastCancelTarget   ghTarget

	// scripted errors on the next matching call.
	listErr     error
	dispatchErr error
	getErr      error
	cancelErr   error
}

func newFakeActions() *fakeActions {
	return &fakeActions{nextID: 1, cancelled: map[int64]bool{}}
}

func (f *fakeActions) listRunsByName(_ context.Context, _ ghTarget, runName string) ([]ghRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := []ghRun{}
	for _, r := range f.runs {
		if r.name == runName {
			out = append(out, ghRun(r))
		}
	}
	return out, nil
}

func (f *fakeActions) dispatch(_ context.Context, tgt ghTarget, token, runName string, dispatchInputs map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.dispatchErr != nil {
		return f.dispatchErr
	}
	f.dispatchCount++
	f.lastDispatchToken = token
	f.lastDispatchTarget = tgt
	// Record the EFFECTIVE input map the way the concrete seam builds it: the
	// caller's extras first, the RA-controlled idempotency token stamped LAST so it
	// wins any collision. This mirrors actions_http_client.go's merge so the
	// pass-through + token-wins discipline can be asserted at the seam boundary.
	merged := make(map[string]string, len(dispatchInputs)+1)
	maps.Copy(merged, dispatchInputs)
	merged["idempotency_token"] = token
	f.lastDispatchInputs = merged
	id := f.nextID
	f.nextID++
	f.runs = append(f.runs, fakeRun{id: id, name: runName, status: "queued"})
	return nil
}

func (f *fakeActions) getRun(_ context.Context, tgt ghTarget, runID int64) (ghRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastGetTarget = tgt
	if f.getErr != nil {
		return ghRun{}, f.getErr
	}
	for _, r := range f.runs {
		if r.id == runID {
			return ghRun(r), nil
		}
	}
	return ghRun{}, fwra.New(fwra.NotFound, "fake: no run")
}

func (f *fakeActions) cancelRun(_ context.Context, tgt ghTarget, runID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastCancelTarget = tgt
	if f.cancelErr != nil {
		return f.cancelErr
	}
	found := false
	for i := range f.runs {
		if f.runs[i].id == runID {
			f.runs[i].status = "completed"
			f.runs[i].conclusion = "cancelled"
			f.cancelled[runID] = true
			found = true
		}
	}
	if !found {
		return fwra.New(fwra.NotFound, "fake: no run") // already gone == success at the RA
	}
	return nil
}

// newAccessForTest builds an Access with a synchronous resolve (the fake's dispatch
// creates the run immediately, so one resolve attempt and no delay suffices).
func newAccessForTest(t *testing.T, f *fakeActions) *access {
	t.Helper()
	a, err := newAccess(f)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.resolveAttempts = 1
	a.resolveDelay = 0
	return a
}

func goodSpec() PipelineSpec {
	return PipelineSpec{
		ProjectID:  "p1",
		ActivityID: "C-X",
		Steps:      []PipelineStep{{Name: "build", Toolchain: "go-1.23", Command: []string{"go", "build"}}},
	}
}

func kind(err error) fwra.Kind {
	var fe *fwra.Error
	if errors.As(err, &fe) {
		return fe.Kind
	}
	return fwra.Unknown
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

func itoaTest(n int64) string { return strconv.FormatInt(n, 10) }

// subRC builds the ResourceAccess call Context for a SubmitAgenticJob call —
// the cross-cutting ctx + caller-supplied idempotencyKey now ride on fwra.Context
// (the RA-context bootstrap) instead of being explicit Submit params.
func subRC(ctx context.Context, key fwra.IdempotencyKey) fwra.Context {
	return fwra.Context{Context: ctx, IdempotencyKey: key}
}

// obsRC builds the call Context for the read verbs (Observe / Cancel), which carry no
// idempotency key.
func obsRC(ctx context.Context) fwra.Context {
	return fwra.Context{Context: ctx}
}

func TestNewRejectsNilClient(t *testing.T) {
	if _, err := newAccess(nil); kind(err) != fwra.ContractMisuse {
		t.Fatalf("New(nil) kind = %v, want ContractMisuse", kind(err))
	}
}

func TestSubmitContractMisuse(t *testing.T) {
	a := newAccessForTest(t, newFakeActions())
	ctx := context.Background()

	if _, err := a.SubmitAgenticJob(subRC(ctx, ""), goodSpec()); kind(err) != fwra.ContractMisuse {
		t.Fatalf("empty key kind = %v", kind(err))
	}
	noSteps := goodSpec()
	noSteps.Steps = nil
	if _, err := a.SubmitAgenticJob(subRC(ctx, "k"), noSteps); kind(err) != fwra.ContractMisuse {
		t.Fatalf("no steps kind = %v", kind(err))
	}
	dup := goodSpec()
	dup.Steps = []PipelineStep{{Name: "x"}, {Name: "x"}}
	if _, err := a.SubmitAgenticJob(subRC(ctx, "k"), dup); kind(err) != fwra.ContractMisuse {
		t.Fatalf("dup step kind = %v", kind(err))
	}
	empty := goodSpec()
	empty.Steps = []PipelineStep{{Name: "  "}}
	if _, err := a.SubmitAgenticJob(subRC(ctx, "k"), empty); kind(err) != fwra.ContractMisuse {
		t.Fatalf("empty step name kind = %v", kind(err))
	}
	dangling := goodSpec()
	dangling.Edges = []StepDependency{{From: "build", To: "nope"}}
	if _, err := a.SubmitAgenticJob(subRC(ctx, "k"), dangling); kind(err) != fwra.ContractMisuse {
		t.Fatalf("dangling edge kind = %v", kind(err))
	}
}

func TestObserveCancelHandleMisuse(t *testing.T) {
	a := newAccessForTest(t, newFakeActions())
	ctx := context.Background()
	if _, err := a.ObserveAgenticJob(obsRC(ctx), PipelineHandle("")); kind(err) != fwra.ContractMisuse {
		t.Fatalf("zero handle observe kind = %v", kind(err))
	}
	if err := a.CancelAgenticJob(obsRC(ctx), PipelineHandle("")); kind(err) != fwra.ContractMisuse {
		t.Fatalf("zero handle cancel kind = %v", kind(err))
	}
	bad := ParsePipelineHandle("garbage-no-slash")
	if _, err := a.ObserveAgenticJob(obsRC(ctx), bad); kind(err) != fwra.ContractMisuse {
		t.Fatalf("malformed handle observe kind = %v", kind(err))
	}
}

func TestSubmitHappyPath(t *testing.T) {
	f := newFakeActions()
	a := newAccessForTest(t, f)
	h, err := a.SubmitAgenticJob(subRC(context.Background(), "key-1"), goodSpec())
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if PipelineHandleIsZero(h) {
		t.Fatal("handle is zero")
	}
	if f.dispatchCount != 1 {
		t.Fatalf("dispatchCount = %d, want 1", f.dispatchCount)
	}
	if PipelineHandleString(h) != "run/1" {
		t.Fatalf("handle = %q, want run/1", PipelineHandleString(h))
	}
}

// TestSubmitForwardsDispatchInputs is the focused C-MSD-Δ Part-1 assertion: the
// additive PipelineSpec.DispatchInputs extra keys reach the dispatched inputs, and
// the RA-controlled idempotency_token stays RA-controlled (a caller-supplied
// idempotency_token in DispatchInputs is OVERWRITTEN by the RA's value).
func TestSubmitForwardsDispatchInputs(t *testing.T) {
	// U20a — extra design-dispatch inputs ride through to the dispatched inputs map.
	t.Run("extra_keys_reach_dispatch", func(t *testing.T) {
		f := newFakeActions()
		a := newAccessForTest(t, f)
		spec := goodSpec()
		spec.DispatchInputs = map[string]string{
			"artifact_kind":   "Mission",
			"design_prompt":   "draft the mission, architect role",
			"target_branch":   "aiarch-design-mission",
			"prior_state_ref": "",
		}
		if _, err := a.SubmitAgenticJob(subRC(context.Background(), "key-di"), spec); err != nil {
			t.Fatalf("submit: %v", err)
		}
		for _, k := range []string{"artifact_kind", "design_prompt", "target_branch", "prior_state_ref"} {
			if _, ok := f.lastDispatchInputs[k]; !ok {
				t.Fatalf("dispatch inputs missing forwarded key %q; got %v", k, f.lastDispatchInputs)
			}
		}
		if got := f.lastDispatchInputs["artifact_kind"]; got != "Mission" {
			t.Fatalf("artifact_kind = %q, want Mission", got)
		}
		// The RA still stamps a non-empty idempotency token (its own derived value).
		if f.lastDispatchInputs["idempotency_token"] == "" {
			t.Fatal("idempotency_token not stamped by the RA")
		}
		if f.lastDispatchInputs["idempotency_token"] != f.lastDispatchToken {
			t.Fatal("token in inputs map diverged from the RA-supplied token")
		}
	})

	// U20b — the idempotency token stays RA-controlled: a caller that smuggles an
	// idempotency_token into DispatchInputs cannot override the RA's derived value.
	t.Run("idempotency_token_stays_RA_controlled", func(t *testing.T) {
		f := newFakeActions()
		a := newAccessForTest(t, f)
		spec := goodSpec()
		spec.DispatchInputs = map[string]string{
			"artifact_kind":     "Glossary",
			"idempotency_token": "SPOOFED-BY-CALLER",
		}
		if _, err := a.SubmitAgenticJob(subRC(context.Background(), "key-spoof"), spec); err != nil {
			t.Fatalf("submit: %v", err)
		}
		got := f.lastDispatchInputs["idempotency_token"]
		if got == "SPOOFED-BY-CALLER" {
			t.Fatal("caller-supplied idempotency_token WON — RA must overwrite it (token not RA-controlled)")
		}
		if got != f.lastDispatchToken || got == "" {
			t.Fatalf("idempotency_token = %q, want the RA-derived token %q", got, f.lastDispatchToken)
		}
		// the legitimate extra key is still forwarded.
		if f.lastDispatchInputs["artifact_kind"] != "Glossary" {
			t.Fatalf("artifact_kind = %q, want Glossary", f.lastDispatchInputs["artifact_kind"])
		}
	})

	// U20c — nil DispatchInputs (the existing UC3 construction caller) is untouched:
	// the dispatch still carries exactly the RA token.
	t.Run("nil_dispatch_inputs_untouched", func(t *testing.T) {
		f := newFakeActions()
		a := newAccessForTest(t, f)
		spec := goodSpec() // DispatchInputs is nil
		if _, err := a.SubmitAgenticJob(subRC(context.Background(), "key-nil"), spec); err != nil {
			t.Fatalf("submit: %v", err)
		}
		if len(f.lastDispatchInputs) != 1 {
			t.Fatalf("nil DispatchInputs produced %d inputs, want exactly 1 (the token); got %v",
				len(f.lastDispatchInputs), f.lastDispatchInputs)
		}
		if f.lastDispatchInputs["idempotency_token"] == "" {
			t.Fatal("idempotency_token not stamped on the nil-inputs path")
		}
	})
}

// TestSubmitPerProjectTargetRetargetsDispatchAndHandle is the focused
// per-project-design-dispatch assertion (the live-activation gap fix): a non-zero
// PipelineSpec.TargetRepo + WorkflowFile RETARGETS the dispatch at the per-project
// repo + aiarch-design.yml, and the returned handle ENCODES that target so a later
// Observe/Cancel re-addresses the SAME per-project repo (not the construction repo).
// A zero TargetRepo leaves the handle in the legacy "run/<id>" shape (UC3 untouched).
func TestSubmitPerProjectTargetRetargetsDispatchAndHandle(t *testing.T) {
	// PER-PROJECT DESIGN dispatch: the target overrides ride into the seam call AND
	// the returned handle, so Observe/Cancel re-address the per-project repo.
	t.Run("design_dispatch_targets_per_project_repo", func(t *testing.T) {
		f := newFakeActions()
		a := newAccessForTest(t, f)
		spec := goodSpec()
		spec.TargetRepo = RepoTarget{Owner: "acme", Name: "my-system"}
		spec.WorkflowFile = "aiarch-design.yml"

		h, err := a.SubmitAgenticJob(subRC(context.Background(), "key-pp"), spec)
		if err != nil {
			t.Fatalf("submit: %v", err)
		}
		want := ghTarget{owner: "acme", repo: "my-system", workflowFile: "aiarch-design.yml"}
		if f.lastDispatchTarget != want {
			t.Fatalf("dispatch target = %+v, want %+v (per-project repo + aiarch-design.yml)", f.lastDispatchTarget, want)
		}
		// The handle encodes the target so the stateless Observe/Cancel can re-address it.
		if PipelineHandleString(h) != "run/1@acme/my-system/aiarch-design.yml" {
			t.Fatalf("handle = %q, want run/1@acme/my-system/aiarch-design.yml", PipelineHandleString(h))
		}
		assertObserveCancelReaddressTarget(t, a, f, h, want)
	})

	// UC3 CONSTRUCTION dispatch (zero TargetRepo): the handle stays the legacy
	// "run/<id>" form and the seam sees a ZERO target (falls back to the construction
	// repo default) — byte-for-byte unchanged.
	t.Run("construction_dispatch_zero_target_legacy_handle", func(t *testing.T) {
		f := newFakeActions()
		a := newAccessForTest(t, f)
		h, err := a.SubmitAgenticJob(subRC(context.Background(), "key-uc3"), goodSpec())
		if err != nil {
			t.Fatalf("submit: %v", err)
		}
		if !f.lastDispatchTarget.isZero() {
			t.Fatalf("UC3 dispatch target = %+v, want zero (construction-repo default)", f.lastDispatchTarget)
		}
		if PipelineHandleString(h) != "run/1" {
			t.Fatalf("UC3 handle = %q, want legacy run/1", PipelineHandleString(h))
		}
		if _, err := a.ObserveAgenticJob(obsRC(context.Background()), h); err != nil {
			t.Fatalf("observe: %v", err)
		}
		if !f.lastGetTarget.isZero() {
			t.Fatalf("UC3 observe target = %+v, want zero", f.lastGetTarget)
		}
	})

	// A per-project handle round-trips through HandleFromString (the Manager persists
	// the handle as a plain string across the Activity boundary).
	t.Run("per_project_handle_round_trips", func(t *testing.T) {
		f := newFakeActions()
		a := newAccessForTest(t, f)
		spec := goodSpec()
		spec.TargetRepo = RepoTarget{Owner: "o", Name: "r"}
		spec.WorkflowFile = "aiarch-design.yml"
		h, err := a.SubmitAgenticJob(subRC(context.Background(), "key-rt"), spec)
		if err != nil {
			t.Fatalf("submit: %v", err)
		}
		rt := ParsePipelineHandle(PipelineHandleString(h))
		if _, err := a.ObserveAgenticJob(obsRC(context.Background()), rt); err != nil {
			t.Fatalf("observe round-tripped handle: %v", err)
		}
		want := ghTarget{owner: "o", repo: "r", workflowFile: "aiarch-design.yml"}
		if f.lastGetTarget != want {
			t.Fatalf("round-trip observe target = %+v, want %+v", f.lastGetTarget, want)
		}
	})
}

// assertObserveCancelReaddressTarget asserts a later Observe AND Cancel on the handle
// re-address the encoded per-project target (not the construction-repo default).
func assertObserveCancelReaddressTarget(t *testing.T, a *access, f *fakeActions, h PipelineHandle, want ghTarget) {
	t.Helper()
	// Observe re-addresses the per-project repo (not the construction default).
	if _, err := a.ObserveAgenticJob(obsRC(context.Background()), h); err != nil {
		t.Fatalf("observe: %v", err)
	}
	if f.lastGetTarget != want {
		t.Fatalf("observe target = %+v, want %+v", f.lastGetTarget, want)
	}
	// Cancel re-addresses the per-project repo too.
	if err := a.CancelAgenticJob(obsRC(context.Background()), h); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if f.lastCancelTarget != want {
		t.Fatalf("cancel target = %+v, want %+v", f.lastCancelTarget, want)
	}
}

func TestObserveStatusMapping(t *testing.T) {
	cases := []struct {
		status, conclusion string
		wantPhase          PipelinePhase
		wantDiag           bool
	}{
		{"queued", "", PhasePending, false},
		{"in_progress", "", PhaseRunning, false},
		{"completed", "success", PhaseSucceeded, false},
		{"completed", "failure", PhaseFailed, true},
		{"completed", "timed_out", PhaseFailed, true},
		{"completed", "cancelled", PhaseCancelled, false},
	}
	for _, tc := range cases {
		f := newFakeActions()
		a := newAccessForTest(t, f)
		h, err := a.SubmitAgenticJob(subRC(context.Background(), "k"), goodSpec())
		if err != nil {
			t.Fatalf("submit: %v", err)
		}
		f.runs[0].status = tc.status
		f.runs[0].conclusion = tc.conclusion
		obs, err := a.ObserveAgenticJob(obsRC(context.Background()), h)
		if err != nil {
			t.Fatalf("observe: %v", err)
		}
		if obs.Phase != tc.wantPhase {
			t.Errorf("(%s/%s) phase = %v, want %v", tc.status, tc.conclusion, obs.Phase, tc.wantPhase)
		}
		if (obs.Diagnostic != "") != tc.wantDiag {
			t.Errorf("(%s/%s) diagnostic = %q, wantDiag=%v", tc.status, tc.conclusion, obs.Diagnostic, tc.wantDiag)
		}
		// neutral diagnostic must carry no GitHub-Actions lexeme.
		for _, lex := range []string{"workflow", "run_id", "github", "dispatch", "actions"} {
			if obs.Diagnostic != "" && containsFold(obs.Diagnostic, lex) {
				t.Errorf("diagnostic %q leaks lexeme %q", obs.Diagnostic, lex)
			}
		}
	}
}

// QA F15 gap 2b + F-GTD-6 — EVERY observation the realisation resolved the run URL for
// carries it: terminal failures surface it as the operator's "why" pointer, and a live
// (in_progress / queued) or succeeded run surfaces it as the "view the run" deep-link
// the generating view renders while the job drafts.
func TestObserveSurfacesRunURL(t *testing.T) {
	cases := []struct {
		status     string
		conclusion string
	}{
		{"completed", "failure"},
		{"completed", "cancelled"},
		{"completed", "success"},
		{"in_progress", ""},
		{"queued", ""},
	}
	for _, tc := range cases {
		f := newFakeActions()
		a := newAccessForTest(t, f)
		h, err := a.SubmitAgenticJob(subRC(context.Background(), "k"), goodSpec())
		if err != nil {
			t.Fatalf("submit: %v", err)
		}
		f.runs[0].status = tc.status
		f.runs[0].conclusion = tc.conclusion
		f.runs[0].htmlURL = "https://github.com/acme/widgets/actions/runs/42"
		obs, err := a.ObserveAgenticJob(obsRC(context.Background()), h)
		if err != nil {
			t.Fatalf("observe: %v", err)
		}
		if obs.RunURL != "https://github.com/acme/widgets/actions/runs/42" {
			t.Errorf("(%s/%s) want RunURL surfaced on every resolvable observation, got %q", tc.status, tc.conclusion, obs.RunURL)
		}
	}
	// An unresolvable URL (the realisation could not build it) stays empty — never fabricated.
	f := newFakeActions()
	a := newAccessForTest(t, f)
	h, err := a.SubmitAgenticJob(subRC(context.Background(), "k"), goodSpec())
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	f.runs[0].status = "in_progress"
	f.runs[0].htmlURL = ""
	obs, err := a.ObserveAgenticJob(obsRC(context.Background()), h)
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	if obs.RunURL != "" {
		t.Errorf("an unresolvable run URL must stay empty, got %q", obs.RunURL)
	}
}

// runHTMLURL builds the run's browser URL from owner/repo/run id; unknown owner/repo → "".
func TestRunHTMLURL(t *testing.T) {
	if got := runHTMLURL("acme", "widgets", 42); got != "https://github.com/acme/widgets/actions/runs/42" {
		t.Fatalf("runHTMLURL = %q", got)
	}
	if got := runHTMLURL("", "widgets", 42); got != "" {
		t.Fatalf("runHTMLURL with empty owner must be empty, got %q", got)
	}
}

func TestObserveNotFound(t *testing.T) {
	f := newFakeActions()
	a := newAccessForTest(t, f)
	if _, err := a.ObserveAgenticJob(obsRC(context.Background()), ParsePipelineHandle("run/999")); kind(err) != fwra.NotFound {
		t.Fatalf("observe unknown kind = %v, want NotFound", kind(err))
	}
}

func TestSubmitErrorKinds(t *testing.T) {
	f := newFakeActions()
	f.listErr = fwra.New(fwra.Auth, "denied")
	a := newAccessForTest(t, f)
	if _, err := a.SubmitAgenticJob(subRC(context.Background(), "k"), goodSpec()); kind(err) != fwra.Auth {
		t.Fatalf("auth submit kind = %v", kind(err))
	}

	f2 := newFakeActions()
	f2.dispatchErr = fwra.New(fwra.Transient, "blip")
	a2 := newAccessForTest(t, f2)
	if _, err := a2.SubmitAgenticJob(subRC(context.Background(), "k"), goodSpec()); kind(err) != fwra.Transient {
		t.Fatalf("transient submit kind = %v", kind(err))
	}
}

func TestCancel(t *testing.T) {
	f := newFakeActions()
	a := newAccessForTest(t, f)
	h, _ := a.SubmitAgenticJob(subRC(context.Background(), "k"), goodSpec())
	if err := a.CancelAgenticJob(obsRC(context.Background()), h); err != nil {
		t.Fatalf("cancel running: %v", err)
	}
	// cancel an absent run → seam NotFound → RA success
	if err := a.CancelAgenticJob(obsRC(context.Background()), ParsePipelineHandle("run/999")); err != nil {
		t.Fatalf("cancel absent = %v, want nil", err)
	}
}

// ---------------------------------------------------------------------------
// THE HARD EXIT GATE — idempotency convergence.
// ---------------------------------------------------------------------------

func TestSubmitIdempotencyConvergence(t *testing.T) {
	ctx := context.Background()

	// G1 — replay after the run exists: same key, second submit short-circuits the
	// probe (no second dispatch) and returns the SAME handle.
	t.Run("replay_short_circuits_dispatch", func(t *testing.T) {
		assertReplayShortCircuitsDispatch(ctx, t)
	})

	// G2 — two CONCURRENT submits with the SAME key. Even if both race past the
	// probe and both dispatch (creating two runs), both converge on the lowest-id
	// canonical handle and exactly ONE run survives (the sibling is cancelled).
	t.Run("concurrent_submits_converge", func(t *testing.T) {
		// Run many times to exercise the race interleavings.
		for iter := range 200 {
			assertConcurrentSubmitsConverge(ctx, t, iter)
		}
	})

	// G3 — replay AFTER completion: the run is terminal; a re-submit still finds it
	// and returns the same handle (no new dispatch).
	t.Run("replay_after_completion", func(t *testing.T) {
		assertReplayAfterCompletion(ctx, t)
	})
}

// assertReplayShortCircuitsDispatch verifies G1: a second submit with the same key
// short-circuits the probe (no second dispatch) and returns the SAME handle.
func assertReplayShortCircuitsDispatch(ctx context.Context, t *testing.T) {
	t.Helper()
	f := newFakeActions()
	a := newAccessForTest(t, f)
	h1, err := a.SubmitAgenticJob(subRC(ctx, "same-key"), goodSpec())
	if err != nil {
		t.Fatalf("submit1: %v", err)
	}
	h2, err := a.SubmitAgenticJob(subRC(ctx, "same-key"), goodSpec())
	if err != nil {
		t.Fatalf("submit2: %v", err)
	}
	if !PipelineHandleEqual(h1, h2) {
		t.Fatalf("handles diverged: %s vs %s", h1, h2)
	}
	if f.dispatchCount != 1 {
		t.Fatalf("dispatchCount = %d, want 1 (replay must NOT re-dispatch)", f.dispatchCount)
	}
	if len(f.runs) != 1 {
		t.Fatalf("run count = %d, want 1", len(f.runs))
	}
}

// assertConcurrentSubmitsConverge verifies G2 for a single race iteration: two
// concurrent submits with the same key converge on the lowest-id canonical handle
// and exactly ONE run survives (any sibling is cancelled).
func assertConcurrentSubmitsConverge(ctx context.Context, t *testing.T, iter int) {
	t.Helper()
	f := newFakeActions()
	a := newAccessForTest(t, f)

	var wg sync.WaitGroup
	handles := make([]PipelineHandle, 2)
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			handles[idx], errs[idx] = a.SubmitAgenticJob(subRC(ctx, "race-key"), goodSpec())
		}(i)
	}
	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Fatalf("iter %d submit %d: %v", iter, i, e)
		}
	}
	if !PipelineHandleEqual(handles[0], handles[1]) {
		t.Fatalf("iter %d: handles diverged: %s vs %s", iter, handles[0], handles[1])
	}
	// The canonical handle is the lowest-id run.
	canonical := handles[0]
	// Exactly one run carrying the dedup name is NOT cancelled (the canonical),
	// and it is the lowest id. Any sibling (if a double-dispatch happened) is
	// cancelled.
	f.mu.Lock()
	var liveCanonical, liveCount int
	lowest := int64(1 << 62)
	for _, r := range f.runs {
		if r.id < lowest {
			lowest = r.id
		}
	}
	for _, r := range f.runs {
		if r.conclusion != "cancelled" {
			liveCount++
			if "run/"+itoaTest(r.id) == PipelineHandleString(canonical) {
				liveCanonical++
			}
		}
	}
	f.mu.Unlock()
	if liveCount != 1 {
		t.Fatalf("iter %d: live (non-cancelled) run count = %d, want exactly 1", iter, liveCount)
	}
	if liveCanonical != 1 {
		t.Fatalf("iter %d: the surviving run is not the canonical handle", iter)
	}
	if PipelineHandleString(canonical) != "run/"+itoaTest(lowest) {
		t.Fatalf("iter %d: canonical %s is not the lowest-id run run/%d", iter, canonical, lowest)
	}
}

// assertReplayAfterCompletion verifies G3: a re-submit after the run is terminal
// still finds it and returns the same handle (no new dispatch).
func assertReplayAfterCompletion(ctx context.Context, t *testing.T) {
	t.Helper()
	f := newFakeActions()
	a := newAccessForTest(t, f)
	h1, err := a.SubmitAgenticJob(subRC(ctx, "done-key"), goodSpec())
	if err != nil {
		t.Fatalf("submit1: %v", err)
	}
	f.runs[0].status = "completed"
	f.runs[0].conclusion = "success"
	h2, err := a.SubmitAgenticJob(subRC(ctx, "done-key"), goodSpec())
	if err != nil {
		t.Fatalf("submit2: %v", err)
	}
	if !PipelineHandleEqual(h1, h2) {
		t.Fatalf("post-completion handles diverged: %s vs %s", h1, h2)
	}
	if f.dispatchCount != 1 {
		t.Fatalf("dispatchCount = %d, want 1", f.dispatchCount)
	}
}

func TestDedupTokenDeterminism(t *testing.T) {
	first, second := dedupToken("a"), dedupToken("a")
	if first != second {
		t.Fatal("dedupToken not deterministic")
	}
	if dedupToken("a") == dedupToken("b") {
		t.Fatal("dedupToken collision on distinct keys")
	}
}

// ---------------------------------------------------------------------------
// test fixtures: a real throwaway shared git repo + a `claude` PATH shim.
// ---------------------------------------------------------------------------

// newSharedRepo creates the real throwaway SHARED repo the local executor operates
// `git worktree` on, seeded with one empty commit on "main". Mirrors
// systemtests/internal/harness/localgit.go's StartLocalGitRepo but reimplemented
// here (this test lives in the server module; the harness module is a sibling
// the RA layer must not import).
//
// NON-BARE, deliberately (SP1 capture seam, Task 5): the per-episode trace sink
// is <repoPath>/.aiarch/traces, and THE TRUST RULE
// (assertTraceSinkOutsideGitDir) refuses to dispatch when that falls inside the
// agent-writable gitDir — which is exactly what a BARE shared repo produces
// (gitDir == repoPath). A non-bare checkout puts gitDir at <repoPath>/.git, so
// the sink sits outside it. The genuinely-bare configuration keeps its own
// fixture (newBareOnlyRepo) and its own refusal proof (TR4).
//
// HEAD is left DETACHED so no branch is checked out in the shared repo: the seed
// helpers below clone it and push main / activity branches back, and git refuses
// to update a branch that some working tree has checked out.
func newSharedRepo(t *testing.T) (repoDir, url string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH; skipping local-executor proof")
	}
	root := t.TempDir()
	shared := filepath.Join(root, "shared")
	testGit(t, "", "init", "--initial-branch=main", shared)
	testGit(t, shared, "config", "user.email", "seed@aiarch.local")
	testGit(t, shared, "config", "user.name", "seed")
	testGit(t, shared, "commit", "--allow-empty", "-m", "seed")
	testGit(t, shared, "checkout", "--detach")

	return shared, "file://" + shared
}

// remoteBranchExists reports whether branch exists on the shared repo.
func remoteBranchExists(t *testing.T, sharedDir, branch string) bool {
	t.Helper()
	cmd := exec.Command("git", "-C", sharedDir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return cmd.Run() == nil
}

// remoteCommitCount returns the commit count on branch in the shared repo.
func remoteCommitCount(t *testing.T, sharedDir, branch string) int {
	t.Helper()
	out := testGitOut(t, sharedDir, "rev-list", "--count", branch)
	var n int
	if _, err := fscanInt(strings.TrimSpace(out), &n); err != nil {
		t.Fatalf("parse commit count %q: %v", out, err)
	}
	return n
}

func fscanInt(s string, n *int) (int, error) {
	v := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errors.New("not a number: " + s)
		}
		v = v*10 + int(r-'0')
	}
	*n = v
	return 1, nil
}

func testGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v (dir=%s): %v\n%s", args, dir, err, out)
	}
}

func testGitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", full...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// fakeStateMCPBin returns a path to SOME existing, executable file to stand in
// for cmd/aiarch-state-mcp — the local-executor only checks it EXISTS (New's
// eager os.Stat) and threads its path verbatim into the --mcp-config JSON; it
// never executes it itself (claude would, in a real run). Using the test
// binary's own `git`-shim-adjacent trick (a tiny script) keeps this hermetic.
func fakeStateMCPBin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "aiarch-state-mcp")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil { //nolint:gosec // test fixture, deliberately executable
		t.Fatalf("write fake state-mcp bin: %v", err)
	}
	return path
}

// installClaudeShim writes an executable `claude` script into a fresh temp dir
// and prepends it to PATH for the test's duration — the local analog of
// claudecli_test.go's installClaudeShim (same rationale: exec.CommandContext
// resolves to the shim, never a real installation).
func installClaudeShim(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shim is a POSIX shell script; localexec is not exercised on windows in CI")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil { //nolint:gosec // test shim, deliberately executable
		t.Fatalf("write claude shim: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// commitShim is a claude shim that, when run, makes ONE commit in its cwd (a
// marker file named by callIndex so repeated invocations are distinguishable),
// captures its argv + the --mcp-config file's content + PWD into captureDir, then
// exits 0. capturePath must be OUTSIDE any git working tree the shim itself
// operates in.
func commitShim(t *testing.T, captureDir string) string {
	t.Helper()
	if err := os.MkdirAll(captureDir, 0o755); err != nil {
		t.Fatalf("mkdir capture dir: %v", err)
	}
	// $CAPTURE is baked in at shim-generation time (not read from the parent env)
	// so the capture location is independent of whatever env the code under test
	// passes to the subprocess (claudeSubprocessEnv strips ANTHROPIC_API_KEY but
	// otherwise passes the parent env through — this keeps the test robust to
	// that regardless).
	script := "#!/bin/sh\n" +
		"set -e\n" +
		"CAPTURE='" + captureDir + "'\n" +
		"n=0\n" +
		"while [ -f \"$CAPTURE/call-$n.args\" ]; do n=$((n+1)); done\n" +
		"printf '%s\\n' \"$@\" > \"$CAPTURE/call-$n.args\"\n" +
		"pwd > \"$CAPTURE/call-$n.pwd\"\n" +
		// the --mcp-config / --settings values are the args immediately after
		// their literal flag; find and copy each (the settings capture proves
		// the Tier-2 sandbox --settings file THE INVARIANT requires).
		"prev=\"\"\n" +
		"for a in \"$@\"; do\n" +
		"  if [ \"$prev\" = \"--mcp-config\" ]; then cp \"$a\" \"$CAPTURE/call-$n.mcpconfig.json\"; fi\n" +
		"  if [ \"$prev\" = \"--settings\" ]; then cp \"$a\" \"$CAPTURE/call-$n.settings.json\"; fi\n" +
		"  prev=\"$a\"\n" +
		"done\n" +
		"env > \"$CAPTURE/call-$n.env\"\n" +
		"git config user.email shim@aiarch.local\n" +
		"git config user.name shim\n" +
		"echo \"phase $n\" >> SHIM_PROGRESS.txt\n" +
		"git add -A\n" +
		"git commit -m \"shim commit $n\" >/dev/null\n" +
		"echo '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"result\":\"ok\"}'\n" +
		"exit 0\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil { //nolint:gosec // test shim
		t.Fatalf("write commit shim: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return path
}

// ---------------------------------------------------------------------------
// U1 — constructor
// ---------------------------------------------------------------------------

func TestNewLocalExec_RejectsEmptyRepoURL(t *testing.T) {
	_, err := NewLocalExecAgenticJobAccess("", "p", fakeStateMCPBin(t), 0)
	if kind(err) != fwra.ContractMisuse {
		t.Fatalf("kind = %v, want ContractMisuse", kind(err))
	}
}

func TestNewLocalExec_RejectsEmptyStateMCPBin(t *testing.T) {
	_, err := NewLocalExecAgenticJobAccess("file:///tmp/x", "p", "", 0)
	if kind(err) != fwra.ContractMisuse {
		t.Fatalf("kind = %v, want ContractMisuse", kind(err))
	}
}

func TestNewLocalExec_RejectsMissingStateMCPBin(t *testing.T) {
	_, err := NewLocalExecAgenticJobAccess("file:///tmp/x", "p", "/no/such/binary-xyz", 0)
	if kind(err) != fwra.ContractMisuse {
		t.Fatalf("kind = %v, want ContractMisuse", kind(err))
	}
}

func TestNewLocalExec_RejectsNonLocalRepoURL(t *testing.T) {
	// The worktree-per-activity executor operates git worktrees directly on the
	// shared repo's filesystem path — a network URL cannot host a worktree.
	_, err := NewLocalExecAgenticJobAccess("https://github.com/acme/repo.git", "p", fakeStateMCPBin(t), 0)
	if kind(err) != fwra.ContractMisuse {
		t.Fatalf("kind = %v, want ContractMisuse", kind(err))
	}
}

// LW2 — startup prune: stale worktree metadata left by a crashed prior process
// (registered in .git/worktrees but whose directory is gone) is pruned when the
// executor is constructed, so a later dispatch for the same activity branch is
// not blocked by a phantom "already checked out" registration.
func TestNewLocalExec_PrunesStaleWorktreesOnStartup(t *testing.T) {
	sharedDir, url := newSharedRepo(t)
	stale := filepath.Join(t.TempDir(), "stale-wt")
	testGit(t, sharedDir, "worktree", "add", "-b", "activity/C-STALE", stale, "main")
	if err := os.RemoveAll(stale); err != nil {
		t.Fatalf("remove stale worktree dir: %v", err)
	}
	newLocalExecForTest(t, url, 0)
	assertNoLingeringWorktrees(t, sharedDir)
}

// assertNoLingeringWorktrees asserts the shared repo carries NO linked-worktree
// registrations — only the primary entry (`git worktree list --porcelain` always
// lists the repo itself first; a bare repo lists as one "worktree" entry too).
func assertNoLingeringWorktrees(t *testing.T, repoDir string) {
	t.Helper()
	out := testGitOut(t, repoDir, "worktree", "list", "--porcelain")
	if n := strings.Count(out, "worktree "); n != 1 {
		t.Fatalf("expected only the primary worktree entry, got %d:\n%s", n, out)
	}
}

// waitForNoLingeringWorktrees polls assertNoLingeringWorktrees's condition until
// it holds or the deadline passes — for paths (cancel) where the terminal status
// is recorded before awaitCompletion's asynchronous worktree removal finishes.
func waitForNoLingeringWorktrees(t *testing.T, repoDir string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		out := testGitOut(t, repoDir, "worktree", "list", "--porcelain")
		if strings.Count(out, "worktree ") == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("worktree was not removed within %s:\n%s", timeout, out)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// ---------------------------------------------------------------------------
// U2-U7 — contract misuse / not-found
// ---------------------------------------------------------------------------

func newLocalExecForTest(t *testing.T, repoURL string, runTimeout time.Duration) *localExecAccess {
	t.Helper()
	v, err := NewLocalExecAgenticJobAccess(repoURL, "test-project", fakeStateMCPBin(t), runTimeout)
	if err != nil {
		t.Fatalf("NewLocalExecAgenticJobAccess: %v", err)
	}
	a, ok := v.(*localExecAccess)
	if !ok {
		t.Fatalf("expected *localExecAccess, got %T", v)
	}
	return a
}

func TestLocalExecSubmit_ContractMisuse(t *testing.T) {
	_, url := newSharedRepo(t)
	a := newLocalExecForTest(t, url, 0)
	ctx := context.Background()

	t.Run("empty idempotencyKey", func(t *testing.T) {
		_, err := a.SubmitAgenticJob(fwra.Context{Context: ctx}, goodSpec())
		if kind(err) != fwra.ContractMisuse {
			t.Fatalf("kind = %v, want ContractMisuse", kind(err))
		}
	})
	t.Run("no steps", func(t *testing.T) {
		spec := goodSpec()
		spec.Steps = nil
		_, err := a.SubmitAgenticJob(subRC(ctx, "k1"), spec)
		if kind(err) != fwra.ContractMisuse {
			t.Fatalf("kind = %v, want ContractMisuse", kind(err))
		}
	})
	t.Run("empty ActivityID", func(t *testing.T) {
		spec := goodSpec()
		spec.ActivityID = ""
		spec.DispatchInputs = map[string]string{"command": "service-construction", "component_id": "C-X"}
		_, err := a.SubmitAgenticJob(subRC(ctx, "k2"), spec)
		if kind(err) != fwra.ContractMisuse {
			t.Fatalf("kind = %v, want ContractMisuse", kind(err))
		}
	})
	t.Run("missing command dispatch input", func(t *testing.T) {
		spec := goodSpec()
		spec.DispatchInputs = map[string]string{"component_id": "C-X"}
		_, err := a.SubmitAgenticJob(subRC(ctx, "k3"), spec)
		if kind(err) != fwra.ContractMisuse {
			t.Fatalf("kind = %v, want ContractMisuse", kind(err))
		}
	})
}

func TestLocalExecObserveCancel_HandleMisuse(t *testing.T) {
	_, url := newSharedRepo(t)
	a := newLocalExecForTest(t, url, 0)
	ctx := obsRC(context.Background())

	for _, h := range []PipelineHandle{"", "run/42", "local:"} {
		if _, err := a.ObserveAgenticJob(ctx, h); kind(err) != fwra.ContractMisuse {
			t.Fatalf("Observe(%q) kind = %v, want ContractMisuse", h, kind(err))
		}
		if err := a.CancelAgenticJob(ctx, h); kind(err) != fwra.ContractMisuse {
			t.Fatalf("Cancel(%q) kind = %v, want ContractMisuse", h, kind(err))
		}
	}
}

// L7 / FR1a — a well-formed handle with NO in-memory run record is a RESTART-LOST run
// (the local executor keeps records only in memory; a server restart drops the map while
// the workflow still polls). It must terminate the observe loop, so Observe returns a
// TERMINAL PhaseFailed observation with the recovery diagnostic — NOT fwra.NotFound and
// NOT a still-running phase — routing the Manager to the StageDraftFailed human gate
// instead of looping to the maxObservePolls ceiling.
func TestLocalExecObserve_RestartLostHandle_TerminalFailed(t *testing.T) {
	_, url := newSharedRepo(t)
	a := newLocalExecForTest(t, url, 0)
	obs, err := a.ObserveAgenticJob(obsRC(context.Background()), "local:deadbeef")
	if err != nil {
		t.Fatalf("Observe(lost handle) returned an error %v, want a terminal-failed observation with nil error", err)
	}
	if obs.Phase != PhaseFailed {
		t.Fatalf("Phase = %v, want PhaseFailed (a restart-lost run must terminate, not loop)", obs.Phase)
	}
	if !PipelinePhaseIsTerminal(obs.Phase) {
		t.Fatalf("Phase %v is not terminal — the observe loop would never break", obs.Phase)
	}
	if obs.Diagnostic == "" {
		t.Fatal("terminal-failed observation for a lost run must carry a recovery diagnostic")
	}
	if !strings.Contains(obs.Diagnostic, "restarted") {
		t.Fatalf("diagnostic %q should explain the run was lost to a restart", obs.Diagnostic)
	}
}

// FR1b — a KNOWN, still-running handle must STILL report running (the terminal-lost path
// must not swallow a legitimately in-flight run). A never-terminating shim keeps the run
// in localRunRunning while we observe it.
func TestLocalExecObserve_KnownRunning_StillRunning(t *testing.T) {
	_, url := newSharedRepo(t)
	installClaudeShim(t, "#!/bin/sh\nexec sleep 30\n")
	a := newLocalExecForTest(t, url, 20*time.Second)

	spec := localSpec("C-RUNNING", "someComponent", "service-construction")
	handle, err := a.SubmitAgenticJob(subRC(context.Background(), "running-key"), spec)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	// The subprocess is running (the shim sleeps); Observe must report Running, never the
	// restart-lost terminal-failed path (the record IS in a.runs).
	obs, err := a.ObserveAgenticJob(obsRC(context.Background()), handle)
	if err != nil {
		t.Fatalf("Observe(running): %v", err)
	}
	if obs.Phase != PhaseRunning {
		t.Fatalf("Phase = %v, want PhaseRunning for a known in-flight run", obs.Phase)
	}
	// The episode summary is a TERMINAL fact — mined from the closed trace after
	// cmd.Wait. A mid-flight observation must never carry a half-read one.
	if obs.Episode != nil {
		t.Fatalf("Episode = %+v on a still-running observation, want nil", obs.Episode)
	}
	// Clean up: cancel the sleeping subprocess so the test does not leak it.
	if err := a.CancelAgenticJob(obsRC(context.Background()), handle); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	waitForTerminal(t, a, handle, 5*time.Second)
}

func TestLocalExecCancel_UnknownHandle_NoopSuccess(t *testing.T) {
	_, url := newSharedRepo(t)
	a := newLocalExecForTest(t, url, 0)
	if err := a.CancelAgenticJob(obsRC(context.Background()), "local:deadbeef"); err != nil {
		t.Fatalf("Cancel(unknown): unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// H1-H3 — happy path dispatch shape + branch continuity
// ---------------------------------------------------------------------------

func localSpec(activityID, componentID, command string) PipelineSpec {
	return PipelineSpec{
		ActivityID: ConstructionActivityID(activityID),
		Steps:      []PipelineStep{{Name: "build", Toolchain: "go-1.23", Command: []string{"sh", "-c", "true"}}},
		DispatchInputs: map[string]string{
			"activity_id":  activityID,
			"component_id": componentID,
			"command":      command,
			"phase":        "construction",
		},
	}
}

func waitForTerminal(t *testing.T, a *localExecAccess, handle PipelineHandle, timeout time.Duration) PipelineObservation {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		obs, err := a.ObserveAgenticJob(obsRC(context.Background()), handle)
		if err != nil {
			t.Fatalf("Observe: %v", err)
		}
		if PipelinePhaseIsTerminal(obs.Phase) {
			return obs
		}
		if time.Now().After(deadline) {
			t.Fatalf("pipeline did not reach a terminal phase within %s (last phase=%v)", timeout, obs.Phase)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestLocalExecSubmit_HappyPath_SpawnsClaudeWithCorrectShapeAndPushesCommit(t *testing.T) {
	sharedDir, url := newSharedRepo(t)
	capture := filepath.Join(t.TempDir(), "capture")
	commitShim(t, capture)
	// Deterministic env-allowlist proof: TERM/USER/LOGNAME are forced present (so the
	// exact-count assertion below is stable everywhere, including CI where they may be
	// unset). USER/LOGNAME are the OS-username pair claude's headless subscription-auth
	// keychain lookup needs (forwarded, low-sensitivity, not secrets). A sentinel
	// parent-only var AND a sentinel ANTHROPIC_API_KEY both prove the allowlist is a
	// CONSTRUCTED list, not a filtered passthrough — neither leaks into the child.
	t.Setenv("TERM", "xterm-test")
	t.Setenv("USER", "aiarch-tester")
	t.Setenv("LOGNAME", "aiarch-tester")
	t.Setenv("ARCHISTRATOR_TEST_PARENT_ONLY_SECRET", "must-not-leak-into-child")
	t.Setenv("ANTHROPIC_API_KEY", "sk-must-not-leak-into-child")
	a := newLocalExecForTest(t, url, 10*time.Second)

	spec := localSpec("C-BILLENG", "billingGatewayAccess", "service-construction")
	handle, err := a.SubmitAgenticJob(subRC(context.Background(), "activity-key-1"), spec)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if handle == "" {
		t.Fatal("Submit returned a zero handle")
	}

	obs := waitForTerminal(t, a, handle, 10*time.Second)
	if obs.Phase != PhaseSucceeded {
		t.Fatalf("Phase = %v, want PhaseSucceeded (diagnostic: %q)", obs.Phase, obs.Diagnostic)
	}

	// The branch exists in the SHARED repo with the shim's commit — the worktree
	// commit advanced the ref directly, no push involved (seed + shim commit = 2).
	branch := "activity/C-BILLENG"
	if !remoteBranchExists(t, sharedDir, branch) {
		t.Fatalf("branch %s does not exist in the shared repo", branch)
	}
	if got := remoteCommitCount(t, sharedDir, branch); got != 2 {
		t.Fatalf("commit count on %s = %d, want 2 (seed + shim commit)", branch, got)
	}

	// LH2 — the worktree was removed after completion: no lingering registration.
	assertNoLingeringWorktrees(t, sharedDir)

	// Dispatch shape: exactly one invocation captured.
	args := readCapturedArgs(t, capture, 0)
	assertClaudeArgsShape(t, args, "-p\n/service-construction billingGatewayAccess C-BILLENG")

	// cwd was a throwaway worktree (a temp dir, distinct from the shared repo path).
	pwd := readCapturedPWD(t, capture, 0)
	if pwd == "" || pwd == sharedDir {
		t.Fatalf("claude cwd = %q, want a throwaway worktree directory distinct from the shared repo", pwd)
	}

	// --mcp-config envelope: exact AIARCH_* shape.
	assertMCPConfigEnvelope(t, capture, 0, pwd, map[string]string{
		"AIARCH_PROJECT_ID":    "test-project",
		"AIARCH_JOB_MODE":      "construct",
		"AIARCH_COMPONENT_ID":  "billingGatewayAccess",
		"AIARCH_ACTIVITY_ID":   "C-BILLENG",
		"AIARCH_TARGET_BRANCH": branch,
	})

	// Tier-2 sandbox --settings envelope: THE INVARIANT (Fix-subagent Task 6),
	// plus the worktree-mode filesystem scope: allowWrite covers the worktree
	// dir AND the shared repo's git dir (worktree commits write .git/worktrees
	// metadata + shared objects/refs — the founder-accepted isolation tradeoff).
	sandboxCfg := assertSandboxSettingsEnvelope(t, capture, 0)
	assertSandboxFilesystemAllowWrite(t, sandboxCfg, pwd, filepath.Join(sharedDir, ".git"))

	// Env allowlist (Fix-subagent Task 6 + USER/LOGNAME for claude subscription auth):
	// EXACTLY PATH/HOME/TERM/USER/LOGNAME + the six AIARCH_* rig vars cross into the
	// child — no other parent var leaks. shellInjectedVars are NOT part of cmd.Env at
	// all — /bin/sh itself sets PWD/SHLVL/_ on every invocation regardless of the
	// incoming env (a shim-script artifact of capturing via `env` inside a spawned sh),
	// so they are excluded from the exact-membership check below rather than asserted on.
	env := readCapturedEnv(t, capture, 0)
	shellInjectedVars := map[string]bool{"PWD": true, "SHLVL": true, "_": true}
	wantKeys := []string{
		"PATH", "HOME", "TERM", "USER", "LOGNAME",
		"AIARCH_PROJECT_ID", "AIARCH_JOB_MODE", "AIARCH_COMPONENT_ID",
		"AIARCH_ACTIVITY_ID", "AIARCH_TARGET_BRANCH", "AIARCH_STATE_ROOT",
	}
	for _, k := range wantKeys {
		if _, ok := env[k]; !ok {
			t.Errorf("child env missing allowlisted var %s", k)
		}
	}
	// USER is the ONE var that makes claude's headless subscription auth work (the
	// keychain credential lookup is USER-scoped) — assert its value crossed intact.
	if env["USER"] != "aiarch-tester" {
		t.Errorf("child env USER = %q, want %q (the OS username claude's subscription-auth keychain lookup needs)", env["USER"], "aiarch-tester")
	}
	if _, leaked := env["ARCHISTRATOR_TEST_PARENT_ONLY_SECRET"]; leaked {
		t.Fatal("child env leaked a parent-only var (ARCHISTRATOR_TEST_PARENT_ONLY_SECRET) — env allowlist regressed to full passthrough")
	}
	// Posture unchanged: ANTHROPIC_API_KEY is NEVER forwarded (the local executor rides
	// the operator's own claude login, never a key). Forwarding USER did not open the door.
	if _, leaked := env["ANTHROPIC_API_KEY"]; leaked {
		t.Fatal("child env leaked ANTHROPIC_API_KEY — the local executor must ride the operator's claude login, never a forwarded key")
	}
	got := 0
	for k := range env {
		if !shellInjectedVars[k] {
			got++
		}
	}
	if got != len(wantKeys) {
		t.Fatalf("child env (excluding shell-injected PWD/SHLVL/_) has %d vars, want exactly the %d-entry allowlist %v; got keys %v", got, len(wantKeys), wantKeys, envKeys(env))
	}
}

// envKeys returns m's keys, for a readable test-failure message.
func envKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// readCapturedArgs reads the commitShim's captured argv for invocation n.
func readCapturedArgs(t *testing.T, captureDir string, n int) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(captureDir, fmt.Sprintf("call-%d.args", n)))
	if err != nil {
		t.Fatalf("read captured args: %v", err)
	}
	return strings.TrimRight(string(raw), "\n")
}

// readCapturedPWD reads the commitShim's captured cwd for invocation n.
func readCapturedPWD(t *testing.T, captureDir string, n int) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(captureDir, fmt.Sprintf("call-%d.pwd", n)))
	if err != nil {
		t.Fatalf("read captured pwd: %v", err)
	}
	return strings.TrimSpace(string(raw))
}

// assertClaudeArgsShape asserts the fixed claude invocation flags plus the given
// -p prompt substring are all present. The shim prints one arg per line
// (`printf '%s\n' "$@"`), so a multi-token flag+value pair appears as two
// adjacent lines — callers pass promptLine already newline-joined.
//
// --settings and --strict-mcp-config are the Fix-subagent Task 6 hardening
// additions (sandboxed-by-default): --settings pairs UNCONDITIONALLY with
// --dangerously-skip-permissions per THE INVARIANT (claudeArgv's doc
// comment); --strict-mcp-config ensures only mcpConfigPath's ONE server
// attaches, never ambient user/project MCP config.
func assertClaudeArgsShape(t *testing.T, args, promptLine string) {
	t.Helper()
	for _, want := range []string{
		"--dangerously-skip-permissions",
		"--settings",
		"--mcp-config",
		"--strict-mcp-config",
		// SP1 capture seam (Task 5): the EVENT STREAM, not the single-object
		// result. --verbose is required alongside it in headless (-p) mode.
		"--output-format\nstream-json",
		"--verbose",
		promptLine,
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("captured claude args %q missing %q", args, want)
		}
	}
}

// assertMCPConfigEnvelope reads invocation n's captured --mcp-config file and
// asserts the aiarch-state server carries wantEnv exactly, plus
// AIARCH_STATE_ROOT matching claudePWD (compared by basename — see the caller's
// note on why not a direct string/symlink comparison).
func assertMCPConfigEnvelope(t *testing.T, captureDir string, n int, claudePWD string, wantEnv map[string]string) {
	t.Helper()
	mcpRaw, err := os.ReadFile(filepath.Join(captureDir, fmt.Sprintf("call-%d.mcpconfig.json", n)))
	if err != nil {
		t.Fatalf("read captured mcp config: %v", err)
	}
	var cfg mcpConfigFileJSON
	if err := json.Unmarshal(mcpRaw, &cfg); err != nil {
		t.Fatalf("decode captured mcp config: %v\n%s", err, mcpRaw)
	}
	srv, ok := cfg.MCPServers["aiarch-state"]
	if !ok {
		t.Fatalf("mcp config missing aiarch-state server: %s", mcpRaw)
	}
	if srv.Command == "" {
		t.Fatal("mcp config aiarch-state command is empty")
	}
	for k, want := range wantEnv {
		if got := srv.Env[k]; got != want {
			t.Fatalf("mcp config env[%s] = %q, want %q", k, got, want)
		}
	}
	// The clone dir is removed by awaitCompletion once claude exits, so by the
	// time this assertion runs it may no longer exist on disk — compare basenames
	// (os.MkdirTemp's random suffix makes this collision-safe) rather than
	// filepath.EvalSymlinks, which also sidesteps macOS's /var vs /private/var
	// symlink spelling difference between the shim's `pwd` (a real getcwd(2) call)
	// and the Go-side path string.
	if got, want := filepath.Base(srv.Env["AIARCH_STATE_ROOT"]), filepath.Base(claudePWD); got != want {
		t.Fatalf("AIARCH_STATE_ROOT basename = %q, want %q (claude's actual cwd)", got, want)
	}
}

// readCapturedSandboxSettings reads + decodes invocation n's captured
// --settings file (Fix-subagent Task 6: THE INVARIANT proof).
func readCapturedSandboxSettings(t *testing.T, captureDir string, n int) sandboxSettingsJSON {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(captureDir, fmt.Sprintf("call-%d.settings.json", n)))
	if err != nil {
		t.Fatalf("read captured sandbox settings: %v", err)
	}
	var cfg sandboxSettingsJSON
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("decode captured sandbox settings: %v\n%s", err, raw)
	}
	return cfg
}

// assertSandboxSettingsEnvelope asserts invocation n's captured --settings
// file carries the FIXED Tier-2 sandbox posture THE INVARIANT requires:
// enabled + failIfUnavailable, and allowUnsandboxedCommands=false (so
// claude's OWN dangerouslyDisableSandbox retry escape hatch is disabled —
// belt-and-braces with the outer --dangerously-skip-permissions bypass).
func assertSandboxSettingsEnvelope(t *testing.T, captureDir string, n int) sandboxConfigJSON {
	t.Helper()
	cfg := readCapturedSandboxSettings(t, captureDir, n)
	s := cfg.Sandbox
	if !s.Enabled {
		t.Fatal("sandbox settings: enabled = false, want true (THE INVARIANT requires an ACTIVE sandbox)")
	}
	if !s.FailIfUnavailable {
		t.Fatal("sandbox settings: failIfUnavailable = false, want true (must fail closed, never silently degrade)")
	}
	if s.AllowUnsandboxedCommands {
		t.Fatal("sandbox settings: allowUnsandboxedCommands = true, want false (disables claude's own unsandboxed-retry escape hatch)")
	}
	return s
}

// assertSandboxFilesystemAllowWrite asserts the captured sandbox settings carry
// a filesystem.allowWrite covering (a) the worktree dir claude ran in and (b)
// the shared repo's git dir. Paths are compared by basename/suffix rather than
// verbatim to sidestep macOS's /var vs /private/var symlink spelling difference
// between the shim's `pwd` (a real getcwd(2)) and the Go-side path strings.
func assertSandboxFilesystemAllowWrite(t *testing.T, s sandboxConfigJSON, worktreePWD, repoGitDir string) {
	t.Helper()
	if s.Filesystem == nil || len(s.Filesystem.AllowWrite) == 0 {
		t.Fatalf("sandbox settings: missing filesystem.allowWrite (worktree mode needs the worktree dir + the shared repo's git dir writable): %+v", s)
	}
	// Compared by the last TWO path elements ("<repoDirName>/.git"): macOS spells
	// the same temp dir /var/… and /private/var/…, so a full-string comparison
	// would fail on spelling alone, while a bare ".git" basename would match any
	// git dir at all.
	wantGitDirSuffix := filepath.Join(filepath.Base(filepath.Dir(repoGitDir)), filepath.Base(repoGitDir))
	var haveWorktree, haveGitDir bool
	for _, p := range s.Filesystem.AllowWrite {
		if filepath.Base(p) == filepath.Base(worktreePWD) {
			haveWorktree = true
		}
		if strings.HasSuffix(p, wantGitDirSuffix) {
			haveGitDir = true
		}
	}
	if !haveWorktree {
		t.Fatalf("sandbox filesystem.allowWrite %v missing the worktree dir (basename %q)", s.Filesystem.AllowWrite, filepath.Base(worktreePWD))
	}
	if !haveGitDir {
		t.Fatalf("sandbox filesystem.allowWrite %v missing the shared repo's git dir (suffix %q)", s.Filesystem.AllowWrite, wantGitDirSuffix)
	}
}

// readCapturedEnv reads invocation n's captured `env` dump (commitShim) into
// a key→value map.
func readCapturedEnv(t *testing.T, captureDir string, n int) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(captureDir, fmt.Sprintf("call-%d.env", n)))
	if err != nil {
		t.Fatalf("read captured env: %v", err)
	}
	out := map[string]string{}
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue // a multi-line value continuation from a preceding var; irrelevant to this allowlist proof
		}
		out[k] = v
	}
	return out
}

func TestLocalExecSubmit_SecondPhase_ReattachesToExistingActivityBranch(t *testing.T) {
	sharedDir, url := newSharedRepo(t)
	capture := filepath.Join(t.TempDir(), "capture")
	commitShim(t, capture)
	a := newLocalExecForTest(t, url, 10*time.Second)

	spec1 := localSpec("C-BILLENG", "billingGatewayAccess", "service-requirements")
	h1, err := a.SubmitAgenticJob(subRC(context.Background(), "phase-key-1"), spec1)
	if err != nil {
		t.Fatalf("Submit (phase 1): %v", err)
	}
	if obs := waitForTerminal(t, a, h1, 10*time.Second); obs.Phase != PhaseSucceeded {
		t.Fatalf("phase 1 Phase = %v, diagnostic %q", obs.Phase, obs.Diagnostic)
	}

	spec2 := localSpec("C-BILLENG", "billingGatewayAccess", "service-construction")
	h2, err := a.SubmitAgenticJob(subRC(context.Background(), "phase-key-2"), spec2)
	if err != nil {
		t.Fatalf("Submit (phase 2): %v", err)
	}
	if obs := waitForTerminal(t, a, h2, 10*time.Second); obs.Phase != PhaseSucceeded {
		t.Fatalf("phase 2 Phase = %v, diagnostic %q", obs.Phase, obs.Diagnostic)
	}

	// BOTH phases' commits landed on the SAME branch (seed + phase1 + phase2 = 3):
	// nothing was reset/lost between the two dispatches.
	branch := "activity/C-BILLENG"
	if got := remoteCommitCount(t, sharedDir, branch); got != 3 {
		t.Fatalf("commit count on %s = %d, want 3 (seed + 2 phase commits)", branch, got)
	}
}

// ---------------------------------------------------------------------------
// G1 — idempotency convergence
// ---------------------------------------------------------------------------

func TestLocalExecSubmit_DuplicateKey_ConvergesWithoutRedispatch(t *testing.T) {
	sharedDir, url := newSharedRepo(t)
	capture := filepath.Join(t.TempDir(), "capture")
	commitShim(t, capture)
	a := newLocalExecForTest(t, url, 10*time.Second)

	spec := localSpec("C-BILLENG", "billingGatewayAccess", "service-construction")
	h1, err := a.SubmitAgenticJob(subRC(context.Background(), "same-key"), spec)
	if err != nil {
		t.Fatalf("Submit (1): %v", err)
	}
	h2, err := a.SubmitAgenticJob(subRC(context.Background(), "same-key"), spec)
	if err != nil {
		t.Fatalf("Submit (2): %v", err)
	}
	if h1 != h2 {
		t.Fatalf("handles diverged for the same idempotencyKey: %q != %q", h1, h2)
	}
	waitForTerminal(t, a, h1, 10*time.Second)

	// Exactly ONE claude invocation was captured — the duplicate submit did not
	// spawn a second subprocess.
	if _, err := os.Stat(filepath.Join(capture, "call-1.args")); !os.IsNotExist(err) {
		t.Fatalf("expected exactly one claude invocation, found a second (call-1.args exists, stat err=%v)", err)
	}
	branch := "activity/C-BILLENG"
	if got := remoteCommitCount(t, sharedDir, branch); got != 2 {
		t.Fatalf("commit count on %s = %d, want 2 (seed + ONE shim commit)", branch, got)
	}
}

// ---------------------------------------------------------------------------
// F1-F2 — exit-code / timeout mapping
// ---------------------------------------------------------------------------

func TestLocalExecObserve_NonZeroExit_Failed(t *testing.T) {
	_, url := newSharedRepo(t)
	installClaudeShim(t, "#!/bin/sh\necho 'boom: contract violation' >&2\nexit 3\n")
	a := newLocalExecForTest(t, url, 10*time.Second)

	spec := localSpec("C-FAIL", "someComponent", "service-construction")
	handle, err := a.SubmitAgenticJob(subRC(context.Background(), "fail-key"), spec)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	obs := waitForTerminal(t, a, handle, 10*time.Second)
	if obs.Phase != PhaseFailed {
		t.Fatalf("Phase = %v, want PhaseFailed", obs.Phase)
	}
	if !strings.Contains(obs.Diagnostic, "exited 3") {
		t.Fatalf("Diagnostic = %q, want it to name the exit code (3)", obs.Diagnostic)
	}
	if !strings.Contains(obs.Diagnostic, "boom: contract violation") {
		t.Fatalf("Diagnostic = %q, want it to include the stderr tail", obs.Diagnostic)
	}
}

func TestLocalExecObserve_Timeout_FailedWithTimeoutDiagnostic(t *testing.T) {
	_, url := newSharedRepo(t)
	// A well-behaved subprocess that honors SIGTERM promptly (mirrors the Cancel
	// test's shim) — this proves the ctx-deadline → Cancel(SIGTERM) → "timed out"
	// classification path, not the separate WaitDelay-forced-kill edge case a
	// TERM-ignoring process would exercise.
	installClaudeShim(t, "#!/bin/sh\nexec sleep 30\n")
	a := newLocalExecForTest(t, url, 200*time.Millisecond)

	spec := localSpec("C-SLOW", "someComponent", "service-construction")
	start := time.Now()
	handle, err := a.SubmitAgenticJob(subRC(context.Background(), "slow-key"), spec)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	obs := waitForTerminal(t, a, handle, 6*time.Second)
	if obs.Phase != PhaseFailed {
		t.Fatalf("Phase = %v, want PhaseFailed", obs.Phase)
	}
	if !strings.Contains(obs.Diagnostic, "timed out") {
		t.Fatalf("Diagnostic = %q, want a timeout message", obs.Diagnostic)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("did not bound to the run timeout promptly: took %s", elapsed)
	}
}

// LF3 — a CLEAN claude exit that made no commit did NOT advance the activity
// branch ref: never a fake success (the durable post-condition — work landed on
// the activity branch — does not hold), and the worktree is still cleaned up.
func TestLocalExecObserve_CleanExitNoCommit_Failed(t *testing.T) {
	sharedDir, url := newSharedRepo(t)
	installClaudeShim(t, "#!/bin/sh\nexit 0\n")
	a := newLocalExecForTest(t, url, 10*time.Second)

	spec := localSpec("C-NOCOMMIT", "someComponent", "service-construction")
	handle, err := a.SubmitAgenticJob(subRC(context.Background(), "nocommit-key"), spec)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	obs := waitForTerminal(t, a, handle, 10*time.Second)
	if obs.Phase != PhaseFailed {
		t.Fatalf("Phase = %v, want PhaseFailed (clean exit but no ref advance is NOT a success)", obs.Phase)
	}
	if !strings.Contains(obs.Diagnostic, "no commits") {
		t.Fatalf("Diagnostic = %q, want it to say the run produced no commits", obs.Diagnostic)
	}
	assertNoLingeringWorktrees(t, sharedDir)
}

// ---------------------------------------------------------------------------
// C1-C2 — cancel
// ---------------------------------------------------------------------------

func TestLocalExecCancel_Running_ConvergesToCancelledNeverFailed(t *testing.T) {
	sharedDir, url := newSharedRepo(t)
	installClaudeShim(t, "#!/bin/sh\nexec sleep 30\n")
	a := newLocalExecForTest(t, url, 20*time.Second)

	spec := localSpec("C-CANCEL", "someComponent", "service-construction")
	handle, err := a.SubmitAgenticJob(subRC(context.Background(), "cancel-key"), spec)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// Give the subprocess a moment to actually start before cancelling.
	time.Sleep(200 * time.Millisecond)
	if err := a.CancelAgenticJob(obsRC(context.Background()), handle); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	obs := waitForTerminal(t, a, handle, 5*time.Second)
	if obs.Phase != PhaseCancelled {
		t.Fatalf("Phase = %v, want PhaseCancelled (diagnostic: %q)", obs.Phase, obs.Diagnostic)
	}
	// LW1 — the worktree is removed on the cancel path too. Cancel records the
	// terminal status IMMEDIATELY while awaitCompletion (which does the removal
	// after cmd.Wait returns) is still unwinding the SIGTERM'd subprocess, so
	// poll briefly rather than asserting instantaneously.
	waitForNoLingeringWorktrees(t, sharedDir, 10*time.Second)
}

func TestLocalExecCancel_AlreadyTerminal_NoopSuccess(t *testing.T) {
	_, url := newSharedRepo(t)
	// commitShim (not a bare exit-0 shim): a clean exit must ADVANCE the activity
	// branch ref to count as PhaseSucceeded under the worktree rework (LF3).
	commitShim(t, filepath.Join(t.TempDir(), "capture"))
	a := newLocalExecForTest(t, url, 10*time.Second)

	spec := localSpec("C-DONE", "someComponent", "service-construction")
	handle, err := a.SubmitAgenticJob(subRC(context.Background(), "done-key"), spec)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitForTerminal(t, a, handle, 10*time.Second)

	if err := a.CancelAgenticJob(obsRC(context.Background()), handle); err != nil {
		t.Fatalf("Cancel(already-terminal): unexpected error: %v", err)
	}
	// still succeeded, not overwritten by the no-op cancel.
	obs, err := a.ObserveAgenticJob(obsRC(context.Background()), handle)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if obs.Phase != PhaseSucceeded {
		t.Fatalf("Phase after no-op cancel = %v, want it to remain PhaseSucceeded", obs.Phase)
	}
}

// ---------------------------------------------------------------------------
// U8-U9 — value semantics
// ---------------------------------------------------------------------------

func TestLocalTokenFromHandle(t *testing.T) {
	tok, ok := localTokenFromHandle("local:abc123")
	if !ok || tok != "abc123" {
		t.Fatalf("localTokenFromHandle(local:abc123) = (%q,%t), want (abc123,true)", tok, ok)
	}
	for _, h := range []PipelineHandle{"", "run/42", "local:"} {
		if _, ok := localTokenFromHandle(h); ok {
			t.Fatalf("localTokenFromHandle(%q) = ok, want rejected", h)
		}
	}
}

func TestClassifyLocalExecFailure_ExitCodeAndStderrTail(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 7")
	runErr := cmd.Run()
	got := classifyLocalExecFailure(runErr, "some failure detail")
	if !strings.Contains(got, "exited 7") {
		t.Fatalf("classifyLocalExecFailure = %q, want it to name exit code 7", got)
	}
	if !strings.Contains(got, "some failure detail") {
		t.Fatalf("classifyLocalExecFailure = %q, want it to include the stderr detail", got)
	}
}

func TestOutputTail_Bounds(t *testing.T) {
	long := strings.Repeat("x", 1000)
	got := outputTail(long, 10)
	if gotRunes := len([]rune(got)); gotRunes > 11 { // 10 + the "…" marker
		t.Fatalf("outputTail did not bound length: got %d runes (%q)", gotRunes, got)
	}
	if outputTail("short", 10) != "short" {
		t.Fatalf("outputTail should pass short text through unchanged")
	}
	// Rune safety: cutting mid-character must not emit a replacement glyph into
	// a string that reaches the UI panel.
	if got := outputTail(strings.Repeat("é", 100), 11); !utf8.ValidString(got) {
		t.Fatalf("outputTail split a multi-byte rune: %q", got)
	}
}

func TestOutputHead_Bounds(t *testing.T) {
	got := outputHead(strings.Repeat("x", 1000), 10)
	if gotRunes := len([]rune(got)); gotRunes > 11 { // 10 + the "…" marker
		t.Fatalf("outputHead did not bound length: got %d runes (%q)", gotRunes, got)
	}
	if !strings.HasPrefix(got, "xxxxxxxxxx") {
		t.Fatalf("outputHead = %q, want it to keep the LEADING text", got)
	}
	if outputHead("short", 10) != "short" {
		t.Fatalf("outputHead should pass short text through unchanged")
	}
	if got := outputHead(strings.Repeat("é", 100), 11); !utf8.ValidString(got) {
		t.Fatalf("outputHead split a multi-byte rune: %q", got)
	}
}

// ---------------------------------------------------------------------------
// LX-OBS — local-run observability: what claude actually said.
//
// The regression these pin: awaitCompletion used to DISCARD stdout, so every
// non-advancing local run surfaced as one bare sentence with no way to tell an
// MCP attach failure from an agent that decided there was nothing to do.
// ---------------------------------------------------------------------------

func TestClaudeOutputDetail_JSONErrorEnvelope(t *testing.T) {
	stdout := `{"type":"result","subtype":"error_during_execution","is_error":true,` +
		`"result":"MCP server aiarch-state failed to start","session_id":"s1"}`
	got := claudeOutputDetail(stdout, "")
	if !strings.Contains(got, "error_during_execution") {
		t.Fatalf("detail = %q, want the envelope subtype", got)
	}
	if !strings.Contains(got, "MCP server aiarch-state failed to start") {
		t.Fatalf("detail = %q, want the envelope result text", got)
	}
}

// A CLEAN envelope still carries the signal that matters on a no-commit run:
// claude believed it SUCCEEDED, which points at the prompt/agent rather than at
// infrastructure.
func TestClaudeOutputDetail_JSONSuccessEnvelopeSurfacesAgentMessage(t *testing.T) {
	stdout := `{"type":"result","subtype":"success","is_error":false,"result":"Nothing to do; the contract already exists."}`
	got := claudeOutputDetail(stdout, "")
	if !strings.Contains(got, "Nothing to do") {
		t.Fatalf("detail = %q, want the agent's own closing message", got)
	}
}

// A structured (object-valued) error field must still be readable, not dropped.
func TestClaudeOutputDetail_StructuredErrorField(t *testing.T) {
	stdout := `{"type":"result","is_error":true,"error":{"code":"auth","message":"invalid api key"}}`
	got := claudeOutputDetail(stdout, "")
	if !strings.Contains(got, "invalid api key") {
		t.Fatalf("detail = %q, want the structured error rendered", got)
	}
}

// claude may print warnings/progress BEFORE the final envelope: the last JSON
// object line still wins over a raw-text fallback.
func TestClaudeOutputDetail_JSONAfterLeadingNoise(t *testing.T) {
	stdout := "warning: something ambient\n" +
		`{"type":"result","subtype":"error_max_turns","is_error":true,"result":"ran out of turns"}` + "\n"
	got := claudeOutputDetail(stdout, "")
	if !strings.Contains(got, "error_max_turns") || !strings.Contains(got, "ran out of turns") {
		t.Fatalf("detail = %q, want the trailing envelope to win", got)
	}
}

// claude can die BEFORE emitting any JSON (crash, startup failure, usage error):
// the raw stdout tail is then the only artifact, and it must survive.
func TestClaudeOutputDetail_NonJSONStdoutFallsBackToRawTail(t *testing.T) {
	got := claudeOutputDetail("Invalid API key · Please run /login", "")
	if !strings.Contains(got, "Invalid API key") {
		t.Fatalf("detail = %q, want the raw stdout text", got)
	}
}

func TestClaudeOutputDetail_FallsBackToStderrOnlyWhenStdoutEmpty(t *testing.T) {
	if got := claudeOutputDetail("   ", "sandbox init failed"); !strings.Contains(got, "sandbox init failed") {
		t.Fatalf("detail = %q, want the stderr fallback", got)
	}
	// Caller passes stderrText="" when its leading sentence already carries the
	// stderr tail — the detail must NOT re-derive it from anywhere.
	if got := claudeOutputDetail("", ""); got != "" {
		t.Fatalf("detail = %q, want empty when there is no output at all", got)
	}
}

// The diagnostic reaches the web UI's failure panel: unbounded agent prose and
// multi-line output would wreck it.
func TestClaudeOutputDetail_BoundedAndSingleLine(t *testing.T) {
	for name, stdout := range map[string]string{
		"structured": `{"subtype":"success","result":"` + strings.Repeat("verbose ", 500) + `"}`,
		"raw":        strings.Repeat("noise\nmore noise\n", 500),
	} {
		got := claudeOutputDetail(stdout, "")
		if len(got) > localExecDetailMaxBytes+8 { // +8 tolerance for the label/ellipsis framing
			t.Fatalf("%s: detail is %d bytes, want it bounded near %d", name, len(got), localExecDetailMaxBytes)
		}
		if strings.ContainsAny(got, "\n\r\t") {
			t.Fatalf("%s: detail is not single-line: %q", name, got)
		}
	}
}

// LX-OBS1 (the live repro) — a clean exit that committed nothing now explains
// ITSELF: the leading sentence is preserved verbatim (downstream consumers match
// on its vocabulary) and claude's own envelope is appended after the separator.
func TestLocalExecObserve_NoCommit_EnrichesDiagnosticFromJSONEnvelope(t *testing.T) {
	_, url := newSharedRepo(t)
	installClaudeShim(t, "#!/bin/sh\n"+
		`echo '{"type":"result","subtype":"error_during_execution","is_error":true,"result":"MCP server aiarch-state failed to start"}'`+"\n"+
		"exit 0\n")
	a := newLocalExecForTest(t, url, 10*time.Second)

	handle, err := a.SubmitAgenticJob(subRC(context.Background(), "obs-json-key"),
		localSpec("C-OBSJSON", "someComponent", "service-construction"))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	obs := waitForTerminal(t, a, handle, 10*time.Second)
	if obs.Phase != PhaseFailed {
		t.Fatalf("Phase = %v, want PhaseFailed", obs.Phase)
	}
	if !strings.HasPrefix(obs.Diagnostic, "run completed but produced no commits on the target branch") {
		t.Fatalf("Diagnostic = %q, want the leading sentence preserved verbatim", obs.Diagnostic)
	}
	if !strings.Contains(obs.Diagnostic, localExecDetailSeparator) {
		t.Fatalf("Diagnostic = %q, want the detail separator", obs.Diagnostic)
	}
	if !strings.Contains(obs.Diagnostic, "MCP server aiarch-state failed to start") {
		t.Fatalf("Diagnostic = %q, want claude's own explanation appended", obs.Diagnostic)
	}
}

// LX-OBS2 — claude died before emitting JSON: the raw tail still reaches the
// operator rather than being swallowed.
func TestLocalExecObserve_NoCommit_NonJSONStdoutFallsBackToRawTail(t *testing.T) {
	_, url := newSharedRepo(t)
	installClaudeShim(t, "#!/bin/sh\necho 'Invalid API key · Please run /login'\nexit 0\n")
	a := newLocalExecForTest(t, url, 10*time.Second)

	handle, err := a.SubmitAgenticJob(subRC(context.Background(), "obs-raw-key"),
		localSpec("C-OBSRAW", "someComponent", "service-construction"))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	obs := waitForTerminal(t, a, handle, 10*time.Second)
	if !strings.Contains(obs.Diagnostic, "Invalid API key") {
		t.Fatalf("Diagnostic = %q, want the raw stdout tail appended", obs.Diagnostic)
	}
}

// LX-OBS3 (+ TR6) — the bounded clause is lossy BY DESIGN, so the full output
// must land somewhere durable that survives the run's own temp-dir cleanup.
// Since the SP1 capture seam the full stdout lives in the per-episode TRACE file
// under the shared repo, so the log dir records its PATH rather than duplicating
// it; stderr is still kept verbatim there.
func TestLocalExecFailedRun_WritesDurableOutputLogThatSurvivesCleanup(t *testing.T) {
	tmp := isolatedTempDir(t)
	repoDir, url := newSharedRepo(t)
	installClaudeShim(t, "#!/bin/sh\n"+
		`echo '{"type":"result","subtype":"success","is_error":false,"result":"POSTMORTEM-STDOUT-MARKER"}'`+"\n"+
		"echo 'POSTMORTEM-STDERR-MARKER' >&2\n"+
		"exit 0\n")
	a := newLocalExecForTest(t, url, 10*time.Second)

	const key fwra.IdempotencyKey = "obs-log-key"
	handle, err := a.SubmitAgenticJob(subRC(context.Background(), key),
		localSpec("C-OBSLOG", "someComponent", "service-construction"))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if obs := waitForTerminal(t, a, handle, 10*time.Second); obs.Phase != PhaseFailed {
		t.Fatalf("Phase = %v, want PhaseFailed", obs.Phase)
	}

	dirs := localExecLogDirs(t, tmp)
	if len(dirs) != 1 {
		t.Fatalf("found %d durable log dirs, want exactly 1 (%v)", len(dirs), dirs)
	}
	wantTrace := filepath.Join(repoDir, ".aiarch", "traces", dedupToken(key)+".jsonl")
	assertFileContains(t, filepath.Join(dirs[0], localExecTracePathFile), wantTrace)
	assertFileContains(t, filepath.Join(dirs[0], "stderr.log"), "POSTMORTEM-STDERR-MARKER")
	// The pointed-at trace really holds what the old verbatim stdout copy did.
	assertFileContains(t, wantTrace, "POSTMORTEM-STDOUT-MARKER")
	if _, err := os.Stat(filepath.Join(dirs[0], "stdout.json")); err == nil {
		t.Fatal("durable log still duplicates the full stdout as stdout.json")
	}
	// The worktree cleanup still ran — the durable log is SEPARATE from the run's
	// own temp dirs, not a suppression of their removal.
	assertNoLingeringWorktrees(t, repoDir)
}

// LX-OBS4 — the success path is unchanged: no diagnostic, and NO log litter
// accumulating in the operator's temp dir during normal operation.
func TestLocalExecSuccessfulRun_NoDiagnosticAndNoLogLitter(t *testing.T) {
	tmp := isolatedTempDir(t)
	_, url := newSharedRepo(t)
	commitShim(t, filepath.Join(t.TempDir(), "capture"))
	a := newLocalExecForTest(t, url, 10*time.Second)

	handle, err := a.SubmitAgenticJob(subRC(context.Background(), "obs-clean-key"),
		localSpec("C-OBSCLEAN", "someComponent", "service-construction"))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	obs := waitForTerminal(t, a, handle, 10*time.Second)
	if obs.Phase != PhaseSucceeded {
		t.Fatalf("Phase = %v, want PhaseSucceeded (diagnostic: %q)", obs.Phase, obs.Diagnostic)
	}
	if obs.Diagnostic != "" {
		t.Fatalf("Diagnostic = %q, want empty on the success path", obs.Diagnostic)
	}
	if dirs := localExecLogDirs(t, tmp); len(dirs) != 0 {
		t.Fatalf("successful run left %d durable log dirs, want 0 (%v)", len(dirs), dirs)
	}
}

// isolatedTempDir points os.MkdirTemp("", ...) — and therefore the durable log
// dir — at a per-test directory, so the log-litter assertions above observe ONLY
// this test's runs and never the developer's real temp dir.
func isolatedTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	return dir
}

func localExecLogDirs(t *testing.T, tempRoot string) []string {
	t.Helper()
	dirs, err := filepath.Glob(filepath.Join(tempRoot, localExecLogDirPattern))
	if err != nil {
		t.Fatalf("glob durable log dirs: %v", err)
	}
	return dirs
}

func assertFileContains(t *testing.T, path, want string) {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // test-owned temp path
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(b), want) {
		t.Fatalf("%s = %q, want it to contain %q", path, string(b), want)
	}
}

// ---------------------------------------------------------------------------
// SEC1-SEC4 — Fix-subagent Task 6: sandboxed-by-default hardening.
//
//	SEC1 claudeArgv: --dangerously-skip-permissions is ALWAYS paired with
//	     --settings <sandboxSettingsPath> by default (THE INVARIANT).
//	SEC2 claudeArgv: the ARCHISTRATOR_LOCAL_EXEC_ALLOW_UNSANDBOXED=true escape
//	     hatch — and ONLY that env var — omits --settings, while
//	     --dangerously-skip-permissions is STILL present (headless has no
//	     human to approve tool calls either way).
//	SEC3 sandboxAllowedDomains: always carries the fixed Anthropic API
//	     domain; adds the git remote's host ONLY for a non-file:// repoURL;
//	     a malformed repoURL degrades to just the fixed domain (never fails).
//	SEC4 writeSandboxSettings: the written file round-trips the fixed
//	     enabled/failIfUnavailable/allowUnsandboxedCommands posture plus the
//	     given network allowlist.
// ---------------------------------------------------------------------------

func TestClaudeArgv_DefaultPairsSkipPermissionsWithActiveSandbox(t *testing.T) {
	args := claudeArgv("/service-construction c a", "/tmp/mcp.json", "/tmp/sandbox.json")
	mustContainArg(t, args, "--dangerously-skip-permissions")
	mustContainAdjacentPair(t, args, "--settings", "/tmp/sandbox.json")
	mustContainAdjacentPair(t, args, "--mcp-config", "/tmp/mcp.json")
	mustContainArg(t, args, "--strict-mcp-config")
}

func TestClaudeArgv_EscapeHatch_OmitsSandboxSettingsButKeepsSkipPermissions(t *testing.T) {
	t.Setenv(localExecAllowUnsandboxedEnv, "true")
	args := claudeArgv("/service-construction c a", "/tmp/mcp.json", "/tmp/sandbox.json")
	mustContainArg(t, args, "--dangerously-skip-permissions") // still required: headless, no human to prompt
	if containsArg(args, "--settings") || containsArg(args, "/tmp/sandbox.json") {
		t.Fatalf("escape hatch active but sandbox settings still present in argv: %v", args)
	}
	mustContainAdjacentPair(t, args, "--mcp-config", "/tmp/mcp.json")
}

func TestClaudeArgv_EscapeHatch_CaseInsensitiveAndTrimmed(t *testing.T) {
	for _, v := range []string{"true", "TRUE", " True "} {
		t.Setenv(localExecAllowUnsandboxedEnv, v)
		if !allowUnsandboxedFromEnv() {
			t.Fatalf("allowUnsandboxedFromEnv() = false for %q, want true", v)
		}
	}
	for _, v := range []string{"", "false", "1", "yes"} {
		t.Setenv(localExecAllowUnsandboxedEnv, v)
		if allowUnsandboxedFromEnv() {
			t.Fatalf("allowUnsandboxedFromEnv() = true for %q, want false (fail-closed default)", v)
		}
	}
}

func mustContainArg(t *testing.T, args []string, want string) {
	t.Helper()
	if !containsArg(args, want) {
		t.Fatalf("argv %v missing %q", args, want)
	}
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// mustContainAdjacentPair asserts flag immediately precedes value somewhere
// in args (a "--flag value" pair, as exec.Cmd.Args expects — no shell
// splitting involved).
func mustContainAdjacentPair(t *testing.T, args []string, flag, value string) {
	t.Helper()
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return
		}
	}
	t.Fatalf("argv %v missing adjacent pair %q %q", args, flag, value)
}

func TestSandboxAllowedDomains(t *testing.T) {
	cases := []struct {
		name    string
		repoURL string
		want    []string
	}{
		{"file url — no remote host", "file:///tmp/x.git", []string{"api.anthropic.com"}},
		{"http(s) remote — host appended", "https://github.com/acme/repo.git", []string{"api.anthropic.com", "github.com"}},
		{"malformed — degrades to just the fixed domain", "not a\x00url", []string{"api.anthropic.com"}},
		{"empty — degrades to just the fixed domain", "", []string{"api.anthropic.com"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sandboxAllowedDomains(c.repoURL)
			if len(got) != len(c.want) {
				t.Fatalf("sandboxAllowedDomains(%q) = %v, want %v", c.repoURL, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("sandboxAllowedDomains(%q) = %v, want %v", c.repoURL, got, c.want)
				}
			}
		})
	}
}

func TestWriteSandboxSettings_Envelope(t *testing.T) {
	dir := t.TempDir()
	path, err := writeSandboxSettings(dir, []string{"api.anthropic.com", "example.com"}, nil)
	if err != nil {
		t.Fatalf("writeSandboxSettings: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written settings file: %v", err)
	}
	var cfg sandboxSettingsJSON
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("decode written settings file: %v\n%s", err, raw)
	}
	if !cfg.Sandbox.Enabled || !cfg.Sandbox.FailIfUnavailable || cfg.Sandbox.AllowUnsandboxedCommands {
		t.Fatalf("unexpected sandbox posture: %+v", cfg.Sandbox)
	}
	if cfg.Sandbox.Network == nil {
		t.Fatal("expected a non-nil network allowlist")
	}
	want := []string{"api.anthropic.com", "example.com"}
	if len(cfg.Sandbox.Network.AllowedDomains) != len(want) {
		t.Fatalf("AllowedDomains = %v, want %v", cfg.Sandbox.Network.AllowedDomains, want)
	}
	for i, d := range want {
		if cfg.Sandbox.Network.AllowedDomains[i] != d {
			t.Fatalf("AllowedDomains = %v, want %v", cfg.Sandbox.Network.AllowedDomains, want)
		}
	}
}

func TestWriteSandboxSettings_EmptyDomainsOmitsNetworkBlock(t *testing.T) {
	dir := t.TempDir()
	path, err := writeSandboxSettings(dir, nil, nil)
	if err != nil {
		t.Fatalf("writeSandboxSettings: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written settings file: %v", err)
	}
	var cfg sandboxSettingsJSON
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("decode written settings file: %v\n%s", err, raw)
	}
	if cfg.Sandbox.Network != nil {
		t.Fatalf("expected a nil network block for an empty allowlist, got %+v", cfg.Sandbox.Network)
	}
	if cfg.Sandbox.Filesystem != nil {
		t.Fatalf("expected a nil filesystem block for an empty allowWrite list, got %+v", cfg.Sandbox.Filesystem)
	}
}

// TestWriteSandboxSettings_FilesystemAllowWrite proves the worktree-mode
// filesystem scope round-trips: the given allowWrite paths land verbatim under
// sandbox.filesystem.allowWrite (the documented Claude Code settings key).
func TestWriteSandboxSettings_FilesystemAllowWrite(t *testing.T) {
	dir := t.TempDir()
	want := []string{"/tmp/aiarch-construct-x/wt", "/home/dev/project/.git"}
	path, err := writeSandboxSettings(dir, nil, want)
	if err != nil {
		t.Fatalf("writeSandboxSettings: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written settings file: %v", err)
	}
	var cfg sandboxSettingsJSON
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("decode written settings file: %v\n%s", err, raw)
	}
	if cfg.Sandbox.Filesystem == nil {
		t.Fatal("expected a non-nil filesystem block")
	}
	if len(cfg.Sandbox.Filesystem.AllowWrite) != len(want) {
		t.Fatalf("AllowWrite = %v, want %v", cfg.Sandbox.Filesystem.AllowWrite, want)
	}
	for i, p := range want {
		if cfg.Sandbox.Filesystem.AllowWrite[i] != p {
			t.Fatalf("AllowWrite = %v, want %v", cfg.Sandbox.Filesystem.AllowWrite, want)
		}
	}
}

// TestLocalExecSubmit_AllowUnsandboxedEscapeHatch_EndToEnd is the SEC2
// integration proof (unit proof is TestClaudeArgv_EscapeHatch_* above): a
// real dispatch with the escape hatch set spawns claude WITHOUT --settings
// and still succeeds (--dangerously-skip-permissions remains).
func TestLocalExecSubmit_AllowUnsandboxedEscapeHatch_EndToEnd(t *testing.T) {
	_, url := newSharedRepo(t)
	capture := filepath.Join(t.TempDir(), "capture")
	commitShim(t, capture)
	t.Setenv(localExecAllowUnsandboxedEnv, "true")
	a := newLocalExecForTest(t, url, 10*time.Second)

	spec := localSpec("C-ESCAPEHATCH", "billingGatewayAccess", "service-construction")
	handle, err := a.SubmitAgenticJob(subRC(context.Background(), "escape-key-1"), spec)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	obs := waitForTerminal(t, a, handle, 10*time.Second)
	if obs.Phase != PhaseSucceeded {
		t.Fatalf("Phase = %v, want PhaseSucceeded (diagnostic: %q)", obs.Phase, obs.Diagnostic)
	}

	args := readCapturedArgs(t, capture, 0)
	if strings.Contains(args, "--settings") {
		t.Fatalf("escape hatch active but --settings still present in captured argv: %q", args)
	}
	if !strings.Contains(args, "--dangerously-skip-permissions") {
		t.Fatal("escape hatch must still pass --dangerously-skip-permissions (headless has no human to approve tool calls)")
	}
	if !strings.Contains(args, "--strict-mcp-config") {
		t.Fatal("escape hatch must NOT also disable --strict-mcp-config (orthogonal Tier-1 protection)")
	}
	if _, err := os.Stat(filepath.Join(capture, "call-0.settings.json")); err == nil {
		t.Fatal("escape hatch active but a --settings file was still captured")
	}
}

// ---------------------------------------------------------------------------
// LM1-LM5 — local merge job (DispatchInputs["job"]="merge")
// ---------------------------------------------------------------------------

// mergeJobSpec builds a merge-job PipelineSpec for the given activity — the
// shape the Manager's local merge step dispatches (no "command": the merge job
// never spawns claude).
func mergeJobSpec(activityID string) PipelineSpec {
	return PipelineSpec{
		ActivityID: ConstructionActivityID(activityID),
		Steps:      []PipelineStep{{Name: "build", Toolchain: "go-1.23", Command: []string{"sh", "-c", "true"}}},
		DispatchInputs: map[string]string{
			DispatchInputJobKey: DispatchJobMerge,
			"activity_id":       activityID,
		},
	}
}

// seedActivityBranch creates activity/<id> off main in the shared repo with one
// commit writing path=content — the state a completed construction run leaves
// behind for the merge job to land.
func seedActivityBranch(t *testing.T, sharedDir, activityID, path, content string) {
	t.Helper()
	work := filepath.Join(t.TempDir(), "seed-branch")
	testGit(t, "", "clone", sharedDir, work)
	testGit(t, work, "config", "user.email", "seed@aiarch.local")
	testGit(t, work, "config", "user.name", "seed")
	testGit(t, work, "checkout", "-b", "activity/"+activityID, "main")
	if err := os.WriteFile(filepath.Join(work, path), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	testGit(t, work, "add", "-A")
	testGit(t, work, "commit", "-m", "branch work")
	testGit(t, work, "push", "origin", "activity/"+activityID)
}

// commitFileOnMain lands one commit on main in the shared repo writing
// path=content — used to force divergence (and conflicts) against a branch.
func commitFileOnMain(t *testing.T, sharedDir, path, content string) {
	t.Helper()
	work := filepath.Join(t.TempDir(), "main-edit")
	testGit(t, "", "clone", "--branch", "main", sharedDir, work)
	testGit(t, work, "config", "user.email", "seed@aiarch.local")
	testGit(t, work, "config", "user.name", "seed")
	if err := os.WriteFile(filepath.Join(work, path), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	testGit(t, work, "add", "-A")
	testGit(t, work, "commit", "-m", "main edit")
	testGit(t, work, "push", "origin", "main")
}

// LM1 — happy path: a real --no-ff merge commit lands on main, the branch is
// deleted, Observe reports Succeeded.
func TestLocalExecMergeJob_MergesNoFFAndDeletesBranch(t *testing.T) {
	sharedDir, url := newSharedRepo(t)
	seedActivityBranch(t, sharedDir, "C-M1", "work.txt", "branch content\n")
	a := newLocalExecForTest(t, url, 0)

	handle, err := a.SubmitAgenticJob(subRC(context.Background(), "merge-key-1"), mergeJobSpec("C-M1"))
	if err != nil {
		t.Fatalf("Submit(merge): %v", err)
	}
	obs, err := a.ObserveAgenticJob(obsRC(context.Background()), handle)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if obs.Phase != PhaseSucceeded {
		t.Fatalf("Phase = %v, want PhaseSucceeded (diagnostic: %q)", obs.Phase, obs.Diagnostic)
	}
	// The tip of main is a REAL --no-ff merge commit (two parents).
	parents := strings.Fields(strings.TrimSpace(testGitOut(t, sharedDir, "log", "-1", "--format=%P", "main")))
	if len(parents) != 2 {
		t.Fatalf("main tip has %d parents, want 2 (a --no-ff merge commit)", len(parents))
	}
	// The branch's work is reachable from main.
	if got := testGitOut(t, sharedDir, "cat-file", "-p", "main:work.txt"); !strings.Contains(got, "branch content") {
		t.Fatalf("main:work.txt = %q, want the branch's content", got)
	}
	if remoteBranchExists(t, sharedDir, "activity/C-M1") {
		t.Fatal("activity branch must be deleted after the merge")
	}
}

// LM2 — a merge conflict is a FAILED run with a "merge conflict" diagnostic and
// leaves the shared repo untouched: main unmoved, the branch still present.
func TestLocalExecMergeJob_ConflictFailsCleanly(t *testing.T) {
	sharedDir, url := newSharedRepo(t)
	seedActivityBranch(t, sharedDir, "C-M2", "conflict.txt", "branch side\n")
	commitFileOnMain(t, sharedDir, "conflict.txt", "main side\n")
	mainBefore := strings.TrimSpace(testGitOut(t, sharedDir, "rev-parse", "main"))
	a := newLocalExecForTest(t, url, 0)

	handle, err := a.SubmitAgenticJob(subRC(context.Background(), "merge-key-2"), mergeJobSpec("C-M2"))
	if err != nil {
		t.Fatalf("Submit(merge): %v", err)
	}
	obs, err := a.ObserveAgenticJob(obsRC(context.Background()), handle)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if obs.Phase != PhaseFailed {
		t.Fatalf("Phase = %v, want PhaseFailed", obs.Phase)
	}
	if !containsFold(obs.Diagnostic, "merge conflict") {
		t.Fatalf("diagnostic %q must name the merge conflict", obs.Diagnostic)
	}
	if got := strings.TrimSpace(testGitOut(t, sharedDir, "rev-parse", "main")); got != mainBefore {
		t.Fatalf("main moved on a conflicted merge: %s -> %s (partial merge left behind)", mainBefore, got)
	}
	if !remoteBranchExists(t, sharedDir, "activity/C-M2") {
		t.Fatal("activity branch must survive a conflicted merge")
	}
}

// LM3 — idempotency under a crashed prior attempt: the branch is already an
// ancestor of main (merged, not yet deleted) → no second merge commit, the
// delete completes, Succeeded.
func TestLocalExecMergeJob_AlreadyMergedDeletesBranchOnly(t *testing.T) {
	sharedDir, url := newSharedRepo(t)
	seedActivityBranch(t, sharedDir, "C-M3", "work.txt", "branch content\n")
	// Simulate the prior attempt's landed merge (without the branch delete).
	work := filepath.Join(t.TempDir(), "prior-merge")
	testGit(t, "", "clone", "--branch", "main", sharedDir, work)
	testGit(t, work, "config", "user.email", "seed@aiarch.local")
	testGit(t, work, "config", "user.name", "seed")
	testGit(t, work, "merge", "--no-ff", "-m", "prior merge", "origin/activity/C-M3")
	testGit(t, work, "push", "origin", "main")
	mainBefore := strings.TrimSpace(testGitOut(t, sharedDir, "rev-parse", "main"))
	a := newLocalExecForTest(t, url, 0)

	handle, err := a.SubmitAgenticJob(subRC(context.Background(), "merge-key-3"), mergeJobSpec("C-M3"))
	if err != nil {
		t.Fatalf("Submit(merge): %v", err)
	}
	obs, err := a.ObserveAgenticJob(obsRC(context.Background()), handle)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if obs.Phase != PhaseSucceeded {
		t.Fatalf("Phase = %v, want PhaseSucceeded (diagnostic: %q)", obs.Phase, obs.Diagnostic)
	}
	if got := strings.TrimSpace(testGitOut(t, sharedDir, "rev-parse", "main")); got != mainBefore {
		t.Fatalf("already-merged path must not add a second merge commit: %s -> %s", mainBefore, got)
	}
	if remoteBranchExists(t, sharedDir, "activity/C-M3") {
		t.Fatal("activity branch must be deleted on the already-merged path")
	}
}

// LM4 — a merge job for a branch that does not exist is an honest failure
// naming the branch.
func TestLocalExecMergeJob_MissingBranchFails(t *testing.T) {
	_, url := newSharedRepo(t)
	a := newLocalExecForTest(t, url, 0)

	handle, err := a.SubmitAgenticJob(subRC(context.Background(), "merge-key-4"), mergeJobSpec("C-NOPE"))
	if err != nil {
		t.Fatalf("Submit(merge): %v", err)
	}
	obs, err := a.ObserveAgenticJob(obsRC(context.Background()), handle)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if obs.Phase != PhaseFailed {
		t.Fatalf("Phase = %v, want PhaseFailed", obs.Phase)
	}
	if !strings.Contains(obs.Diagnostic, "activity/C-NOPE") {
		t.Fatalf("diagnostic %q must name the missing branch", obs.Diagnostic)
	}
}

// LM5 — same idempotencyKey converges on the same handle/run: exactly one merge
// commit on main.
func TestLocalExecMergeJob_IdempotencyConvergence(t *testing.T) {
	sharedDir, url := newSharedRepo(t)
	seedActivityBranch(t, sharedDir, "C-M5", "work.txt", "branch content\n")
	a := newLocalExecForTest(t, url, 0)

	h1, err := a.SubmitAgenticJob(subRC(context.Background(), "merge-key-5"), mergeJobSpec("C-M5"))
	if err != nil {
		t.Fatalf("Submit(merge) #1: %v", err)
	}
	h2, err := a.SubmitAgenticJob(subRC(context.Background(), "merge-key-5"), mergeJobSpec("C-M5"))
	if err != nil {
		t.Fatalf("Submit(merge) #2: %v", err)
	}
	if !PipelineHandleEqual(h1, h2) {
		t.Fatalf("same key returned different handles: %q vs %q", h1, h2)
	}
	// Exactly one merge commit: seed + branch commit + one merge = 3 on main.
	if n := remoteCommitCount(t, sharedDir, "main"); n != 3 {
		t.Fatalf("main has %d commits, want 3 (seed + branch work + ONE merge)", n)
	}
}

// ---------------------------------------------------------------------------
// LD1-LD5 — DESIGN-JOB ARM (job_mode-discriminated, NO ActivityID). The local
// counterpart of aiarch-design.yml's draft job: worktree on the design SESSION
// branch, "-p /<command>" (no args), and the aiarch-design.yml MCP envelope. On the
// local profile the branch-staging rail is dormant, so the executor CREATES the
// session branch off main on first use and re-attaches to its tip on a redraft.
// ---------------------------------------------------------------------------

// designSpec builds a DESIGN-job PipelineSpec: NO ActivityID (its parameters ride
// DispatchInputs), a placeholder step to satisfy validateSpec, and the design
// DispatchInputs the design Managers set (job_mode discriminates the arm).
func designSpec(command, targetBranch, artifactKind, jobMode string) PipelineSpec {
	return PipelineSpec{
		Steps: []PipelineStep{{Name: "design", Toolchain: "go-1.23", Command: []string{"sh", "-c", "true"}}},
		DispatchInputs: map[string]string{
			"job_mode":        jobMode,
			"command":         command,
			"target_branch":   targetBranch,
			"artifact_kind":   artifactKind,
			"prior_state_ref": "",
		},
	}
}

// seedDesignBranch stages a MID-session design SESSION branch off main in the shared
// repo with one extra commit — the state a prior draft/critique/answer job leaves for a
// redraft to re-attach to (the local branch-staging rail is dormant, so this stands in
// for a branch a PRIOR executor run created; a FIRST-of-session job has no such branch
// and the executor creates it off main).
func seedDesignBranch(t *testing.T, sharedDir, branch string) {
	t.Helper()
	work := filepath.Join(t.TempDir(), "seed-design-branch")
	testGit(t, "", "clone", sharedDir, work)
	testGit(t, work, "config", "user.email", "seed@aiarch.local")
	testGit(t, work, "config", "user.name", "seed")
	testGit(t, work, "checkout", "-b", branch, "main")
	testGit(t, work, "commit", "--allow-empty", "-m", "session branch base")
	testGit(t, work, "push", "origin", branch)
}

// assertBranchForkedOffMain asserts main's tip is an ancestor of branch — i.e. the
// branch was created off main (the local stand-in for the cloud's OpenBranch), so the
// worktree checked out a tree carrying main's committed state.
func assertBranchForkedOffMain(t *testing.T, sharedDir, branch string) {
	t.Helper()
	cmd := exec.Command("git", "-C", sharedDir, "merge-base", "--is-ancestor", localMainBranch, branch)
	if err := cmd.Run(); err != nil {
		t.Fatalf("branch %s is not a descendant of %s — expected it created off main: %v", branch, localMainBranch, err)
	}
}

// LD1 — FIRST-of-session happy path across ALL THREE job modes (draft/critique/answer all
// discriminate the design arm off a non-empty job_mode). The session branch does NOT exist
// yet, so the executor creates it off main (the branch-staging rail is dormant locally) and
// the worktree sees main's committed state. Full envelope + prompt + worktree +
// branch-created-off-main + branch-advance assertions each time; the mechanism is
// mode-agnostic at this layer (the .claude command decides model vs verdict vs responses —
// the RA only spawns).
func TestLocalExecSubmit_DesignJob_FirstOfSession_CreatesBranchOffMain_AllModes(t *testing.T) {
	for _, mode := range []string{"draft", "critique", "answer"} {
		t.Run(mode, func(t *testing.T) {
			sharedDir, url := newSharedRepo(t)
			branch := "aiarch-design/mission/session-" + mode
			// Deliberately NOT seeded: this is the first job of the session, so nothing has
			// staged the branch — the executor must create it off main.
			if remoteBranchExists(t, sharedDir, branch) {
				t.Fatalf("precondition: session branch %s must not exist before the first job", branch)
			}
			capture := filepath.Join(t.TempDir(), "capture")
			commitShim(t, capture)
			t.Setenv("TERM", "xterm-test")
			a := newLocalExecForTest(t, url, 10*time.Second)

			command := "mission-" + mode
			spec := designSpec(command, branch, "Mission", mode)
			handle, err := a.SubmitAgenticJob(subRC(context.Background(), fwra.IdempotencyKey("design-key-"+mode)), spec)
			if err != nil {
				t.Fatalf("Submit(design %s): %v", mode, err)
			}
			if handle == "" {
				t.Fatal("Submit returned a zero handle")
			}

			obs := waitForTerminal(t, a, handle, 10*time.Second)
			if obs.Phase != PhaseSucceeded {
				t.Fatalf("[%s] Phase = %v, want PhaseSucceeded (diagnostic: %q)", mode, obs.Phase, obs.Diagnostic)
			}

			// The executor created the session branch off main (the local stand-in for the
			// cloud's OpenBranch), so the worktree saw main's committed state, and the drafted
			// commit advanced the branch directly (no push): main's seed + the drafted commit = 2.
			if !remoteBranchExists(t, sharedDir, branch) {
				t.Fatalf("session branch %s was not created by the design arm", branch)
			}
			assertBranchForkedOffMain(t, sharedDir, branch)
			if got := remoteCommitCount(t, sharedDir, branch); got != 2 {
				t.Fatalf("commit count on %s = %d, want 2 (main seed + drafted commit)", branch, got)
			}
			assertNoLingeringWorktrees(t, sharedDir)

			// Prompt is EXACTLY "/<command>" — no component/activity args (design shape).
			args := readCapturedArgs(t, capture, 0)
			assertClaudeArgsShape(t, args, "-p\n/"+command)
			if strings.Contains(args, "/"+command+" ") {
				t.Fatalf("[%s] design prompt carried trailing args, want a bare \"/%s\": %q", mode, command, args)
			}

			pwd := readCapturedPWD(t, capture, 0)
			if pwd == "" || pwd == sharedDir {
				t.Fatalf("[%s] claude cwd = %q, want a throwaway worktree distinct from the shared repo", mode, pwd)
			}

			// --mcp-config envelope: the EXACT aiarch-design.yml set (assertMCPConfigEnvelope
			// also checks AIARCH_STATE_ROOT == the worktree cwd).
			assertMCPConfigEnvelope(t, capture, 0, pwd, map[string]string{
				"AIARCH_PROJECT_ID":    "test-project",
				"AIARCH_ARTIFACT_KIND": "Mission",
				"AIARCH_JOB_MODE":      mode,
				"AIARCH_TARGET_BRANCH": branch,
			})

			// The design envelope carries NO construct-only ambient vars, and is EXACTLY
			// the 5-var aiarch-design.yml set — proving the arm did not leak the construct rig.
			env := readCapturedEnv(t, capture, 0)
			for _, forbidden := range []string{"AIARCH_COMPONENT_ID", "AIARCH_ACTIVITY_ID"} {
				if _, present := env[forbidden]; present {
					t.Fatalf("[%s] design child env leaked construct-only var %s (%v)", mode, forbidden, envKeys(env))
				}
			}
			wantAIARCH := []string{"AIARCH_PROJECT_ID", "AIARCH_ARTIFACT_KIND", "AIARCH_JOB_MODE", "AIARCH_TARGET_BRANCH", "AIARCH_STATE_ROOT"}
			for _, k := range wantAIARCH {
				if _, ok := env[k]; !ok {
					t.Errorf("[%s] design child env missing %s", mode, k)
				}
			}
			gotAIARCH := 0
			for k := range env {
				if strings.HasPrefix(k, "AIARCH_") {
					gotAIARCH++
				}
			}
			if gotAIARCH != len(wantAIARCH) {
				t.Fatalf("[%s] design child env has %d AIARCH_* vars, want exactly %d %v; got %v", mode, gotAIARCH, len(wantAIARCH), wantAIARCH, envKeys(env))
			}

			// Tier-2 sandbox posture unchanged (THE INVARIANT), and the filesystem scope
			// covers the worktree + the shared git dir — identical to the construct arm.
			sandboxCfg := assertSandboxSettingsEnvelope(t, capture, 0)
			assertSandboxFilesystemAllowWrite(t, sandboxCfg, pwd, filepath.Join(sharedDir, ".git"))
		})
	}
}

// LD2 — a design-job submit missing the command dispatch input is ContractMisuse
// (exactly as the construct arm rejects a missing command), BEFORE any worktree/spawn.
func TestLocalExecSubmit_DesignJob_MissingCommand_ContractMisuse(t *testing.T) {
	_, url := newSharedRepo(t)
	a := newLocalExecForTest(t, url, 0)
	spec := designSpec("", "design/mission/s1", "Mission", "draft")
	_, err := a.SubmitAgenticJob(subRC(context.Background(), "d-nocommand"), spec)
	if kind(err) != fwra.ContractMisuse {
		t.Fatalf("kind = %v, want ContractMisuse", kind(err))
	}
}

// LD3 — a design-job submit missing the target_branch dispatch input is ContractMisuse.
func TestLocalExecSubmit_DesignJob_MissingTargetBranch_ContractMisuse(t *testing.T) {
	_, url := newSharedRepo(t)
	a := newLocalExecForTest(t, url, 0)
	spec := designSpec("mission-draft", "", "Mission", "draft")
	_, err := a.SubmitAgenticJob(subRC(context.Background(), "d-nobranch"), spec)
	if kind(err) != fwra.ContractMisuse {
		t.Fatalf("kind = %v, want ContractMisuse", kind(err))
	}
}

// LD4 — a MID-session design job whose SESSION branch already exists (a prior
// draft/critique/answer job opened it) re-attaches to its TIP: the branch is NEVER
// recreated/reset, the prior session commits are preserved, and the drafted commit lands
// on top. This is the counterpart of LD1's first-of-session create-off-main path.
func TestLocalExecSubmit_DesignJob_ExistingSessionBranch_ReattachesToTip(t *testing.T) {
	sharedDir, url := newSharedRepo(t)
	branch := "aiarch-design/mission/session-redraft"
	seedDesignBranch(t, sharedDir, branch) // a prior job's session branch (main seed + a session-base commit)
	priorTip := strings.TrimSpace(testGitOut(t, sharedDir, "rev-parse", branch))
	capture := filepath.Join(t.TempDir(), "capture")
	commitShim(t, capture)
	a := newLocalExecForTest(t, url, 10*time.Second)

	spec := designSpec("mission-draft", branch, "Mission", "draft")
	handle, err := a.SubmitAgenticJob(subRC(context.Background(), "d-redraft"), spec)
	if err != nil {
		t.Fatalf("Submit(design redraft): %v", err)
	}
	obs := waitForTerminal(t, a, handle, 10*time.Second)
	if obs.Phase != PhaseSucceeded {
		t.Fatalf("Phase = %v, want PhaseSucceeded (diagnostic: %q)", obs.Phase, obs.Diagnostic)
	}

	// Re-attached to the existing tip, not recreated: the drafted commit is a DESCENDANT
	// of the prior tip (prior session commits preserved), and the count is main seed +
	// session base + drafted = 3.
	cmd := exec.Command("git", "-C", sharedDir, "merge-base", "--is-ancestor", priorTip, branch)
	if err := cmd.Run(); err != nil {
		t.Fatalf("prior session tip %s is not an ancestor of %s — the branch was reset, not re-attached: %v", priorTip, branch, err)
	}
	if got := remoteCommitCount(t, sharedDir, branch); got != 3 {
		t.Fatalf("commit count on %s = %d, want 3 (main seed + session base + drafted commit)", branch, got)
	}
}

// LD5 — two design submits with the SAME idempotencyKey converge on the SAME handle and
// spawn claude exactly ONCE (the in-memory run-record short-circuit, same as construct).
// First-of-session (unseeded), so the arm also creates the branch off main exactly once.
func TestLocalExecSubmit_DesignJob_DuplicateKey_ConvergesWithoutRedispatch(t *testing.T) {
	sharedDir, url := newSharedRepo(t)
	branch := "aiarch-design/glossary/session-1"
	capture := filepath.Join(t.TempDir(), "capture")
	commitShim(t, capture)
	a := newLocalExecForTest(t, url, 10*time.Second)

	spec := designSpec("glossary-draft", branch, "Glossary", "draft")
	h1, err := a.SubmitAgenticJob(subRC(context.Background(), "design-same-key"), spec)
	if err != nil {
		t.Fatalf("Submit (1): %v", err)
	}
	h2, err := a.SubmitAgenticJob(subRC(context.Background(), "design-same-key"), spec)
	if err != nil {
		t.Fatalf("Submit (2): %v", err)
	}
	if h1 != h2 {
		t.Fatalf("handles diverged for the same idempotencyKey: %q != %q", h1, h2)
	}
	waitForTerminal(t, a, h1, 10*time.Second)

	if _, err := os.Stat(filepath.Join(capture, "call-1.args")); !os.IsNotExist(err) {
		t.Fatalf("expected exactly one claude invocation, found a second (call-1.args exists, stat err=%v)", err)
	}
	if got := remoteCommitCount(t, sharedDir, branch); got != 2 {
		t.Fatalf("commit count on %s = %d, want 2 (main seed + ONE drafted commit)", branch, got)
	}
}

// ---------------------------------------------------------------------------
// LS1 — SEAT-ASSETS (the .claude prompt-surface materialization the local arm was
// missing, causing "Unknown command: /<command>" no-commit runs). The executor must
// run `aiarch-state-mcp seat-assets --dest <workDir>` BEFORE spawning claude, exactly
// as both seated workflow templates do, so the slash command resolves.
// ---------------------------------------------------------------------------

// recordingStateMCPBin writes a fake aiarch-state-mcp that (a) RECORDS its argv (one
// arg per line) into captureDir/seat-args and (b) mimics real `seat-assets` by rendering
// a marker into <--dest>/.claude, then exits 0. A downstream claude shim can then PROVE
// seat-assets ran BEFORE it, in the SAME worktree, by requiring that marker — a behavioral
// ordering proof that needs no timestamps. (The real binary renders the whole .claude
// surface via methodassets.Materialize; the executor only needs it to exist + exit 0, so
// a recorder is a faithful stand-in — same pattern as the claude shim.)
func recordingStateMCPBin(t *testing.T, captureDir string) string {
	t.Helper()
	if err := os.MkdirAll(captureDir, 0o755); err != nil {
		t.Fatalf("mkdir seat capture dir: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "aiarch-state-mcp")
	// $CAPTURE is baked in at generation time (not read from the child env) so the
	// capture location is independent of the minimal env the executor passes.
	script := "#!/bin/sh\n" +
		"CAPTURE='" + captureDir + "'\n" +
		"printf '%s\\n' \"$@\" >> \"$CAPTURE/seat-args\"\n" +
		// Extract the --dest value and render the marker there (mimics seat-assets
		// writing .claude/** into the checkout root).
		"dest=''\n" +
		"prev=''\n" +
		"for a in \"$@\"; do if [ \"$prev\" = \"--dest\" ]; then dest=\"$a\"; fi; prev=\"$a\"; done\n" +
		"if [ -n \"$dest\" ]; then mkdir -p \"$dest/.claude/commands\" && : > \"$dest/.claude/SEATED\"; fi\n" +
		"exit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil { //nolint:gosec // test stub, deliberately executable
		t.Fatalf("write recording state-mcp bin: %v", err)
	}
	return path
}

// readSeatArgs reads the recording state-mcp stub's captured argv (one arg per line).
func readSeatArgs(t *testing.T, captureDir string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(captureDir, "seat-args"))
	if err != nil {
		t.Fatalf("read seat-assets args: %v", err)
	}
	var out []string
	for _, l := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// LS1 — the executor materializes the .claude prompt surface before spawning claude. The
// recording state-mcp stub records `seat-assets --dest <dir>` and writes a marker into
// that dir; the claude shim FAILS unless the marker is present in its cwd, then commits.
// So PhaseSucceeded ⟺ seat-assets ran BEFORE claude in claude's OWN worktree, and the
// recorded argv proves the exact `seat-assets --dest <workDir>` invocation shape.
func TestLocalExecSubmit_SeatsClaudePromptSurfaceBeforeSpawn(t *testing.T) {
	sharedDir, url := newSharedRepo(t)
	seatCapture := filepath.Join(t.TempDir(), "seat")
	stateMCPBin := recordingStateMCPBin(t, seatCapture)

	// A claude shim that proves ordering: it exits non-zero unless the seat marker
	// exists in its cwd (i.e. seat-assets ran first, in this worktree), else it commits.
	installClaudeShim(t, "#!/bin/sh\n"+
		"set -e\n"+
		"test -f .claude/SEATED || { echo 'seat marker missing — seat-assets did not run before claude' >&2; exit 9; }\n"+
		"git config user.email shim@aiarch.local\n"+
		"git config user.name shim\n"+
		"echo drafted >> DRAFTED.txt\n"+
		"git add -A\n"+
		"git commit -m 'drafted after seat' >/dev/null\n"+
		"echo '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false}'\n"+
		"exit 0\n")

	v, err := NewLocalExecAgenticJobAccess(url, "test-project", stateMCPBin, 10*time.Second)
	if err != nil {
		t.Fatalf("NewLocalExecAgenticJobAccess: %v", err)
	}
	a, ok := v.(*localExecAccess)
	if !ok {
		t.Fatalf("expected *localExecAccess, got %T", v)
	}

	branch := "aiarch-design/mission/session-seat"
	spec := designSpec("mission-draft", branch, "Mission", "draft")
	handle, err := a.SubmitAgenticJob(subRC(context.Background(), "seat-key"), spec)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	obs := waitForTerminal(t, a, handle, 10*time.Second)
	// PhaseSucceeded is the ordering proof: the shim committed, which it only does when
	// the seat marker is present — so seat-assets ran BEFORE claude, in claude's worktree.
	if obs.Phase != PhaseSucceeded {
		t.Fatalf("Phase = %v, want PhaseSucceeded (the claude shim requires the seat marker; diagnostic: %q)", obs.Phase, obs.Diagnostic)
	}

	// The recorded argv proves the exact invocation shape: `seat-assets --dest <workDir>`.
	seatArgs := readSeatArgs(t, seatCapture)
	if len(seatArgs) == 0 || seatArgs[0] != "seat-assets" {
		t.Fatalf("state-mcp not invoked as `seat-assets ...`; argv=%v", seatArgs)
	}
	var dest string
	for i := 0; i+1 < len(seatArgs); i++ {
		if seatArgs[i] == "--dest" {
			dest = seatArgs[i+1]
		}
	}
	if dest == "" {
		t.Fatalf("seat-assets argv missing a non-empty --dest: %v", seatArgs)
	}
	// --dest is the worktree (a throwaway dir distinct from the shared repo), and it is the
	// SAME dir the marker landed in that the shim then read — so it equals claude's cwd.
	if dest == sharedDir {
		t.Fatalf("seat-assets --dest = %q, want the throwaway worktree, not the shared repo", dest)
	}
}

// ---------------------------------------------------------------------------
// TR1-TR6 — SP1 capture seam (Task 5): stream-json + the trace tee + the
// bounded in-memory tail + the trace-sink trust rule.
//
//	TR1 tailBuffer keeps the SUFFIX of an oversized stream and never grows past
//	    tailBufferCap — the terminal `result` event (the LAST line) must survive,
//	    because that is the only part every existing stdout consumer reads.
//	TR2 claudeArgv asks for --output-format stream-json + --verbose (the event
//	    stream), never the single-object `json` format.
//	TR3 the trust rule: the resolved trace sink must sit OUTSIDE the agent-
//	    writable git dir (the sandbox allowWrite list is [workDir, gitDir]).
//	TR4 a BARE shared repo (gitDir == repoPath, so the sink WOULD be agent-
//	    writable) fails the dispatch loudly instead of tee-ing into it.
//	TR5 a dispatched run tees claude's WHOLE stdout to
//	    <repoPath>/.aiarch/traces/<episodeId>.jsonl (episodeId == the submit
//	    dedup token), alongside the self-ignoring .gitignore.
//	TR6 the durable failure log points AT the trace file instead of duplicating
//	    the full stdout; stderr is still kept verbatim.
// ---------------------------------------------------------------------------

func TestTailBufferKeepsSuffix(t *testing.T) {
	var tb tailBuffer
	big := bytes.Repeat([]byte("x"), 600*1024)
	if _, err := tb.Write(big); err != nil {
		t.Fatalf("Write(big): %v", err)
	}
	if _, err := tb.Write([]byte("TERMINAL")); err != nil {
		t.Fatalf("Write(terminal): %v", err)
	}
	s := tb.String()
	if len(s) > tailBufferCap {
		t.Fatalf("tail length = %d, want <= %d", len(s), tailBufferCap)
	}
	if !strings.HasSuffix(s, "TERMINAL") {
		t.Fatalf("tail does not end with the last write (len=%d)", len(s))
	}
	if !bytes.HasSuffix(tb.Bytes(), []byte("TERMINAL")) {
		t.Fatal("Bytes() does not end with the last write")
	}
}

func TestTailBufferUnderCapKeepsEverything(t *testing.T) {
	var tb tailBuffer
	if _, err := tb.Write([]byte("alpha\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := tb.Write([]byte("omega")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := tb.String(); got != "alpha\nomega" {
		t.Fatalf("String() = %q, want the whole stream", got)
	}
}

// A SINGLE write larger than the cap is the pathological case (one giant tool
// result): the suffix must still be kept, not the prefix.
func TestTailBufferSingleOversizedWriteKeepsSuffix(t *testing.T) {
	var tb tailBuffer
	if _, err := tb.Write(append(bytes.Repeat([]byte("y"), tailBufferCap+4096), []byte("END")...)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	s := tb.String()
	if len(s) != tailBufferCap {
		t.Fatalf("tail length = %d, want exactly %d", len(s), tailBufferCap)
	}
	if !strings.HasSuffix(s, "END") {
		t.Fatal("oversized single write kept the prefix, not the suffix")
	}
}

// TR2 — the format switch. --verbose is REQUIRED alongside stream-json in
// headless (-p) mode; without it claude refuses the combination.
func TestClaudeArgv_AsksForStreamJSONEventStream(t *testing.T) {
	args := claudeArgv("/service-construction c a", "/tmp/mcp.json", "/tmp/sandbox.json")
	mustContainAdjacentPair(t, args, "--output-format", "stream-json")
	mustContainArg(t, args, "--verbose")
	if containsArg(args, "json") {
		t.Fatalf("argv still asks for the single-object `json` output format: %v", args)
	}
}

// TR3 — the trust rule, as a pure unit over the two paths dispatch already has
// in hand (a.repoPath and addWorktree's gitDir).
func TestTraceSinkTrustRule(t *testing.T) {
	repo := t.TempDir()

	if err := assertTraceSinkOutsideGitDir(repo, filepath.Join(repo, ".git")); err != nil {
		t.Fatalf("non-bare repo rejected: %v", err)
	}
	if err := assertTraceSinkOutsideGitDir(repo, repo); err == nil {
		t.Fatal("bare repo (gitDir == repoPath) accepted; the trace sink would be agent-writable")
	}
	// A gitDir that CONTAINS the sink by any other route is rejected too (a
	// $GIT_DIR pointed at the checkout root, a worktree-style .git file target).
	if err := assertTraceSinkOutsideGitDir(repo, filepath.Join(repo, ".aiarch")); err == nil {
		t.Fatal("gitDir containing the trace sink accepted")
	}
}

// newBareOnlyRepo creates a genuinely BARE shared repo — the configuration the
// trust rule refuses (gitDir == repoPath, so <repoPath>/.aiarch/traces sits
// inside the agent-writable sandbox scope).
func newBareOnlyRepo(t *testing.T) (bareDir, url string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH; skipping local-executor proof")
	}
	root := t.TempDir()
	bare := filepath.Join(root, "remote.git")
	testGit(t, "", "init", "--bare", "--initial-branch=main", bare)
	seed := filepath.Join(root, "seed")
	testGit(t, "", "clone", bare, seed)
	testGit(t, seed, "config", "user.email", "seed@aiarch.local")
	testGit(t, seed, "config", "user.name", "seed")
	testGit(t, seed, "commit", "--allow-empty", "-m", "seed")
	testGit(t, seed, "push", "origin", "main")
	return bare, "file://" + bare
}

// TR4 — the loud failure. A bare shared repo is refused PRE-SPAWN: Submit
// errors, no claude process runs, and no run record is left behind (so a retry
// against a fixed configuration converges cleanly).
func TestLocalExecSubmit_BareSharedRepo_RefusesToDispatch(t *testing.T) {
	_, url := newBareOnlyRepo(t)
	installClaudeShim(t, "#!/bin/sh\ntouch \"$AIARCH_SHIM_RAN_MARKER\" 2>/dev/null\nexit 0\n")
	a := newLocalExecForTest(t, url, 10*time.Second)

	_, err := a.SubmitAgenticJob(subRC(context.Background(), "bare-repo-key"),
		localSpec("C-BARE", "someComponent", "service-construction"))
	if err == nil {
		t.Fatal("Submit succeeded against a BARE shared repo; the trace sink would be agent-writable")
	}
	// The message must be self-service: name the sink, and name BOTH halves of
	// the remedy — a non-bare checkout, plus the receive.denyCurrentBranch
	// setting that checkout needs before anything can push its checked-out branch.
	for _, want := range []string{"trace", "NON-bare working checkout", "receive.denyCurrentBranch updateInstead"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Submit error = %q, want it to mention %q", err.Error(), want)
		}
	}
	a.mu.Lock()
	n := len(a.runs)
	a.mu.Unlock()
	if n != 0 {
		t.Fatalf("a pre-spawn refusal left %d run record(s); a retry must start clean", n)
	}
}

// TR5 — the tee. claude's WHOLE stream-json stdout lands in the per-episode
// trace file under the shared repo, keyed by the SAME dedup token the handle is
// (no second identity), next to a self-ignoring .gitignore.
//
// TE7 (folded in rather than duplicating the whole dispatch harness): the SAME
// real dispatched run's terminal observation carries the EpisodeSummary mined
// from that trace — the end-to-end proof that the parser is actually wired into
// awaitCompletion's success path and republished by ObserveAgenticJob.
func TestLocalExecRun_TeesWholeStdoutToPerEpisodeTraceFile(t *testing.T) {
	repoDir, url := newSharedRepo(t)
	installClaudeShim(t, "#!/bin/sh\n"+
		"set -e\n"+
		"git config user.email shim@aiarch.local\n"+
		"git config user.name shim\n"+
		`echo '{"type":"system","subtype":"init","session_id":"s1","model":"claude-sonnet-5"}'`+"\n"+
		`echo '{"type":"assistant","message":{"content":[{"type":"text","text":"MIDDLE-EVENT"}],"usage":{"input_tokens":3,"output_tokens":2,"cache_read_input_tokens":100,"cache_creation_input_tokens":50}}}'`+"\n"+
		`echo '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_a","name":"Write"}],"usage":{"input_tokens":1,"output_tokens":1,"cache_read_input_tokens":10,"cache_creation_input_tokens":5}}}'`+"\n"+
		"echo work >> WORK.txt\n"+
		"git add -A\n"+
		"git commit -m 'shim work' >/dev/null\n"+
		`echo '{"type":"result","subtype":"success","is_error":false,"result":"done","num_turns":2,"total_cost_usd":0.5,"usage":{"input_tokens":5,"output_tokens":4,"cache_read_input_tokens":120,"cache_creation_input_tokens":60}}'`+"\n"+
		"exit 0\n")
	a := newLocalExecForTest(t, url, 10*time.Second)

	const key fwra.IdempotencyKey = "trace-tee-key"
	handle, err := a.SubmitAgenticJob(subRC(context.Background(), key),
		localSpec("C-TRACE", "someComponent", "service-construction"))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	obs := waitForEpisode(t, a, handle, 10*time.Second)
	if obs.Phase != PhaseSucceeded {
		t.Fatalf("Phase = %v, want PhaseSucceeded (diagnostic: %q)", obs.Phase, obs.Diagnostic)
	}

	tracesDir := filepath.Join(repoDir, ".aiarch", "traces")
	tracePath := filepath.Join(tracesDir, dedupToken(key)+".jsonl")
	raw, err := os.ReadFile(tracePath) //nolint:gosec // test-owned temp path
	if err != nil {
		t.Fatalf("read per-episode trace: %v", err)
	}
	for _, want := range []string{`"subtype":"init"`, "MIDDLE-EVENT", `"type":"result"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("trace file missing %q; got:\n%s", want, raw)
		}
	}
	// The terminal result event is the LAST line — the invariant every stdout
	// consumer (envelopeDetail's end-scan) and Task 6's miner depend on.
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if !strings.Contains(lines[len(lines)-1], `"type":"result"`) {
		t.Fatalf("last trace line is not the result event: %q", lines[len(lines)-1])
	}
	// Self-ignoring, so an operated repo needs no scaffold change.
	assertFileContains(t, filepath.Join(tracesDir, ".gitignore"), "*")

	assertDispatchedEpisode(t, obs.Episode, dedupToken(key), tracePath)
}

// assertDispatchedEpisode is TE7: the summary mined from the trace file TR5 just
// asserted, as reported on the terminal observation. The id is the trace's own
// name (one identity, no second spelling), TracePath points at that artifact,
// BOTH token totals are present and DIFFER (proving neither is derived from the
// other), and the wall-clock bounds come from the dispatch rather than the
// stream — the TR5 shim emits no per-event timestamps, so the parser's fallback
// must not have been used.
func assertDispatchedEpisode(t *testing.T, ep *EpisodeSummary, wantID, wantTracePath string) {
	t.Helper()
	if ep.EpisodeID != wantID {
		t.Fatalf("Episode.EpisodeID = %q, want the dedup token %q", ep.EpisodeID, wantID)
	}
	if ep.TracePath == nil || *ep.TracePath != wantTracePath {
		t.Fatalf("Episode.TracePath = %v, want %q", ep.TracePath, wantTracePath)
	}
	if ep.Outcome != EpisodeSucceeded {
		t.Fatalf("Episode.Outcome = %v, want EpisodeSucceeded", ep.Outcome)
	}
	assertModel(t, *ep, "claude-sonnet-5")
	assertTurnsAndCost(t, *ep, 2, 0.5)
	assertUsage(t, "Episode.Usage (terminal event)", ep.Usage, EpisodeUsage{In: 5, Out: 4, CacheRead: 120, CacheCreate: 60})
	assertStreamedUsage(t, *ep, EpisodeUsage{In: 4, Out: 3, CacheRead: 110, CacheCreate: 55})
	assertSoleToolCall(t, ep.ToolCallCounts, "Write", 1)
	if ep.StartedAt.IsZero() || ep.EndedAt.IsZero() || ep.EndedAt.Before(ep.StartedAt) {
		t.Fatalf("Episode wall-clock bounds %v..%v are not a real dispatch interval", ep.StartedAt, ep.EndedAt)
	}
}

// TR6 is folded into LX-OBS3 above (the durable-log test): the log dir now
// RECORDS WHERE the trace is instead of duplicating the full stdout.

// TR7 — the format switch against REAL captured output (Task 1 fixtures,
// testdata/streamjson/*.jsonl: genuine `claude --output-format stream-json
// --verbose -p ...` runs). The terminal `result` event is the LAST line and
// still carries subtype/is_error/result, so envelopeDetail's end-of-text scan
// finds it unchanged — AND still finds it after the stream has overflowed the
// bounded tail, which is the whole reason the tail keeps the SUFFIX.
func TestClaudeOutputDetail_RealStreamJSONFixtures(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		want    string
	}{
		{"failure.jsonl", "no-such-model"},
		{"success_with_tools.jsonl", "hello.t"},
		{"success_with_subagent.jsonl", "success"},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", "streamjson", tc.fixture)) //nolint:gosec // fixed test fixture path
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			got := claudeOutputDetail(string(raw), "")
			if !strings.Contains(got, tc.want) {
				t.Fatalf("claudeOutputDetail = %q, want it to mention %q (the terminal result event)", got, tc.want)
			}

			// Same fixture, but preceded by enough noise to overflow the tail.
			var tb tailBuffer
			if _, err := tb.Write(bytes.Repeat([]byte("{\"type\":\"assistant\",\"pad\":\"noise\"}\n"), 40000)); err != nil {
				t.Fatalf("Write(pad): %v", err)
			}
			if _, err := tb.Write(raw); err != nil {
				t.Fatalf("Write(fixture): %v", err)
			}
			if len(tb.String()) != tailBufferCap {
				t.Fatalf("tail length = %d, want the stream to have overflowed to exactly %d", len(tb.String()), tailBufferCap)
			}
			if got := claudeOutputDetail(tb.String(), ""); !strings.Contains(got, tc.want) {
				t.Fatalf("after tail overflow claudeOutputDetail = %q, want it to still mention %q", got, tc.want)
			}
		})
	}
}

// TR8 — a trace-file write fault must never reach os/exec's stdout copier. That
// copier is `io.Copy(w, pr); pr.Close()`, so a returned error would close the
// read end and claude would take EPIPE on its next write and DIE part-way
// through the run. traceSink absorbs the fault (recorded + logged, never
// returned) so the pipe keeps draining and the run reaches its real outcome.
func TestTraceSinkAbsorbsWriteFaults(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "trace-*.jsonl")
	if err != nil {
		t.Fatalf("create temp trace: %v", err)
	}
	path := f.Name()
	if err := f.Close(); err != nil { // every later write now fails
		t.Fatalf("close: %v", err)
	}
	s := &traceSink{file: f, path: path}
	n, err := s.Write([]byte("first\n"))
	if err != nil || n != len("first\n") {
		t.Fatalf("Write = (%d, %v), want (%d, nil) — a fault must not reach the copier", n, err, len("first\n"))
	}
	if s.err == nil {
		t.Fatal("traceSink did not record the underlying write fault")
	}
	n, err = s.Write([]byte("second\n"))
	if err != nil || n != len("second\n") {
		t.Fatalf("Write after fault = (%d, %v), want (%d, nil)", n, err, len("second\n"))
	}
}

// failingTraceFile is a traceWriteCloser whose every Write fails — the
// injectable stand-in for a full disk. Injected through
// localExecAccess.openTrace, the package's test-only trace-open seam, because a
// mid-run write fault cannot be provoked portably against a real file (an
// already-open fd stays writable through chmod).
type failingTraceFile struct{}

func (failingTraceFile) Write([]byte) (int, error) {
	return 0, errors.New("no space left on device (simulated)")
}
func (failingTraceFile) Sync() error  { return nil }
func (failingTraceFile) Close() error { return nil }

// TR9 — a trace that could not be written in full must not be silently passed
// off as complete. The run still reaches its real terminal outcome (the write
// fault never reaches os/exec's copier, so claude is not killed by EPIPE), AND
// the incompleteness is recorded on the run record — where Task 6 reads it —
// rather than living only in a log line, because trace_path.txt and the episode
// summary both point at that file as if it were whole.
func TestLocalExecRun_TraceWriteFault_RunCompletesAndTruncationIsRecorded(t *testing.T) {
	for _, tc := range []struct {
		name        string
		installShim func(t *testing.T)
		key         fwra.IdempotencyKey
		activityID  string
		wantPhase   PipelinePhase
		wantNotice  bool
	}{
		{
			// The success path is the dangerous one: nothing else would ever
			// record the truncation, since no diagnostic is produced at all.
			name:        "successful run",
			installShim: func(t *testing.T) { commitShim(t, filepath.Join(t.TempDir(), "capture")) },
			key:         "trace-fault-ok-key",
			activityID:  "C-TRFAULTOK",
			wantPhase:   PhaseSucceeded,
			wantNotice:  false,
		},
		{
			name: "failing run",
			installShim: func(t *testing.T) {
				installClaudeShim(t, "#!/bin/sh\n"+
					`echo '{"type":"result","subtype":"success","is_error":false,"result":"nothing to do"}'`+"\n"+
					"exit 0\n")
			},
			key:        "trace-fault-fail-key",
			activityID: "C-TRFAULTFAIL",
			wantPhase:  PhaseFailed,
			wantNotice: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, url := newSharedRepo(t)
			tc.installShim(t)
			a := newLocalExecForTest(t, url, 10*time.Second)
			a.openTrace = func(string) (traceWriteCloser, error) { return failingTraceFile{}, nil }

			handle, err := a.SubmitAgenticJob(subRC(context.Background(), tc.key),
				localSpec(tc.activityID, "someComponent", "service-construction"))
			if err != nil {
				t.Fatalf("Submit: %v", err)
			}
			obs := waitForTerminal(t, a, handle, 10*time.Second)

			// The run reached its REAL outcome — the trace fault did not kill it.
			if obs.Phase != tc.wantPhase {
				t.Fatalf("Phase = %v, want %v (diagnostic: %q)", obs.Phase, tc.wantPhase, obs.Diagnostic)
			}

			a.mu.Lock()
			run := a.runs[dedupToken(tc.key)]
			a.mu.Unlock()
			if run == nil {
				t.Fatal("no run record for the dispatched episode")
			}
			run.mu.Lock()
			truncated, reason := run.traceTruncated, run.traceTruncationReason
			run.mu.Unlock()

			if !truncated {
				t.Fatal("run record does not mark the trace as truncated; a partial trace would read as complete")
			}
			if !strings.Contains(reason, "no space left on device") {
				t.Fatalf("traceTruncationReason = %q, want the underlying write fault", reason)
			}

			// The failure panel says so too; a successful run keeps an empty
			// diagnostic (the run-record flag carries the fact there).
			if got := strings.Contains(obs.Diagnostic, localExecTraceTruncatedNotice); got != tc.wantNotice {
				t.Fatalf("diagnostic carries the truncation notice = %v, want %v (diagnostic: %q)", got, tc.wantNotice, obs.Diagnostic)
			}
			if !tc.wantNotice && obs.Diagnostic != "" {
				t.Fatalf("Diagnostic = %q, want empty on the success path", obs.Diagnostic)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// SP1 CAPTURE SEAM — TE-series: the episode stream parser + the summary the
// terminal observation reports.
//
// Every expected number below is PINNED from the real Task-1 fixtures (read once
// with jq, hard-coded here). They are not floors: a change in what the parser
// counts must break these tests loudly rather than pass a ">= 1" check.
// ---------------------------------------------------------------------------

// fixtureTimes are the two arbitrary wall-clock bounds the parser is handed when
// the caller knows them (the dispatch/completion instants). They must be
// reported verbatim — the parser never second-guesses a caller-supplied bound.
var (
	fixtureStarted = time.Date(2026, 8, 2, 20, 0, 0, 0, time.UTC)
	fixtureEnded   = time.Date(2026, 8, 2, 21, 0, 0, 0, time.UTC)
)

func openFixture(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "streamjson", name)) //nolint:gosec // fixed test fixture path
	if err != nil {
		t.Fatalf("open fixture %s: %v", name, err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "streamjson", name)) //nolint:gosec // fixed test fixture path
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(raw)
}

func assertUsage(t *testing.T, label string, got, want EpisodeUsage) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %+v, want %+v", label, got, want)
	}
}

func assertModel(t *testing.T, sum EpisodeSummary, want string) {
	t.Helper()
	if sum.Model == nil || *sum.Model != want {
		t.Fatalf("Model = %v, want %q", sum.Model, want)
	}
}

func assertTurnsAndCost(t *testing.T, sum EpisodeSummary, wantTurns int64, wantCost float64) {
	t.Helper()
	if sum.NumTurns == nil || *sum.NumTurns != wantTurns {
		t.Fatalf("NumTurns = %v, want %d", sum.NumTurns, wantTurns)
	}
	if sum.CostUSD == nil || *sum.CostUSD != wantCost {
		t.Fatalf("CostUSD = %v, want %v", sum.CostUSD, wantCost)
	}
}

func assertStreamedUsage(t *testing.T, sum EpisodeSummary, want EpisodeUsage) {
	t.Helper()
	if sum.StreamedUsage == nil {
		t.Fatal("StreamedUsage is nil; the per-turn assistant-event sum must be recorded too")
	}
	assertUsage(t, "StreamedUsage (summed assistant events)", *sum.StreamedUsage, want)
}

// assertSoleToolCall pins the tool-call histogram exactly: one tool, called
// exactly n times, and nothing else counted.
func assertSoleToolCall(t *testing.T, counts map[string]int64, name string, n int64) {
	t.Helper()
	if len(counts) != 1 || counts[name] != n {
		t.Fatalf("ToolCallCounts = %v, want exactly {%s:%d}", counts, name, n)
	}
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return ts
}

// TE1 — success_with_tools.jsonl: one executed tool (Write, exactly once), a
// terminal result event carrying the authoritative usage/cost/turns, and BOTH
// token totals recorded (they differ: the terminal total is the API's own, the
// streamed sum is per-turn — never assert equality).
func TestParseEpisodeStreamSuccess(t *testing.T) {
	sum, gapReason, err := parseEpisodeStream(openFixture(t, "success_with_tools.jsonl"),
		"ep-1", "trace/ep-1.jsonl", fixtureStarted, fixtureEnded)
	if err != nil {
		t.Fatalf("parseEpisodeStream: %v", err)
	}
	if gapReason != "" {
		t.Fatalf("gapReason = %q, want empty (the fixture HAS a terminal result event)", gapReason)
	}
	if sum.EpisodeID != "ep-1" {
		t.Fatalf("EpisodeID = %q, want ep-1", sum.EpisodeID)
	}
	if sum.Outcome != EpisodeSucceeded {
		t.Fatalf("Outcome = %v, want EpisodeSucceeded", sum.Outcome)
	}
	assertModel(t, sum, "claude-sonnet-5") // the init event's model
	assertTurnsAndCost(t, sum, 2, 0.054201000000000006)
	assertUsage(t, "Usage (terminal result event)", sum.Usage,
		EpisodeUsage{In: 4, Out: 171, CacheRead: 63600, CacheCreate: 5424})
	assertStreamedUsage(t, sum, EpisodeUsage{In: 6, Out: 3, CacheRead: 92778, CacheCreate: 10668})
	assertSoleToolCall(t, sum.ToolCallCounts, "Write", 1)
	if len(sum.SubagentSpans) != 0 {
		t.Fatalf("SubagentSpans = %v, want none (this fixture spawned no subagent)", sum.SubagentSpans)
	}
	if !sum.StartedAt.Equal(fixtureStarted) || !sum.EndedAt.Equal(fixtureEnded) {
		t.Fatalf("StartedAt/EndedAt = %v/%v, want the caller-supplied bounds %v/%v",
			sum.StartedAt, sum.EndedAt, fixtureStarted, fixtureEnded)
	}
	if sum.TracePath == nil || *sum.TracePath != "trace/ep-1.jsonl" {
		t.Fatalf("TracePath = %v, want trace/ep-1.jsonl", sum.TracePath)
	}
}

// TE2 — success_with_subagent.jsonl: the main loop called Agent once; the ONE
// event carrying parent_tool_use_id is attributed to that span and NOT to the
// main-loop tool counts. Both usage totals are recorded and are ALLOWED to
// diverge (a subagent's own turns are billed into the terminal total).
func TestParseEpisodeStreamSubagent(t *testing.T) {
	sum, gapReason, err := parseEpisodeStream(openFixture(t, "success_with_subagent.jsonl"),
		"ep-2", "trace/ep-2.jsonl", fixtureStarted, fixtureEnded)
	if err != nil {
		t.Fatalf("parseEpisodeStream: %v", err)
	}
	if gapReason != "" {
		t.Fatalf("gapReason = %q, want empty", gapReason)
	}
	if sum.Outcome != EpisodeSucceeded {
		t.Fatalf("Outcome = %v, want EpisodeSucceeded", sum.Outcome)
	}
	// The subagent tool in this real capture is "Agent", not "Task".
	assertSoleToolCall(t, sum.ToolCallCounts, "Agent", 1)
	if len(sum.SubagentSpans) != 1 {
		t.Fatalf("SubagentSpans = %v, want exactly 1", sum.SubagentSpans)
	}
	span := sum.SubagentSpans[0]
	if span.ToolUseID != "toolu_01DL6Gqvj23os5rsNRhFSTmy" {
		t.Fatalf("span.ToolUseID = %q, want the Agent tool_use id", span.ToolUseID)
	}
	// The fixture carries exactly ONE parented event, so the span's first and
	// last timestamps are the same instant — both are real, neither is zero.
	want := mustTime(t, "2026-08-02T20:33:05.433Z")
	if span.StartedAt == nil || !span.StartedAt.Equal(want) {
		t.Fatalf("span.StartedAt = %v, want %v", span.StartedAt, want)
	}
	if span.EndedAt == nil || !span.EndedAt.Equal(want) {
		t.Fatalf("span.EndedAt = %v, want %v", span.EndedAt, want)
	}
	assertUsage(t, "Usage (terminal result event)", sum.Usage,
		EpisodeUsage{In: 4, Out: 266, CacheRead: 63944, CacheCreate: 5941})
	assertStreamedUsage(t, sum, EpisodeUsage{In: 6, Out: 5, CacheRead: 93122, CacheCreate: 11529})
	// Both totals are non-zero and DIVERGE. Recorded as two facts, never reconciled.
	if sum.Usage == *sum.StreamedUsage {
		t.Fatal("the fixture's two totals happen to be equal; the test's premise (they may diverge) is stale")
	}
}

// TE3 — failure.jsonl. THE CLI QUIRK: the terminal event is subtype "success"
// with is_error true. Outcome keys on is_error, so this is a FAILED episode; a
// subtype-only reading would have called it a success.
func TestParseEpisodeStreamFailure(t *testing.T) {
	sum, gapReason, err := parseEpisodeStream(openFixture(t, "failure.jsonl"),
		"ep-3", "trace/ep-3.jsonl", fixtureStarted, fixtureEnded)
	if err != nil {
		t.Fatalf("parseEpisodeStream: %v", err)
	}
	if gapReason != "" {
		t.Fatalf("gapReason = %q, want empty (a terminal result event IS present)", gapReason)
	}
	if sum.Outcome != EpisodeFailed {
		t.Fatalf(`Outcome = %v, want EpisodeFailed — the terminal event is subtype "success" with is_error true`, sum.Outcome)
	}
	// The init event's model, NOT the "<synthetic>" model on the assistant turn.
	assertModel(t, sum, "no-such-model")
	assertTurnsAndCost(t, sum, 1, 0)
	assertUsage(t, "Usage", sum.Usage, EpisodeUsage{})
	// A zero-usage assistant turn is still a RECORDED total, not an absent one.
	assertStreamedUsage(t, sum, EpisodeUsage{})
	if len(sum.ToolCallCounts) != 0 {
		t.Fatalf("ToolCallCounts = %v, want none", sum.ToolCallCounts)
	}
}

// TE4 — the stream ends without its terminal result event (a killed subprocess,
// a truncated trace). That is a GAP, not a success and not a failure: the parser
// cannot say how the episode ended, and says so instead of guessing.
func TestParseEpisodeStreamNoTerminal(t *testing.T) {
	full := readFixture(t, "success_with_tools.jsonl")
	lines := strings.Split(strings.TrimRight(full, "\n"), "\n")
	truncated := strings.Join(lines[:len(lines)-1], "\n") + "\n"

	sum, gapReason, err := parseEpisodeStream(strings.NewReader(truncated),
		"ep-4", "trace/ep-4.jsonl", fixtureStarted, fixtureEnded)
	if err != nil {
		t.Fatalf("parseEpisodeStream: %v", err)
	}
	if sum.Outcome != EpisodeGap {
		t.Fatalf("Outcome = %v, want EpisodeGap", sum.Outcome)
	}
	if gapReason == "" {
		t.Fatal("a gap outcome must carry a reason explaining what is missing")
	}
	if !strings.Contains(gapReason, "terminal") {
		t.Fatalf("gapReason = %q, want it to name the missing terminal result event", gapReason)
	}
	// Everything observed BEFORE the gap is still reported — a partial trace is
	// partial evidence, not no evidence.
	if sum.ToolCallCounts["Write"] != 1 {
		t.Fatalf("ToolCallCounts = %v, want the pre-gap Write call still counted", sum.ToolCallCounts)
	}
	if sum.StreamedUsage == nil || *sum.StreamedUsage == (EpisodeUsage{}) {
		t.Fatalf("StreamedUsage = %v, want the pre-gap per-turn sum", sum.StreamedUsage)
	}
	// The terminal-only fields stay unset rather than being invented.
	if sum.NumTurns != nil || sum.CostUSD != nil {
		t.Fatalf("NumTurns/CostUSD = %v/%v, want both nil (only the terminal event carries them)", sum.NumTurns, sum.CostUSD)
	}
	assertUsage(t, "Usage", sum.Usage, EpisodeUsage{})
}

// TE5 — a stream-json trace is a LOG, not a document: a crashed CLI, a stray
// warning on stdout, or a half-written last line must never cost us the events
// that DID parse. Non-JSON lines are skipped; the parse still succeeds.
func TestParseEpisodeStreamGarbage(t *testing.T) {
	full := readFixture(t, "success_with_tools.jsonl")
	lines := strings.Split(strings.TrimRight(full, "\n"), "\n")
	var b strings.Builder
	b.WriteString("node:internal/process/promises warning\n")
	for _, ln := range lines {
		b.WriteString(ln)
		b.WriteString("\n")
		b.WriteString("\n")                                     // blank lines
		b.WriteString("not json at all\n")                      // plain noise
		b.WriteString(`{"type":"assistant","message":{` + "\n") // a half-written JSON line
	}

	sum, gapReason, err := parseEpisodeStream(strings.NewReader(b.String()),
		"ep-5", "trace/ep-5.jsonl", fixtureStarted, fixtureEnded)
	if err != nil {
		t.Fatalf("parseEpisodeStream: %v — interleaved garbage must never be fatal", err)
	}
	if gapReason != "" {
		t.Fatalf("gapReason = %q, want empty", gapReason)
	}
	if sum.Outcome != EpisodeSucceeded {
		t.Fatalf("Outcome = %v, want EpisodeSucceeded (the good lines still parsed)", sum.Outcome)
	}
	assertSoleToolCall(t, sum.ToolCallCounts, "Write", 1)
	assertUsage(t, "Usage", sum.Usage, EpisodeUsage{In: 4, Out: 171, CacheRead: 63600, CacheCreate: 5424})
}

// TE6 — with no caller-supplied bounds the parser falls back to the stream's OWN
// first/last event timestamps. This is what the restart-lost path has: a trace
// on disk and no run record to date it.
func TestParseEpisodeStreamDerivesBoundsFromTheStream(t *testing.T) {
	sum, _, err := parseEpisodeStream(openFixture(t, "success_with_tools.jsonl"),
		"ep-6", "", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("parseEpisodeStream: %v", err)
	}
	wantStart := mustTime(t, "2026-08-02T20:28:51.269Z")
	wantEnd := mustTime(t, "2026-08-02T20:28:54.729Z")
	if !sum.StartedAt.Equal(wantStart) {
		t.Fatalf("StartedAt = %v, want the first timestamped event %v", sum.StartedAt, wantStart)
	}
	if !sum.EndedAt.Equal(wantEnd) {
		t.Fatalf("EndedAt = %v, want the last timestamped event %v", sum.EndedAt, wantEnd)
	}
	if sum.TracePath != nil {
		t.Fatalf("TracePath = %v, want nil when no path is known", sum.TracePath)
	}
}

// TE8 — the CANCEL arm. awaitCompletion's already-cancelled short-circuit must
// still close the trace, mine it, and publish a summary whose Outcome is
// CANCELLED — overriding whatever the (necessarily partial) stream says. The
// sleeping shim writes nothing at all, so this is also the empty-trace case: a
// summary is still published, because "we ran an agent and it produced no
// evidence" is itself the fact the capture seam records.
func TestLocalExecCancel_TerminalObservationCarriesCancelledEpisode(t *testing.T) {
	_, url := newSharedRepo(t)
	installClaudeShim(t, "#!/bin/sh\nexec sleep 30\n")
	a := newLocalExecForTest(t, url, 20*time.Second)

	const key fwra.IdempotencyKey = "episode-cancel-key"
	handle, err := a.SubmitAgenticJob(subRC(context.Background(), key),
		localSpec("C-EPCANCEL", "someComponent", "service-construction"))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := a.CancelAgenticJob(obsRC(context.Background()), handle); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	obs := waitForEpisode(t, a, handle, 10*time.Second)
	if obs.Phase != PhaseCancelled {
		t.Fatalf("Phase = %v, want PhaseCancelled", obs.Phase)
	}
	if obs.Episode.Outcome != EpisodeCancelled {
		t.Fatalf("Episode.Outcome = %v, want EpisodeCancelled (the operator's intent overrides the stream)", obs.Episode.Outcome)
	}
	if obs.Episode.EpisodeID != dedupToken(key) {
		t.Fatalf("Episode.EpisodeID = %q, want the dedup token %q", obs.Episode.EpisodeID, dedupToken(key))
	}
}

// TE9 — the RESTART-LOST arm. The run record is gone, so the RA cannot attest
// the outcome — but the trace it wrote is still on disk and is real evidence.
// The observation reports a GAP episode recovered from the handle's own token,
// alongside the existing lost-run diagnostic (which IS the gap reason on this
// path — EpisodeSummary carries no reason field of its own).
func TestLocalExecObserve_RestartLostRun_ReportsGapEpisodeFromTheOrphanedTrace(t *testing.T) {
	repoDir, url := newSharedRepo(t)
	a := newLocalExecForTest(t, url, 0)

	// Simulate the restart: a trace file exists for a token whose run record does not.
	const token = "aaaabbbbccccddddeeeeffff00001111"
	tracesDir := filepath.Join(repoDir, ".aiarch", "traces")
	if err := os.MkdirAll(tracesDir, 0o750); err != nil {
		t.Fatalf("mkdir traces: %v", err)
	}
	partial := readFixture(t, "success_with_tools.jsonl")
	if err := os.WriteFile(filepath.Join(tracesDir, token+".jsonl"), []byte(partial), 0o600); err != nil {
		t.Fatalf("write orphaned trace: %v", err)
	}

	obs, err := a.ObserveAgenticJob(obsRC(context.Background()), PipelineHandle(localHandlePrefix+token))
	if err != nil {
		t.Fatalf("Observe(lost): %v", err)
	}
	if obs.Phase != PhaseFailed || obs.Diagnostic != localRunLostDiagnostic {
		t.Fatalf("Phase/Diagnostic = %v/%q, want the unchanged lost-run terminal failure", obs.Phase, obs.Diagnostic)
	}
	if obs.Episode == nil {
		t.Fatal("a lost run with an orphaned trace must still report the episode it can recover")
	}
	if obs.Episode.EpisodeID != token {
		t.Fatalf("Episode.EpisodeID = %q, want the token recovered from the handle", obs.Episode.EpisodeID)
	}
	// GAP even though the trace HAS a terminal result event: the run record is
	// gone, so the RA never verified its own post-conditions (ref advanced,
	// worktree removed) and must not report a success it cannot attest.
	if obs.Episode.Outcome != EpisodeGap {
		t.Fatalf("Episode.Outcome = %v, want EpisodeGap for an unrecoverable run", obs.Episode.Outcome)
	}
	// The recovered evidence is still reported.
	if obs.Episode.ToolCallCounts["Write"] != 1 {
		t.Fatalf("Episode.ToolCallCounts = %v, want the orphaned trace's evidence", obs.Episode.ToolCallCounts)
	}
}

// TE10 — a lost handle with NO trace on disk reports NO episode. This is what
// keeps the local MERGE job (a handle + dedup token, no agent, no trace) from
// ever being handed a fabricated episode.
func TestLocalExecObserve_RestartLostRun_NoTraceReportsNoEpisode(t *testing.T) {
	_, url := newSharedRepo(t)
	a := newLocalExecForTest(t, url, 0)
	obs, err := a.ObserveAgenticJob(obsRC(context.Background()), "local:deadbeef")
	if err != nil {
		t.Fatalf("Observe(lost): %v", err)
	}
	if obs.Episode != nil {
		t.Fatalf("Episode = %+v, want nil — nothing ran, so there is nothing to summarise", obs.Episode)
	}
}

// TE11 — the local MERGE job runs no agent: it has a handle and a dedup token
// but never spawns claude and never opens a trace. Its terminal observation must
// carry NO episode rather than an empty one.
func TestLocalExecMergeJob_TerminalObservationCarriesNoEpisode(t *testing.T) {
	sharedDir, url := newSharedRepo(t)
	seedActivityBranch(t, sharedDir, "C-EPMERGE", "work.txt", "branch content\n")
	a := newLocalExecForTest(t, url, 0)

	handle, err := a.SubmitAgenticJob(subRC(context.Background(), "episode-merge-key"), mergeJobSpec("C-EPMERGE"))
	if err != nil {
		t.Fatalf("Submit(merge): %v", err)
	}
	obs, err := a.ObserveAgenticJob(obsRC(context.Background()), handle)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if obs.Phase != PhaseSucceeded {
		t.Fatalf("Phase = %v, want PhaseSucceeded (diagnostic: %q)", obs.Phase, obs.Diagnostic)
	}
	if obs.Episode != nil {
		t.Fatalf("Episode = %+v, want nil — a merge job is not an agentic episode", obs.Episode)
	}
}

// TE12 — a FAILED dispatched run still reports its episode: the failure panel's
// diagnostic and the episode summary are independent facts, and a failed run's
// token spend is exactly what the capture seam exists to account for.
func TestLocalExecObserve_FailedRun_TerminalObservationCarriesFailedEpisode(t *testing.T) {
	_, url := newSharedRepo(t)
	installClaudeShim(t, "#!/bin/sh\n"+
		`echo '{"type":"system","subtype":"init","model":"claude-sonnet-5"}'`+"\n"+
		`echo '{"type":"assistant","message":{"model":"claude-sonnet-5","usage":{"input_tokens":7,"output_tokens":3,"cache_read_input_tokens":11,"cache_creation_input_tokens":5},"content":[{"type":"tool_use","id":"toolu_x","name":"Bash"}]}}'`+"\n"+
		`echo '{"type":"result","subtype":"success","is_error":true,"num_turns":3,"total_cost_usd":0.25,"result":"boom","usage":{"input_tokens":9,"output_tokens":4,"cache_read_input_tokens":13,"cache_creation_input_tokens":6}}'`+"\n"+
		"exit 1\n")
	a := newLocalExecForTest(t, url, 10*time.Second)

	handle, err := a.SubmitAgenticJob(subRC(context.Background(), "episode-fail-key"),
		localSpec("C-EPFAIL", "someComponent", "service-construction"))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	obs := waitForEpisode(t, a, handle, 10*time.Second)
	if obs.Phase != PhaseFailed {
		t.Fatalf("Phase = %v, want PhaseFailed", obs.Phase)
	}
	if obs.Diagnostic == "" {
		t.Fatal("a failed run must keep its diagnostic")
	}
	if obs.Episode.Outcome != EpisodeFailed {
		t.Fatalf("Episode.Outcome = %v, want EpisodeFailed", obs.Episode.Outcome)
	}
	assertUsage(t, "Episode.Usage", obs.Episode.Usage, EpisodeUsage{In: 9, Out: 4, CacheRead: 13, CacheCreate: 6})
	if obs.Episode.ToolCallCounts["Bash"] != 1 {
		t.Fatalf("Episode.ToolCallCounts = %v, want {Bash:1}", obs.Episode.ToolCallCounts)
	}
}

// waitForEpisode polls to a terminal observation that CARRIES its episode
// summary — the whole point of the capture seam on the local arm.
//
// It polls rather than reading the first terminal observation because on the
// CANCEL arm the two facts land at different moments: CancelAgenticJob records
// PhaseCancelled synchronously, while the episode can only be mined once the
// SIGTERM'd subprocess has actually unwound and awaitCompletion has closed the
// trace. Same reason TestLocalExecCancel_Running_ConvergesToCancelledNeverFailed
// polls for the worktree removal instead of asserting it instantaneously.
func waitForEpisode(t *testing.T, a *localExecAccess, handle PipelineHandle, timeout time.Duration) PipelineObservation {
	t.Helper()
	obs := waitForTerminal(t, a, handle, timeout)
	deadline := time.Now().Add(timeout)
	for obs.Episode == nil {
		if time.Now().After(deadline) {
			t.Fatalf("terminal observation (phase %v) never carried an Episode summary within %s", obs.Phase, timeout)
		}
		time.Sleep(50 * time.Millisecond)
		var err error
		obs, err = a.ObserveAgenticJob(obsRC(context.Background()), handle)
		if err != nil {
			t.Fatalf("Observe: %v", err)
		}
	}
	return obs
}
