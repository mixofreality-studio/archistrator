package constructionpipeline

// SERVICE TEST PLAN (STP) — constructionPipelineAccess (C-CP-R rework, GitHub
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

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// SERVICE TEST PLAN (STP) — the LOCAL-EXECUTOR realisation (the localexec
// section of constructionpipelineaccess.go). Per [[the-method-testing]], black-box
// at the RA's public verbs. The only external boundary faked is the `claude`
// binary itself (a PATH shim script, the SAME pattern
// framework-go-infrastructure-llm/claudecli_test.go uses for the SAME reason: no
// real subscription touched in CI); every git operation runs against a REAL
// throwaway local repo via the actual `git` binary — no fake for that seam.
//
//   PRE-CONDITION / CONTRACT-MISUSE:
//     L1  New rejects empty repoURL / empty stateMCPBin / a stateMCPBin that
//         does not exist on disk
//     L2  Submit rejects an empty idempotencyKey
//     L3  Submit rejects a spec with no steps (validateSpec, shared with the
//         GitHub-Actions realisation above)
//     L4  Submit rejects an empty ActivityID
//     L5  Submit rejects a spec with no DispatchInputs["command"]
//     L6  Observe / Cancel reject a zero / malformed PipelineHandle
//     L7  Observe an unknown handle → fwra.NotFound
//
//   HAPPY-PATH / DISPATCH SHAPE:
//     LH1 Submit spawns claude with --dangerously-skip-permissions, --mcp-config,
//         --output-format json, -p "/<command> <component> <activity>", cwd on a
//         FRESH CLONE of the repo checked out onto activity/<id>; the --mcp-config
//         file carries the exact AIARCH_* envelope (project/component/activity/
//         branch/state-root) pointing at cmd/aiarch-state-mcp
//     LH2 On a clean claude exit, the shim's commit is pushed to activity/<id> on
//         the ORIGIN repo and Observe reports PhaseSucceeded
//     LH3 A SECOND phase dispatch for the SAME activity (different idempotencyKey)
//         re-attaches to the EXISTING activity/<id> branch — both commits present,
//         nothing lost
//
//   EXIT-CODE MAPPING (Step 3):
//     LF1 Non-zero claude exit → PhaseFailed, diagnostic names the exit code
//     LF2 claude exceeds the run timeout → PhaseFailed, "timed out" diagnostic,
//         and the subprocess is actually gone (no orphan)
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

// subRC builds the ResourceAccess call Context for a SubmitConstructionPipeline call —
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

	if _, err := a.SubmitConstructionPipeline(subRC(ctx, ""), goodSpec()); kind(err) != fwra.ContractMisuse {
		t.Fatalf("empty key kind = %v", kind(err))
	}
	noSteps := goodSpec()
	noSteps.Steps = nil
	if _, err := a.SubmitConstructionPipeline(subRC(ctx, "k"), noSteps); kind(err) != fwra.ContractMisuse {
		t.Fatalf("no steps kind = %v", kind(err))
	}
	dup := goodSpec()
	dup.Steps = []PipelineStep{{Name: "x"}, {Name: "x"}}
	if _, err := a.SubmitConstructionPipeline(subRC(ctx, "k"), dup); kind(err) != fwra.ContractMisuse {
		t.Fatalf("dup step kind = %v", kind(err))
	}
	empty := goodSpec()
	empty.Steps = []PipelineStep{{Name: "  "}}
	if _, err := a.SubmitConstructionPipeline(subRC(ctx, "k"), empty); kind(err) != fwra.ContractMisuse {
		t.Fatalf("empty step name kind = %v", kind(err))
	}
	dangling := goodSpec()
	dangling.Edges = []StepDependency{{From: "build", To: "nope"}}
	if _, err := a.SubmitConstructionPipeline(subRC(ctx, "k"), dangling); kind(err) != fwra.ContractMisuse {
		t.Fatalf("dangling edge kind = %v", kind(err))
	}
}

func TestObserveCancelHandleMisuse(t *testing.T) {
	a := newAccessForTest(t, newFakeActions())
	ctx := context.Background()
	if _, err := a.ObserveConstructionPipeline(obsRC(ctx), PipelineHandle("")); kind(err) != fwra.ContractMisuse {
		t.Fatalf("zero handle observe kind = %v", kind(err))
	}
	if err := a.CancelConstructionPipeline(obsRC(ctx), PipelineHandle("")); kind(err) != fwra.ContractMisuse {
		t.Fatalf("zero handle cancel kind = %v", kind(err))
	}
	bad := ParsePipelineHandle("garbage-no-slash")
	if _, err := a.ObserveConstructionPipeline(obsRC(ctx), bad); kind(err) != fwra.ContractMisuse {
		t.Fatalf("malformed handle observe kind = %v", kind(err))
	}
}

func TestSubmitHappyPath(t *testing.T) {
	f := newFakeActions()
	a := newAccessForTest(t, f)
	h, err := a.SubmitConstructionPipeline(subRC(context.Background(), "key-1"), goodSpec())
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
		if _, err := a.SubmitConstructionPipeline(subRC(context.Background(), "key-di"), spec); err != nil {
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
		if _, err := a.SubmitConstructionPipeline(subRC(context.Background(), "key-spoof"), spec); err != nil {
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
		if _, err := a.SubmitConstructionPipeline(subRC(context.Background(), "key-nil"), spec); err != nil {
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

		h, err := a.SubmitConstructionPipeline(subRC(context.Background(), "key-pp"), spec)
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
		h, err := a.SubmitConstructionPipeline(subRC(context.Background(), "key-uc3"), goodSpec())
		if err != nil {
			t.Fatalf("submit: %v", err)
		}
		if !f.lastDispatchTarget.isZero() {
			t.Fatalf("UC3 dispatch target = %+v, want zero (construction-repo default)", f.lastDispatchTarget)
		}
		if PipelineHandleString(h) != "run/1" {
			t.Fatalf("UC3 handle = %q, want legacy run/1", PipelineHandleString(h))
		}
		if _, err := a.ObserveConstructionPipeline(obsRC(context.Background()), h); err != nil {
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
		h, err := a.SubmitConstructionPipeline(subRC(context.Background(), "key-rt"), spec)
		if err != nil {
			t.Fatalf("submit: %v", err)
		}
		rt := ParsePipelineHandle(PipelineHandleString(h))
		if _, err := a.ObserveConstructionPipeline(obsRC(context.Background()), rt); err != nil {
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
	if _, err := a.ObserveConstructionPipeline(obsRC(context.Background()), h); err != nil {
		t.Fatalf("observe: %v", err)
	}
	if f.lastGetTarget != want {
		t.Fatalf("observe target = %+v, want %+v", f.lastGetTarget, want)
	}
	// Cancel re-addresses the per-project repo too.
	if err := a.CancelConstructionPipeline(obsRC(context.Background()), h); err != nil {
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
		h, err := a.SubmitConstructionPipeline(subRC(context.Background(), "k"), goodSpec())
		if err != nil {
			t.Fatalf("submit: %v", err)
		}
		f.runs[0].status = tc.status
		f.runs[0].conclusion = tc.conclusion
		obs, err := a.ObserveConstructionPipeline(obsRC(context.Background()), h)
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
		h, err := a.SubmitConstructionPipeline(subRC(context.Background(), "k"), goodSpec())
		if err != nil {
			t.Fatalf("submit: %v", err)
		}
		f.runs[0].status = tc.status
		f.runs[0].conclusion = tc.conclusion
		f.runs[0].htmlURL = "https://github.com/acme/widgets/actions/runs/42"
		obs, err := a.ObserveConstructionPipeline(obsRC(context.Background()), h)
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
	h, err := a.SubmitConstructionPipeline(subRC(context.Background(), "k"), goodSpec())
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	f.runs[0].status = "in_progress"
	f.runs[0].htmlURL = ""
	obs, err := a.ObserveConstructionPipeline(obsRC(context.Background()), h)
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
	if _, err := a.ObserveConstructionPipeline(obsRC(context.Background()), ParsePipelineHandle("run/999")); kind(err) != fwra.NotFound {
		t.Fatalf("observe unknown kind = %v, want NotFound", kind(err))
	}
}

func TestSubmitErrorKinds(t *testing.T) {
	f := newFakeActions()
	f.listErr = fwra.New(fwra.Auth, "denied")
	a := newAccessForTest(t, f)
	if _, err := a.SubmitConstructionPipeline(subRC(context.Background(), "k"), goodSpec()); kind(err) != fwra.Auth {
		t.Fatalf("auth submit kind = %v", kind(err))
	}

	f2 := newFakeActions()
	f2.dispatchErr = fwra.New(fwra.Transient, "blip")
	a2 := newAccessForTest(t, f2)
	if _, err := a2.SubmitConstructionPipeline(subRC(context.Background(), "k"), goodSpec()); kind(err) != fwra.Transient {
		t.Fatalf("transient submit kind = %v", kind(err))
	}
}

func TestCancel(t *testing.T) {
	f := newFakeActions()
	a := newAccessForTest(t, f)
	h, _ := a.SubmitConstructionPipeline(subRC(context.Background(), "k"), goodSpec())
	if err := a.CancelConstructionPipeline(obsRC(context.Background()), h); err != nil {
		t.Fatalf("cancel running: %v", err)
	}
	// cancel an absent run → seam NotFound → RA success
	if err := a.CancelConstructionPipeline(obsRC(context.Background()), ParsePipelineHandle("run/999")); err != nil {
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
	h1, err := a.SubmitConstructionPipeline(subRC(ctx, "same-key"), goodSpec())
	if err != nil {
		t.Fatalf("submit1: %v", err)
	}
	h2, err := a.SubmitConstructionPipeline(subRC(ctx, "same-key"), goodSpec())
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
			handles[idx], errs[idx] = a.SubmitConstructionPipeline(subRC(ctx, "race-key"), goodSpec())
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
	h1, err := a.SubmitConstructionPipeline(subRC(ctx, "done-key"), goodSpec())
	if err != nil {
		t.Fatalf("submit1: %v", err)
	}
	f.runs[0].status = "completed"
	f.runs[0].conclusion = "success"
	h2, err := a.SubmitConstructionPipeline(subRC(ctx, "done-key"), goodSpec())
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
// test fixtures: a real throwaway bare git repo + a `claude` PATH shim.
// ---------------------------------------------------------------------------

// newBareRepo creates a real bare git repo seeded with one empty commit on
// "main" — the local-executor's clone SOURCE, mirroring
// systemtests/internal/harness/localgit.go's StartLocalGitRepo but reimplemented
// here (this test lives in the server module; the harness module is a sibling
// the RA layer must not import).
func newBareRepo(t *testing.T) (bareDir, url string) {
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

// remoteBranchExists reports whether branch exists on the bare repo.
func remoteBranchExists(t *testing.T, bareDir, branch string) bool {
	t.Helper()
	cmd := exec.Command("git", "-C", bareDir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return cmd.Run() == nil
}

// remoteCommitCount returns the commit count on branch in the bare repo.
func remoteCommitCount(t *testing.T, bareDir, branch string) int {
	t.Helper()
	out := testGitOut(t, bareDir, "rev-list", "--count", branch)
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
		// the --mcp-config value is the arg immediately after the literal
		// "--mcp-config" flag; find and copy it.
		"prev=\"\"\n" +
		"for a in \"$@\"; do\n" +
		"  if [ \"$prev\" = \"--mcp-config\" ]; then cp \"$a\" \"$CAPTURE/call-$n.mcpconfig.json\"; fi\n" +
		"  prev=\"$a\"\n" +
		"done\n" +
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
	_, err := NewLocalExecConstructionPipelineAccess("", "p", fakeStateMCPBin(t), 0)
	if kind(err) != fwra.ContractMisuse {
		t.Fatalf("kind = %v, want ContractMisuse", kind(err))
	}
}

func TestNewLocalExec_RejectsEmptyStateMCPBin(t *testing.T) {
	_, err := NewLocalExecConstructionPipelineAccess("file:///tmp/x", "p", "", 0)
	if kind(err) != fwra.ContractMisuse {
		t.Fatalf("kind = %v, want ContractMisuse", kind(err))
	}
}

func TestNewLocalExec_RejectsMissingStateMCPBin(t *testing.T) {
	_, err := NewLocalExecConstructionPipelineAccess("file:///tmp/x", "p", "/no/such/binary-xyz", 0)
	if kind(err) != fwra.ContractMisuse {
		t.Fatalf("kind = %v, want ContractMisuse", kind(err))
	}
}

// ---------------------------------------------------------------------------
// U2-U7 — contract misuse / not-found
// ---------------------------------------------------------------------------

func newLocalExecForTest(t *testing.T, repoURL string, runTimeout time.Duration) *localExecAccess {
	t.Helper()
	v, err := NewLocalExecConstructionPipelineAccess(repoURL, "test-project", fakeStateMCPBin(t), runTimeout)
	if err != nil {
		t.Fatalf("NewLocalExecConstructionPipelineAccess: %v", err)
	}
	a, ok := v.(*localExecAccess)
	if !ok {
		t.Fatalf("expected *localExecAccess, got %T", v)
	}
	return a
}

func TestLocalExecSubmit_ContractMisuse(t *testing.T) {
	_, url := newBareRepo(t)
	a := newLocalExecForTest(t, url, 0)
	ctx := context.Background()

	t.Run("empty idempotencyKey", func(t *testing.T) {
		_, err := a.SubmitConstructionPipeline(fwra.Context{Context: ctx}, goodSpec())
		if kind(err) != fwra.ContractMisuse {
			t.Fatalf("kind = %v, want ContractMisuse", kind(err))
		}
	})
	t.Run("no steps", func(t *testing.T) {
		spec := goodSpec()
		spec.Steps = nil
		_, err := a.SubmitConstructionPipeline(subRC(ctx, "k1"), spec)
		if kind(err) != fwra.ContractMisuse {
			t.Fatalf("kind = %v, want ContractMisuse", kind(err))
		}
	})
	t.Run("empty ActivityID", func(t *testing.T) {
		spec := goodSpec()
		spec.ActivityID = ""
		spec.DispatchInputs = map[string]string{"command": "service-construction", "component_id": "C-X"}
		_, err := a.SubmitConstructionPipeline(subRC(ctx, "k2"), spec)
		if kind(err) != fwra.ContractMisuse {
			t.Fatalf("kind = %v, want ContractMisuse", kind(err))
		}
	})
	t.Run("missing command dispatch input", func(t *testing.T) {
		spec := goodSpec()
		spec.DispatchInputs = map[string]string{"component_id": "C-X"}
		_, err := a.SubmitConstructionPipeline(subRC(ctx, "k3"), spec)
		if kind(err) != fwra.ContractMisuse {
			t.Fatalf("kind = %v, want ContractMisuse", kind(err))
		}
	})
}

func TestLocalExecObserveCancel_HandleMisuse(t *testing.T) {
	_, url := newBareRepo(t)
	a := newLocalExecForTest(t, url, 0)
	ctx := obsRC(context.Background())

	for _, h := range []PipelineHandle{"", "run/42", "local:"} {
		if _, err := a.ObserveConstructionPipeline(ctx, h); kind(err) != fwra.ContractMisuse {
			t.Fatalf("Observe(%q) kind = %v, want ContractMisuse", h, kind(err))
		}
		if err := a.CancelConstructionPipeline(ctx, h); kind(err) != fwra.ContractMisuse {
			t.Fatalf("Cancel(%q) kind = %v, want ContractMisuse", h, kind(err))
		}
	}
}

func TestLocalExecObserve_UnknownHandle_NotFound(t *testing.T) {
	_, url := newBareRepo(t)
	a := newLocalExecForTest(t, url, 0)
	_, err := a.ObserveConstructionPipeline(obsRC(context.Background()), "local:deadbeef")
	if kind(err) != fwra.NotFound {
		t.Fatalf("kind = %v, want NotFound", kind(err))
	}
}

func TestLocalExecCancel_UnknownHandle_NoopSuccess(t *testing.T) {
	_, url := newBareRepo(t)
	a := newLocalExecForTest(t, url, 0)
	if err := a.CancelConstructionPipeline(obsRC(context.Background()), "local:deadbeef"); err != nil {
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
		obs, err := a.ObserveConstructionPipeline(obsRC(context.Background()), handle)
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
	bareDir, url := newBareRepo(t)
	capture := filepath.Join(t.TempDir(), "capture")
	commitShim(t, capture)
	a := newLocalExecForTest(t, url, 10*time.Second)

	spec := localSpec("C-BILLENG", "billingGatewayAccess", "service-construction")
	handle, err := a.SubmitConstructionPipeline(subRC(context.Background(), "activity-key-1"), spec)
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

	// The branch exists on the ORIGIN repo with the shim's commit pushed (seed +
	// shim commit = 2).
	branch := "activity/C-BILLENG"
	if !remoteBranchExists(t, bareDir, branch) {
		t.Fatalf("branch %s was not pushed to origin", branch)
	}
	if got := remoteCommitCount(t, bareDir, branch); got != 2 {
		t.Fatalf("commit count on %s = %d, want 2 (seed + shim commit)", branch, got)
	}

	// Dispatch shape: exactly one invocation captured.
	args := readCapturedArgs(t, capture, 0)
	assertClaudeArgsShape(t, args, "-p\n/service-construction billingGatewayAccess C-BILLENG")

	// cwd was the fresh clone (a temp dir, distinct from the bare repo path).
	pwd := readCapturedPWD(t, capture, 0)
	if pwd == "" || pwd == bareDir {
		t.Fatalf("claude cwd = %q, want a fresh clone directory distinct from the bare repo", pwd)
	}

	// --mcp-config envelope: exact AIARCH_* shape.
	assertMCPConfigEnvelope(t, capture, 0, pwd, map[string]string{
		"AIARCH_PROJECT_ID":    "test-project",
		"AIARCH_JOB_MODE":      "construct",
		"AIARCH_COMPONENT_ID":  "billingGatewayAccess",
		"AIARCH_ACTIVITY_ID":   "C-BILLENG",
		"AIARCH_TARGET_BRANCH": branch,
	})
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
func assertClaudeArgsShape(t *testing.T, args, promptLine string) {
	t.Helper()
	for _, want := range []string{"--dangerously-skip-permissions", "--mcp-config", "--output-format\njson", promptLine} {
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

func TestLocalExecSubmit_SecondPhase_ReattachesToExistingActivityBranch(t *testing.T) {
	bareDir, url := newBareRepo(t)
	capture := filepath.Join(t.TempDir(), "capture")
	commitShim(t, capture)
	a := newLocalExecForTest(t, url, 10*time.Second)

	spec1 := localSpec("C-BILLENG", "billingGatewayAccess", "service-requirements")
	h1, err := a.SubmitConstructionPipeline(subRC(context.Background(), "phase-key-1"), spec1)
	if err != nil {
		t.Fatalf("Submit (phase 1): %v", err)
	}
	if obs := waitForTerminal(t, a, h1, 10*time.Second); obs.Phase != PhaseSucceeded {
		t.Fatalf("phase 1 Phase = %v, diagnostic %q", obs.Phase, obs.Diagnostic)
	}

	spec2 := localSpec("C-BILLENG", "billingGatewayAccess", "service-construction")
	h2, err := a.SubmitConstructionPipeline(subRC(context.Background(), "phase-key-2"), spec2)
	if err != nil {
		t.Fatalf("Submit (phase 2): %v", err)
	}
	if obs := waitForTerminal(t, a, h2, 10*time.Second); obs.Phase != PhaseSucceeded {
		t.Fatalf("phase 2 Phase = %v, diagnostic %q", obs.Phase, obs.Diagnostic)
	}

	// BOTH phases' commits landed on the SAME branch (seed + phase1 + phase2 = 3):
	// nothing was reset/lost between the two dispatches.
	branch := "activity/C-BILLENG"
	if got := remoteCommitCount(t, bareDir, branch); got != 3 {
		t.Fatalf("commit count on %s = %d, want 3 (seed + 2 phase commits)", branch, got)
	}
}

// ---------------------------------------------------------------------------
// G1 — idempotency convergence
// ---------------------------------------------------------------------------

func TestLocalExecSubmit_DuplicateKey_ConvergesWithoutRedispatch(t *testing.T) {
	bareDir, url := newBareRepo(t)
	capture := filepath.Join(t.TempDir(), "capture")
	commitShim(t, capture)
	a := newLocalExecForTest(t, url, 10*time.Second)

	spec := localSpec("C-BILLENG", "billingGatewayAccess", "service-construction")
	h1, err := a.SubmitConstructionPipeline(subRC(context.Background(), "same-key"), spec)
	if err != nil {
		t.Fatalf("Submit (1): %v", err)
	}
	h2, err := a.SubmitConstructionPipeline(subRC(context.Background(), "same-key"), spec)
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
	if got := remoteCommitCount(t, bareDir, branch); got != 2 {
		t.Fatalf("commit count on %s = %d, want 2 (seed + ONE shim commit)", branch, got)
	}
}

// ---------------------------------------------------------------------------
// F1-F2 — exit-code / timeout mapping
// ---------------------------------------------------------------------------

func TestLocalExecObserve_NonZeroExit_Failed(t *testing.T) {
	_, url := newBareRepo(t)
	installClaudeShim(t, "#!/bin/sh\necho 'boom: contract violation' >&2\nexit 3\n")
	a := newLocalExecForTest(t, url, 10*time.Second)

	spec := localSpec("C-FAIL", "someComponent", "service-construction")
	handle, err := a.SubmitConstructionPipeline(subRC(context.Background(), "fail-key"), spec)
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
	_, url := newBareRepo(t)
	// A well-behaved subprocess that honors SIGTERM promptly (mirrors the Cancel
	// test's shim) — this proves the ctx-deadline → Cancel(SIGTERM) → "timed out"
	// classification path, not the separate WaitDelay-forced-kill edge case a
	// TERM-ignoring process would exercise.
	installClaudeShim(t, "#!/bin/sh\nexec sleep 30\n")
	a := newLocalExecForTest(t, url, 200*time.Millisecond)

	spec := localSpec("C-SLOW", "someComponent", "service-construction")
	start := time.Now()
	handle, err := a.SubmitConstructionPipeline(subRC(context.Background(), "slow-key"), spec)
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

// ---------------------------------------------------------------------------
// C1-C2 — cancel
// ---------------------------------------------------------------------------

func TestLocalExecCancel_Running_ConvergesToCancelledNeverFailed(t *testing.T) {
	_, url := newBareRepo(t)
	installClaudeShim(t, "#!/bin/sh\nexec sleep 30\n")
	a := newLocalExecForTest(t, url, 20*time.Second)

	spec := localSpec("C-CANCEL", "someComponent", "service-construction")
	handle, err := a.SubmitConstructionPipeline(subRC(context.Background(), "cancel-key"), spec)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// Give the subprocess a moment to actually start before cancelling.
	time.Sleep(200 * time.Millisecond)
	if err := a.CancelConstructionPipeline(obsRC(context.Background()), handle); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	obs := waitForTerminal(t, a, handle, 5*time.Second)
	if obs.Phase != PhaseCancelled {
		t.Fatalf("Phase = %v, want PhaseCancelled (diagnostic: %q)", obs.Phase, obs.Diagnostic)
	}
}

func TestLocalExecCancel_AlreadyTerminal_NoopSuccess(t *testing.T) {
	_, url := newBareRepo(t)
	installClaudeShim(t, "#!/bin/sh\nexit 0\n")
	a := newLocalExecForTest(t, url, 10*time.Second)

	spec := localSpec("C-DONE", "someComponent", "service-construction")
	handle, err := a.SubmitConstructionPipeline(subRC(context.Background(), "done-key"), spec)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitForTerminal(t, a, handle, 10*time.Second)

	if err := a.CancelConstructionPipeline(obsRC(context.Background()), handle); err != nil {
		t.Fatalf("Cancel(already-terminal): unexpected error: %v", err)
	}
	// still succeeded, not overwritten by the no-op cancel.
	obs, err := a.ObserveConstructionPipeline(obsRC(context.Background()), handle)
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

func TestStderrTail_Bounds(t *testing.T) {
	long := strings.Repeat("x", 1000)
	got := stderrTail(long, 10)
	if gotRunes := len([]rune(got)); gotRunes > 11 { // 10 + the "…" marker
		t.Fatalf("stderrTail did not bound length: got %d runes (%q)", gotRunes, got)
	}
	if stderrTail("short", 10) != "short" {
		t.Fatalf("stderrTail should pass short text through unchanged")
	}
}
