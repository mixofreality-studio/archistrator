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
// SELF-CASCADE FINDING (read directly from server/internal/manager/construction/
// workflow.go's PumpNextActivityWorkflow, confirmed empirically via `temporal
// workflow show` against a live run): ExecuteNextActivity's Temporal workflow does
// NOT return after dispatching one activity. It calls `child.Get(ctx, nil)` —a
// BLOCKING wait for the dispatched per-activity child workflow to run to full
// TERMINAL completion — then `workflow.NewContinueAsNewError(...)` to pick the
// NEXT eligible activity, repeating until the frontier drains. The Temporal Go
// client's WorkflowRun.Get transparently follows a ContinueAsNew chain, so a
// SINGLE synchronous ExecuteNextActivity call blocks until the ENTIRE reachable
// cascade drains to quiescence — there is NO code path that returns
// PumpResult{Dispatched:true, ActivityID:...} to a synchronous caller; the method
// doc comment ("the child runs asynchronously") does not match this behavior. Two
// direct consequences for these tests, both noted inline:
//   - A call that dispatches a phase-gated activity blocks until that phase's
//     approval is delivered by a CONCURRENT caller — proven here by racing the
//     blocking call against a poll+approve goroutine, not sequential steps.
//   - Every synchronous ExecuteNextActivity call that DOES eventually return
//     (rather than hang on a still-suspended gate) reports dispatched=false — the
//     drained-quiescent result — even on the very tick that did the dispatching.
//     "Was something dispatched" is therefore proven via GetConstructionSessionState
//     observability, not the call's own return value, in every case below.
//
// DETERMINISM: under DRYRUN every external effect (pipeline submit/observe, worker
// generate, artifact store) is an INSTANT, deterministic stub — there is no LLM/model
// dependency anywhere in this file, so every assertion here is HARD (t.Fatalf on
// timeout/mismatch), unlike UC1/UC2's best-effort TryReachStage polls.

// Test_UC3_ExecuteNextActivity_DispatchesAndDrivesPhaseGate is STP-UC3-H1: the pump
// dispatches the next eligible activity and drives it through the review gate.
// Proves ExecuteNextActivity picks the next eligible activity (no unmet
// predecessors), and a PhaseApprove advances it to exit — observed via
// GetConstructionSessionState while the dispatching ExecuteNextActivity call is
// still in flight (see the self-cascade finding above).
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
	// dispatched activity SUSPENDS at the review gate instead of the DRYRUN pipeline
	// racing straight through to exit within the one blocking tick call.
	if err := tr.UpdateReviewPolicy(ctx, projectID, map[string][]string{
		"service": {"detailed_design"},
	}); err != nil {
		t.Fatalf("updateReviewPolicy: %v", err)
	}

	type tickResult struct {
		dispatched bool
		activityID string
		err        error
	}
	tickDone := make(chan tickResult, 1)
	go func() {
		d, id, err := tr.ExecuteNextActivity(ctx, projectID, "tick-h1-1")
		tickDone <- tickResult{d, id, err}
	}()

	// Step 1/2: the tick dispatched the only eligible activity — observed via the
	// session reaching the gate (the call itself is still blocked in-flight).
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

	// The now-unblocked tick call returns once the cascade drains (nothing else is
	// eligible in this single-activity network) — a quiet, error-free tick, per the
	// self-cascade finding above.
	select {
	case res := <-tickDone:
		if res.err != nil {
			t.Fatalf("executeNextActivity: %v", res.err)
		}
		if res.dispatched {
			t.Fatalf("expected the drained tick to report dispatched=false, got dispatched=true activityId=%q", res.activityID)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("executeNextActivity never returned after the dispatched activity exited")
	}
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
// a rejecting reviewer. The dispatching ExecuteNextActivity call for THIS case never
// legitimately returns within the test (the activity is never approved, so the
// child workflow never reaches a terminal state and the pump's child.Get() never
// unblocks) — it is fired with a bounded context and its result deliberately
// discarded; only the observable session state is asserted.
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

	tickCtx, cancelTick := context.WithTimeout(ctx, 20*time.Second)
	t.Cleanup(cancelTick)
	go func() { _, _, _ = tr.ExecuteNextActivity(tickCtx, projectID, "tick-n2-1") }()

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
// scheduler retries must not double-dispatch. Neither call gates anything, so both
// return promptly: the first cascades the single activity to completion in-line
// (dispatched=false — the drained result, per the self-cascade finding above); the
// SAME tickID replayed after the workflow has reached a terminal state starts a
// fresh execution (Temporal's default WorkflowIDReusePolicy) that immediately finds
// nothing eligible — also dispatched=false. The load-bearing assertion is that the
// activity reaches "exited" exactly once and the retry produces no error, no hang,
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

	dispatched1, activityID1, err := tr.ExecuteNextActivity(ctx, projectID, tickID)
	if err != nil {
		t.Fatalf("executeNextActivity (first): %v", err)
	}
	if dispatched1 || activityID1 != "" {
		t.Fatalf("first call: expected the drained result dispatched=false (see self-cascade finding), got dispatched=%t activityId=%q", dispatched1, activityID1)
	}
	harness.TryReachConstructionStage(ctx, t, tr, projectID, activityID, "exited", 15*time.Second)

	dispatched2, activityID2, err := tr.ExecuteNextActivity(ctx, projectID, tickID)
	if err != nil {
		t.Fatalf("executeNextActivity (duplicate tickID, retried after completion): %v", err)
	}
	if dispatched2 || activityID2 != "" {
		t.Fatalf("duplicate tickID: expected dispatched=false (nothing eligible — %s already Done), got dispatched=%t activityId=%q", activityID, dispatched2, activityID2)
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
