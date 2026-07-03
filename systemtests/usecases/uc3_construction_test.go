package usecases

import (
	"context"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator/systemtests/internal/harness"
)

// I-UC3 — UC3 (construction / Phase-3) black-box wire coverage per the System Test
// Plan (.aiarch/state/project.json .testingState.systemTestPlan STP-UC3): a black-
// box exercise of the constructionManager pump + review-gate surface. Proves that
// ExecuteNextActivity respects the dependency network, that a send-back review
// verdict blocks the merge, and that the pump is tick-idempotent.
//
// Unlike UC1/UC2's agentic tests, UC3 needs NO AgenticGitHub-style fake: every
// server is booted with ARCHISTRATOR_CONSTRUCTION_DRYRUN=true (the harness base
// env's own default — server.go StartServer), which registers the REAL
// constructionManager Temporal Worker (the real pump + per-activity supervision +
// head-state cascade) against IN-MEMORY, instant-success stubs for its three
// EXTERNAL-effect deps (constructionpipeline / artifact / worker — server/cmd/server/
// construction_dryrun.go). No GitHub App credentials are configured, so
// sourcecontrol.SourceControlAccess (the git-forward branch/PR rail) stays nil and
// dormant — these tests prove the pump + phase-gate machinery end-to-end with NO
// GitHub REST calls at all, real or faked.
//
// Each project is pre-staged directly into Phase 3 (committed ActivityList +
// Network + ServiceContracts, phase=construction) in the seed commit of a fresh
// LocalGitRepo (harness.ConstructionProjectJSON + StartLocalGitRepoWithFiles) —
// the black-box equivalent of "this project already completed Phase 1/2" (STP-UC1/
// UC2's job), driven ENTIRELY through the published on-disk JSON shape, never an
// imported server type (R1/R3).
//
// SYNCHRONOUS DISPATCH OUTCOME (per contract .serviceContracts.constructionManager
// ExecuteNextActivity → PumpResult{dispatched, activityId}): ExecuteNextActivity returns
// THIS tick's dispatch outcome as soon as the pump has decided — {dispatched:true,
// activityId} for the activity dispatched this tick, or {dispatched:false} when
// quiescent — WITHOUT blocking on the per-activity child or the pump's background
// self-cascade over the dependency frontier. The pump still self-cascades durably in
// the background (it drains the reachable frontier via child.Get + ContinueAsNew); the
// façade just reads the first dispatch decision off the pump's queryPumpDispatch Query
// rather than awaiting the whole cascade drain. So each test below asserts the sync
// return value directly, and STILL cross-checks the observable head-state via
// GetConstructionSessionState (a robustness pattern — the two must agree).
//
// DETERMINISM: under DRYRUN every external effect (pipeline submit/observe, worker
// generate, artifact store) is an INSTANT, deterministic stub — there is no LLM/model
// dependency anywhere in this file, so every assertion here is HARD (t.Fatalf on
// timeout/mismatch), unlike UC1/UC2's best-effort TryReachStage polls.

// Test_UC3_ExecuteNextActivity_DispatchesAndDrivesPhaseGate is STP-UC3-H1: the pump
// dispatches the next eligible activity and drives it through the review gate.
// Proves ExecuteNextActivity picks the next eligible activity (no unmet
// predecessors) and returns its SYNCHRONOUS dispatch outcome ({dispatched:true,
// activityId}) WITHOUT blocking on the phase gate, and that a PhaseApprove advances
// the (background-cascading) activity to exit.
func Test_UC3_ExecuteNextActivity_DispatchesAndDrivesPhaseGate(t *testing.T) {
	requireStack(t)
	ctx := context.Background()

	const activityID = "C-BILLENG"
	projectID := "uc3-h1-" + harness.ShortID()

	seed := harness.ConstructionProjectJSON(projectID, []harness.SeedActivity{
		{ID: activityID, EffortDays: 5},
	})
	repo := harness.StartLocalGitRepoWithFiles(t, "main", map[string][]byte{
		".aiarch/state/project.json": seed,
	})
	srv := startServerWithEnv(t, true, harness.GitLocalEnv(repo.URL()))
	tr := harness.NewHTTPTransport(srv.BaseURL())
	t.Cleanup(func() { _ = tr.Close() })

	// Staging op: gate the "service" activity type's detailed_design phase, so the
	// dispatched activity SUSPENDS at the review gate. The sync tick return no longer
	// depends on this (it returns the dispatch decision, not the drained cascade), but
	// the gate is what lets the rest of the test observe/approve the suspended phase.
	if err := tr.UpdateReviewPolicy(ctx, projectID, map[string][]string{
		"service": {"detailed_design"},
	}); err != nil {
		t.Fatalf("updateReviewPolicy: %v", err)
	}

	// Step 1: the tick returns SYNCHRONOUSLY with this tick's dispatch outcome — the
	// only eligible activity — without blocking on the phase gate (per contract).
	dispatched, dispatchedID, err := tr.ExecuteNextActivity(ctx, projectID, "tick-h1-1")
	if err != nil {
		t.Fatalf("executeNextActivity: %v", err)
	}
	if !dispatched || dispatchedID != activityID {
		t.Fatalf("expected sync dispatch of %q, got dispatched=%t activityId=%q", activityID, dispatched, dispatchedID)
	}

	// Step 2: cross-check via head-state — the dispatched activity reaches the gate
	// (the pump drives it in the background after the sync return).
	st := harness.TryReachConstructionStage(ctx, t, tr, projectID, activityID, "awaitingApproval", 30*time.Second)
	if st.ActivityID != activityID {
		t.Fatalf("session activityId = %q, want %q", st.ActivityID, activityID)
	}

	// Step 3: PhaseApprove is accepted; the activity advances past the gate.
	if err := tr.SubmitPhaseDecision(ctx, projectID, activityID, "detailed_design", "approve", ""); err != nil {
		t.Fatalf("submitPhaseDecision(approve): %v", err)
	}

	// The approve unblocks the per-activity workflow (no further gate on the
	// remaining ungated phases under DRYRUN) — it runs to exit on its own.
	harness.TryReachConstructionStage(ctx, t, tr, projectID, activityID, "exited", 30*time.Second)
}

// Test_UC3_ExecuteNextActivity_NoEligibleActivity_Quiescent is STP-UC3-N1: the pump
// does not dispatch when no activity is eligible. Proves the pump honors the
// dependency network: with the only activity blocked by an unmet predecessor, it
// must NOT dispatch out of order. No cascade / no gate is involved, so this call
// returns immediately (unlike the other cases in this file).
func Test_UC3_ExecuteNextActivity_NoEligibleActivity_Quiescent(t *testing.T) {
	requireStack(t)
	ctx := context.Background()

	projectID := "uc3-n1-" + harness.ShortID()
	// C-BLOCKED depends on C-NEVERDONE, which is absent from the activity list — it
	// can never be recorded Done, so C-BLOCKED can never become eligible.
	seed := harness.ConstructionProjectJSON(projectID, []harness.SeedActivity{
		{ID: "C-BLOCKED", DependsOn: []string{"C-NEVERDONE"}, EffortDays: 5},
	})
	repo := harness.StartLocalGitRepoWithFiles(t, "main", map[string][]byte{
		".aiarch/state/project.json": seed,
	})
	srv := startServerWithEnv(t, true, harness.GitLocalEnv(repo.URL()))
	tr := harness.NewHTTPTransport(srv.BaseURL())
	t.Cleanup(func() { _ = tr.Close() })

	dispatched, activityID, err := tr.ExecuteNextActivity(ctx, projectID, "tick-blocked-01")
	if err != nil {
		t.Fatalf("executeNextActivity: %v", err)
	}
	if dispatched {
		t.Fatalf("expected dispatched=false (frontier empty), got dispatched=true activityId=%q", activityID)
	}
	if activityID != "" {
		t.Fatalf("expected empty activityId on a quiet tick, got %q", activityID)
	}
}

// Test_UC3_PhaseSendBack_BlocksMergeReopensPhase is STP-UC3-N2: a PhaseSendBack
// review verdict blocks the merge and re-opens the phase. Proves a send-back
// verdict must not merge/exit the activity — exposing any path that merges despite
// a rejecting reviewer. ExecuteNextActivity returns THIS tick's dispatch outcome
// synchronously (the pump then drives the activity in the background and suspends it at
// the never-approved gate); the merge-guard behavior is asserted on the observable
// head-state.
func Test_UC3_PhaseSendBack_BlocksMergeReopensPhase(t *testing.T) {
	requireStack(t)
	ctx := context.Background()

	const activityID = "C-BILLENG"
	projectID := "uc3-n2-" + harness.ShortID()

	seed := harness.ConstructionProjectJSON(projectID, []harness.SeedActivity{
		{ID: activityID, EffortDays: 5},
	})
	repo := harness.StartLocalGitRepoWithFiles(t, "main", map[string][]byte{
		".aiarch/state/project.json": seed,
	})
	srv := startServerWithEnv(t, true, harness.GitLocalEnv(repo.URL()))
	tr := harness.NewHTTPTransport(srv.BaseURL())
	t.Cleanup(func() { _ = tr.Close() })

	if err := tr.UpdateReviewPolicy(ctx, projectID, map[string][]string{
		"service": {"detailed_design"},
	}); err != nil {
		t.Fatalf("updateReviewPolicy: %v", err)
	}

	dispatched, dispatchedID, err := tr.ExecuteNextActivity(ctx, projectID, "tick-n2-1")
	if err != nil {
		t.Fatalf("executeNextActivity: %v", err)
	}
	if !dispatched || dispatchedID != activityID {
		t.Fatalf("expected sync dispatch of %q, got dispatched=%t activityId=%q", activityID, dispatched, dispatchedID)
	}

	harness.TryReachConstructionStage(ctx, t, tr, projectID, activityID, "awaitingApproval", 30*time.Second)

	// PhaseSendBack, with feedback notes (required non-empty by the contract).
	if err := tr.SubmitPhaseDecision(ctx, projectID, activityID, "detailed_design", "sendBack",
		"Contract has 22 operations; split the component (reject >=20)."); err != nil {
		t.Fatalf("submitPhaseDecision(sendBack): %v", err)
	}

	// HARD: the activity must NEVER reach "exited" in a window a clean approve
	// would have taken it there in (30s, per the H1 proof above) — the send-back
	// re-runs the phase's pipeline and re-suspends at the SAME gate, never merging.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st, err := tr.GetConstructionSessionState(ctx, projectID, activityID)
		if err == nil && st.Stage == "exited" {
			t.Fatalf("activity %q exited after PhaseSendBack — the merge guard did not block a rejecting reviewer", activityID)
		}
		time.Sleep(100 * time.Millisecond)
	}
	// The phase re-opened: the activity is back at the (redrafted) approval gate,
	// not stuck anywhere else and not exited.
	final := harness.TryReachConstructionStage(ctx, t, tr, projectID, activityID, "awaitingApproval", 30*time.Second)
	if final.Stage != "awaitingApproval" {
		t.Fatalf("expected the phase to re-open at awaitingApproval after send-back, got stage %q", final.Stage)
	}
}

// Test_UC3_ExecuteNextActivity_DuplicateTickID_Idempotent is STP-UC3-B1: a
// duplicate ExecuteNextActivity with the same tickID is idempotent. Proves
// scheduler retries must not double-dispatch. The FIRST call returns THIS tick's
// synchronous dispatch outcome ({dispatched:true, C-BILLENG}); the activity then runs
// to exit in the background cascade. A retry with the SAME tickID must never
// re-dispatch nor dispatch a DIFFERENT activity, and once the (single-activity) frontier
// is drained the retry reports {dispatched:false}. The load-bearing assertion is that
// the activity reaches "exited" exactly once and the retry produces no error, no hang,
// and no re-dispatch.
func Test_UC3_ExecuteNextActivity_DuplicateTickID_Idempotent(t *testing.T) {
	requireStack(t)
	ctx := context.Background()

	const activityID = "C-BILLENG"
	projectID := "uc3-b1-" + harness.ShortID()

	seed := harness.ConstructionProjectJSON(projectID, []harness.SeedActivity{
		{ID: activityID, EffortDays: 5},
	})
	repo := harness.StartLocalGitRepoWithFiles(t, "main", map[string][]byte{
		".aiarch/state/project.json": seed,
	})
	srv := startServerWithEnv(t, true, harness.GitLocalEnv(repo.URL()))
	tr := harness.NewHTTPTransport(srv.BaseURL())
	t.Cleanup(func() { _ = tr.Close() })

	const tickID = "tick-dup-42"

	// First call returns the sync dispatch outcome for the only eligible activity.
	dispatched1, activityID1, err := tr.ExecuteNextActivity(ctx, projectID, tickID)
	if err != nil {
		t.Fatalf("executeNextActivity (first): %v", err)
	}
	if !dispatched1 || activityID1 != activityID {
		t.Fatalf("first call: expected sync dispatch of %q, got dispatched=%t activityId=%q", activityID, dispatched1, activityID1)
	}
	harness.TryReachConstructionStage(ctx, t, tr, projectID, activityID, "exited", 15*time.Second)

	// Retry the SAME tickID until the (now single-activity, drained) frontier reports
	// quiescent. A retry may only ever observe the already-dispatched C-BILLENG (while
	// the background cascade winds down) or nothing — never a re-dispatch of a NEW
	// activity. It converges to dispatched=false once the cascade has drained.
	deadline := time.Now().Add(15 * time.Second)
	var dispatched2 bool
	var activityID2 string
	for {
		dispatched2, activityID2, err = tr.ExecuteNextActivity(ctx, projectID, tickID)
		if err != nil {
			t.Fatalf("executeNextActivity (duplicate tickID): %v", err)
		}
		if activityID2 != "" && activityID2 != activityID {
			t.Fatalf("duplicate tickID re-dispatched a DIFFERENT activity %q (want none / %q)", activityID2, activityID)
		}
		if !dispatched2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("duplicate tickID never drained to quiescent (last: dispatched=%t activityId=%q)", dispatched2, activityID2)
		}
		time.Sleep(200 * time.Millisecond)
	}

	// The activity is still (only) exited — the retry did not re-dispatch or
	// otherwise disturb it.
	final, err := tr.GetConstructionSessionState(ctx, projectID, activityID)
	if err != nil {
		t.Fatalf("getConstructionSessionState after duplicate tick: %v", err)
	}
	if final.Stage != "exited" {
		t.Fatalf("expected %s to remain exited after the duplicate tick, got stage %q", activityID, final.Stage)
	}
}
