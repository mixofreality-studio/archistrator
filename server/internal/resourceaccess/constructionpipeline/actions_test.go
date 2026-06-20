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
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"

	fwra "github.com/davidmarne/archistrator-platform/framework-go/resourceaccess"
)

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
			out = append(out, ghRun{id: r.id, name: r.name, status: r.status, conclusion: r.conclusion})
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
	for k, v := range dispatchInputs {
		merged[k] = v
	}
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
			return ghRun{id: r.id, name: r.name, status: r.status, conclusion: r.conclusion}, nil
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
func newAccessForTest(t *testing.T, f *fakeActions) *Access {
	t.Helper()
	a, err := New(f)
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

func TestNewRejectsNilClient(t *testing.T) {
	if _, err := New(nil); kind(err) != fwra.ContractMisuse {
		t.Fatalf("New(nil) kind = %v, want ContractMisuse", kind(err))
	}
}

func TestSubmitContractMisuse(t *testing.T) {
	a := newAccessForTest(t, newFakeActions())
	ctx := context.Background()

	if _, err := a.SubmitConstructionPipeline(ctx, goodSpec(), ""); kind(err) != fwra.ContractMisuse {
		t.Fatalf("empty key kind = %v", kind(err))
	}
	noSteps := goodSpec()
	noSteps.Steps = nil
	if _, err := a.SubmitConstructionPipeline(ctx, noSteps, "k"); kind(err) != fwra.ContractMisuse {
		t.Fatalf("no steps kind = %v", kind(err))
	}
	dup := goodSpec()
	dup.Steps = []PipelineStep{{Name: "x"}, {Name: "x"}}
	if _, err := a.SubmitConstructionPipeline(ctx, dup, "k"); kind(err) != fwra.ContractMisuse {
		t.Fatalf("dup step kind = %v", kind(err))
	}
	empty := goodSpec()
	empty.Steps = []PipelineStep{{Name: "  "}}
	if _, err := a.SubmitConstructionPipeline(ctx, empty, "k"); kind(err) != fwra.ContractMisuse {
		t.Fatalf("empty step name kind = %v", kind(err))
	}
	dangling := goodSpec()
	dangling.Edges = []StepDependency{{From: "build", To: "nope"}}
	if _, err := a.SubmitConstructionPipeline(ctx, dangling, "k"); kind(err) != fwra.ContractMisuse {
		t.Fatalf("dangling edge kind = %v", kind(err))
	}
}

func TestObserveCancelHandleMisuse(t *testing.T) {
	a := newAccessForTest(t, newFakeActions())
	ctx := context.Background()
	if _, err := a.ObserveConstructionPipeline(ctx, PipelineHandle{}); kind(err) != fwra.ContractMisuse {
		t.Fatalf("zero handle observe kind = %v", kind(err))
	}
	if err := a.CancelConstructionPipeline(ctx, PipelineHandle{}); kind(err) != fwra.ContractMisuse {
		t.Fatalf("zero handle cancel kind = %v", kind(err))
	}
	bad := HandleFromString("garbage-no-slash")
	if _, err := a.ObserveConstructionPipeline(ctx, bad); kind(err) != fwra.ContractMisuse {
		t.Fatalf("malformed handle observe kind = %v", kind(err))
	}
}

func TestSubmitHappyPath(t *testing.T) {
	f := newFakeActions()
	a := newAccessForTest(t, f)
	h, err := a.SubmitConstructionPipeline(context.Background(), goodSpec(), "key-1")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if h.IsZero() {
		t.Fatal("handle is zero")
	}
	if f.dispatchCount != 1 {
		t.Fatalf("dispatchCount = %d, want 1", f.dispatchCount)
	}
	if h.String() != "run/1" {
		t.Fatalf("handle = %q, want run/1", h.String())
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
		if _, err := a.SubmitConstructionPipeline(context.Background(), spec, "key-di"); err != nil {
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
		if _, err := a.SubmitConstructionPipeline(context.Background(), spec, "key-spoof"); err != nil {
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
		if _, err := a.SubmitConstructionPipeline(context.Background(), spec, "key-nil"); err != nil {
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

		h, err := a.SubmitConstructionPipeline(context.Background(), spec, "key-pp")
		if err != nil {
			t.Fatalf("submit: %v", err)
		}
		want := ghTarget{owner: "acme", repo: "my-system", workflowFile: "aiarch-design.yml"}
		if f.lastDispatchTarget != want {
			t.Fatalf("dispatch target = %+v, want %+v (per-project repo + aiarch-design.yml)", f.lastDispatchTarget, want)
		}
		// The handle encodes the target so the stateless Observe/Cancel can re-address it.
		if h.String() != "run/1@acme/my-system/aiarch-design.yml" {
			t.Fatalf("handle = %q, want run/1@acme/my-system/aiarch-design.yml", h.String())
		}
		// Observe re-addresses the per-project repo (not the construction default).
		if _, err := a.ObserveConstructionPipeline(context.Background(), h); err != nil {
			t.Fatalf("observe: %v", err)
		}
		if f.lastGetTarget != want {
			t.Fatalf("observe target = %+v, want %+v", f.lastGetTarget, want)
		}
		// Cancel re-addresses the per-project repo too.
		if err := a.CancelConstructionPipeline(context.Background(), h); err != nil {
			t.Fatalf("cancel: %v", err)
		}
		if f.lastCancelTarget != want {
			t.Fatalf("cancel target = %+v, want %+v", f.lastCancelTarget, want)
		}
	})

	// UC3 CONSTRUCTION dispatch (zero TargetRepo): the handle stays the legacy
	// "run/<id>" form and the seam sees a ZERO target (falls back to the construction
	// repo default) — byte-for-byte unchanged.
	t.Run("construction_dispatch_zero_target_legacy_handle", func(t *testing.T) {
		f := newFakeActions()
		a := newAccessForTest(t, f)
		h, err := a.SubmitConstructionPipeline(context.Background(), goodSpec(), "key-uc3")
		if err != nil {
			t.Fatalf("submit: %v", err)
		}
		if !f.lastDispatchTarget.isZero() {
			t.Fatalf("UC3 dispatch target = %+v, want zero (construction-repo default)", f.lastDispatchTarget)
		}
		if h.String() != "run/1" {
			t.Fatalf("UC3 handle = %q, want legacy run/1", h.String())
		}
		if _, err := a.ObserveConstructionPipeline(context.Background(), h); err != nil {
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
		h, err := a.SubmitConstructionPipeline(context.Background(), spec, "key-rt")
		if err != nil {
			t.Fatalf("submit: %v", err)
		}
		rt := HandleFromString(h.String())
		if _, err := a.ObserveConstructionPipeline(context.Background(), rt); err != nil {
			t.Fatalf("observe round-tripped handle: %v", err)
		}
		want := ghTarget{owner: "o", repo: "r", workflowFile: "aiarch-design.yml"}
		if f.lastGetTarget != want {
			t.Fatalf("round-trip observe target = %+v, want %+v", f.lastGetTarget, want)
		}
	})
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
		h, err := a.SubmitConstructionPipeline(context.Background(), goodSpec(), "k")
		if err != nil {
			t.Fatalf("submit: %v", err)
		}
		f.runs[0].status = tc.status
		f.runs[0].conclusion = tc.conclusion
		obs, err := a.ObserveConstructionPipeline(context.Background(), h)
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

func TestObserveNotFound(t *testing.T) {
	f := newFakeActions()
	a := newAccessForTest(t, f)
	if _, err := a.ObserveConstructionPipeline(context.Background(), HandleFromString("run/999")); kind(err) != fwra.NotFound {
		t.Fatalf("observe unknown kind = %v, want NotFound", kind(err))
	}
}

func TestSubmitErrorKinds(t *testing.T) {
	f := newFakeActions()
	f.listErr = fwra.New(fwra.Auth, "denied")
	a := newAccessForTest(t, f)
	if _, err := a.SubmitConstructionPipeline(context.Background(), goodSpec(), "k"); kind(err) != fwra.Auth {
		t.Fatalf("auth submit kind = %v", kind(err))
	}

	f2 := newFakeActions()
	f2.dispatchErr = fwra.New(fwra.Transient, "blip")
	a2 := newAccessForTest(t, f2)
	if _, err := a2.SubmitConstructionPipeline(context.Background(), goodSpec(), "k"); kind(err) != fwra.Transient {
		t.Fatalf("transient submit kind = %v", kind(err))
	}
}

func TestCancel(t *testing.T) {
	f := newFakeActions()
	a := newAccessForTest(t, f)
	h, _ := a.SubmitConstructionPipeline(context.Background(), goodSpec(), "k")
	if err := a.CancelConstructionPipeline(context.Background(), h); err != nil {
		t.Fatalf("cancel running: %v", err)
	}
	// cancel an absent run → seam NotFound → RA success
	if err := a.CancelConstructionPipeline(context.Background(), HandleFromString("run/999")); err != nil {
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
		f := newFakeActions()
		a := newAccessForTest(t, f)
		h1, err := a.SubmitConstructionPipeline(ctx, goodSpec(), "same-key")
		if err != nil {
			t.Fatalf("submit1: %v", err)
		}
		h2, err := a.SubmitConstructionPipeline(ctx, goodSpec(), "same-key")
		if err != nil {
			t.Fatalf("submit2: %v", err)
		}
		if !h1.Equal(h2) {
			t.Fatalf("handles diverged: %s vs %s", h1, h2)
		}
		if f.dispatchCount != 1 {
			t.Fatalf("dispatchCount = %d, want 1 (replay must NOT re-dispatch)", f.dispatchCount)
		}
		if len(f.runs) != 1 {
			t.Fatalf("run count = %d, want 1", len(f.runs))
		}
	})

	// G2 — two CONCURRENT submits with the SAME key. Even if both race past the
	// probe and both dispatch (creating two runs), both converge on the lowest-id
	// canonical handle and exactly ONE run survives (the sibling is cancelled).
	t.Run("concurrent_submits_converge", func(t *testing.T) {
		// Run many times to exercise the race interleavings.
		for iter := 0; iter < 200; iter++ {
			f := newFakeActions()
			a := newAccessForTest(t, f)

			var wg sync.WaitGroup
			handles := make([]PipelineHandle, 2)
			errs := make([]error, 2)
			for i := 0; i < 2; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					handles[idx], errs[idx] = a.SubmitConstructionPipeline(ctx, goodSpec(), "race-key")
				}(i)
			}
			wg.Wait()

			for i, e := range errs {
				if e != nil {
					t.Fatalf("iter %d submit %d: %v", iter, i, e)
				}
			}
			if !handles[0].Equal(handles[1]) {
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
					if "run/"+itoaTest(r.id) == canonical.String() {
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
			if canonical.String() != "run/"+itoaTest(lowest) {
				t.Fatalf("iter %d: canonical %s is not the lowest-id run run/%d", iter, canonical, lowest)
			}
		}
	})

	// G3 — replay AFTER completion: the run is terminal; a re-submit still finds it
	// and returns the same handle (no new dispatch).
	t.Run("replay_after_completion", func(t *testing.T) {
		f := newFakeActions()
		a := newAccessForTest(t, f)
		h1, err := a.SubmitConstructionPipeline(ctx, goodSpec(), "done-key")
		if err != nil {
			t.Fatalf("submit1: %v", err)
		}
		f.runs[0].status = "completed"
		f.runs[0].conclusion = "success"
		h2, err := a.SubmitConstructionPipeline(ctx, goodSpec(), "done-key")
		if err != nil {
			t.Fatalf("submit2: %v", err)
		}
		if !h1.Equal(h2) {
			t.Fatalf("post-completion handles diverged: %s vs %s", h1, h2)
		}
		if f.dispatchCount != 1 {
			t.Fatalf("dispatchCount = %d, want 1", f.dispatchCount)
		}
	})
}

func TestDedupTokenDeterminism(t *testing.T) {
	if dedupToken("a") != dedupToken("a") {
		t.Fatal("dedupToken not deterministic")
	}
	if dedupToken("a") == dedupToken("b") {
		t.Fatal("dedupToken collision on distinct keys")
	}
}
