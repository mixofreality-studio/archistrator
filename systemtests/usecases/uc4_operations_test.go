package usecases

import (
	"context"
	"errors"
	"testing"

	"github.com/mixofreality-studio/archistrator/systemtests/internal/harness"
)

// I-UC4 — UC4 (operations / Phase-4) black-box wire coverage per the System Test Plan
// (.aiarch/state/project.json .testingState.systemTestPlan STP-UC4): a black-box exercise
// of the operationsManager surface.
//
// D9 SUPERSEDES THE 503 PREMISE BELOW (operations-argocd-deployment Task 11,
// 2026-08-07). These tests boot the LOCAL profile (project-state git local), and D9
// ruled that a profile holding no deployment credential must not APPEAR to operate —
// "not a disabled console, not a simulated one". The composition root therefore
// UNMOUNTS every /api/v1/operations/ route on that profile (server/cmd/server/
// hooks.go ExtraMounts), so the honest wire expectation here is 404 / ErrNotFound,
// NOT the 503 fail-fast these tests were written against. Until this was corrected
// the whole file failed — invisibly, because the suite panicked on an earlier
// timeout before ever reaching it.
//
// REAL UC4 wire coverage (the 503 fail-fast, and eventually a seeded happy path)
// needs a CLOUD-profile server, which needs the deployment credential the local
// harness deliberately has no way to supply. That remains the N-DEP earmark below.
//
// IMPLEMENTATION-STATE FINDING (read directly from the server module, not inferred), as of
// the C-OSA / C-OR construction:
//   - operatedSystemStateAccess is now the REAL Postgres head-state store
//     (internal/resourceaccess/operatedsystemstate: NewPostgresOperatedSystemStateAccess,
//     operated_system head-state row + optimistic-concurrency version + the
//     operated_system_mutation dedup-first idempotency ledger). It is no longer a stub.
//   - operatedRuntimeAccess is the PROFILED RA (internal/resourceaccess/operatedruntime),
//     now selected by the DEPLOYMENT PROFILE in the generated composition root
//     (main.gen.go: cloud → Real, local → Local) plus the orthogonal
//     ARCHISTRATOR_OPERATIONS_DRYRUN Finalize override (hooks.go
//     FinalizeOperatedRuntimeAccess: cloud + DRYRUN=true swaps Real → Local). The harness
//     boots every server with ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL=true (a throwaway
//     on-disk repo), which IS the "local" deployment profile — so these tests run against
//     the LOCAL deterministic operatedRuntime variant (writes accepted as no-ops, observe
//     reads Healthy/SLO-met), NOT the Real profile's explicit not-implemented errors.
//     That distinction is unobservable below anyway: every wire path here is gated by a
//     head-state read FIRST, so no operatedRuntime verb is ever reached (see the outcome
//     notes). The Real profile (cloud, DRYRUN=false) still defers to the N-DEP
//     GitOps/kubernetes follow-up with a diagnosable non-retryable error.
//
// What this means for the STP-UC4 wire outcomes, against an operated app that was NEVER
// seeded (the deploy-after-construction SEEDING handoff — a cross-Manager write / an added
// verb that populates the operated_system row + its DeployableBundleRef — is a documented
// open gap on the frozen contract, so no wire path creates a row today):
//   - deploy (full bundle) and queryOperatedSystemView read head-state FIRST; a missing row
//     is a real fwra.NotFound, the workflow fails, and the operationsManager façade maps
//     EVERY workflow-execution failure to fwmgr.Infrastructure → HTTP 503 (operationsmanager.go
//     we.Get() branches) → harness.ErrUnavailable. A deterministic fail-fast on a REAL
//     NotFound — reached before any operatedRuntime verb, in EITHER runtime profile.
//   - reconcileOperatedState scans the in-flight set FIRST (ReadInFlightOperatedApps). With no
//     seeded apps that scan is a clean, empty SUCCESS (observed=0) — it never reaches the
//     operatedRuntime reads. This is a genuine improvement the real head-state store enables
//     and the stub could not: the reconcile tick now runs to completion.
//   - withdrawSystem reads head-state FIRST; a missing row is the already-withdrawn terminal
//     post-condition, so WithdrawWorkflow returns SUCCESS (withdrawn=true) — an idempotent
//     no-op. Also a real improvement over the stub's uniform failure.
//   - applyDelinquencyPolicy is a QUEUED Signal (SignalWithStartWorkflow, fire-and-forget):
//     it returns success once durably enqueued, independent of the enforcement workflow.
//
// A full deploy→reconcile→observe→Healthy happy path remains honestly PENDING on two
// follow-ups: (1) the operated_system SEEDING handoff (no frozen verb populates the row /
// DeployableBundleRef), and (2) — for the cloud profile only — the operatedRuntime REAL
// profile's kubernetes/GitOps backend (N-DEP). The local harness already runs the
// deterministic Local runtime variant, but deploy/view still need a seeded row, so the
// seeding gap is the gating item.

// operationsSurfaceServer boots a plain server (no construction/agentic profile —
// operationsManager needs no git substrate of its own) with the harness's own
// auto-provisioned throwaway project-state repo (main_test.go's startServer default), and
// returns a bound Transport. That throwaway repo sets ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL
// = the "local" deployment profile, so the LOCAL deterministic operatedRuntime variant is
// in force (see the file-level finding) — not that it matters below: head-state gates
// every path first.
func operationsSurfaceServer(t *testing.T) harness.Transport {
	t.Helper()
	srv := startServer(t, true)
	tr := harness.NewHTTPTransport(srv.BaseURL())
	t.Cleanup(func() { _ = tr.Close() })

	// D9 GUARD. Every test below drives a verb under /api/v1/operations/, and this
	// harness can only boot the LOCAL profile — where D9 unmounts that whole route
	// prefix on purpose. Their 503 fail-fast expectations are not WRONG, they are
	// unreachable here, and the exercise they describe needs the cloud profile's
	// deployment credential (the standing N-DEP earmark). Skip with the reason rather
	// than delete the plan coverage or rewrite it into an assertion about 404s —
	// Test_UC4_D9_OperationsSurfaceIsAbsentOnLocalProfile owns that claim.
	if _, err := tr.QueryOperatedSystemView(context.Background(), harness.ShortID(), "req-d9-probe"); errors.Is(err, harness.ErrNotFound) {
		t.Skip("D9: the local profile unmounts /api/v1/operations/ — UC4 wire coverage needs a cloud-profile server (N-DEP)")
	}
	return tr
}

// Test_UC4_D9_OperationsSurfaceIsAbsentOnLocalProfile pins the D9 ruling itself
// (operations-argocd-deployment Task 11, 2026-08-07): a profile holding no deployment
// credential must not APPEAR to operate — "not a disabled console, not a simulated
// one". The composition root unmounts the generated routes, so the wire answers 404
// exactly as if they had never been registered. Hiding the webApp nav entry is not
// enough on its own; this is the server-side half of that ruling, and without a test
// the unmount could be dropped in a refactor and nothing would notice.
func Test_UC4_D9_OperationsSurfaceIsAbsentOnLocalProfile(t *testing.T) {
	requireStack(t)
	ctx := context.Background()

	srv := startServer(t, true)
	tr := harness.NewHTTPTransport(srv.BaseURL())
	t.Cleanup(func() { _ = tr.Close() })

	if _, err := tr.QueryOperatedSystemView(ctx, harness.ShortID(), "req-d9-assert"); !errors.Is(err, harness.ErrNotFound) {
		t.Fatalf("queryOperatedSystemView on the local profile: want ErrNotFound (D9 unmounts the route), got %v", err)
	}
}

// Test_UC4_DeployReconcileObserve is STP-UC4-H1 (Deploy after construction, reconcile, and
// observe Healthy), adapted to the CURRENT implementation state (see the file-level finding):
// deploy and observe against an unseeded operated app fail fast on a REAL head-state NotFound
// (→ 503), while the reconcile tick — over the real, empty in-flight scan — now SUCCEEDS
// (observed=0) rather than failing on a stub. The literal Published/Healthy outcome is gated
// on the operated_system seeding handoff (documented follow-up).
func Test_UC4_DeployReconcileObserve(t *testing.T) {
	requireStack(t)
	ctx := context.Background()
	tr := operationsSurfaceServer(t)

	operatedAppID := harness.NewProjectID()

	published, revision, err := tr.DeployAfterConstruction(ctx, operatedAppID, harness.DesiredStateChange{
		Reason:    "deployAfterConstruction",
		PatchKind: "fullBundle",
		ChangeID:  "chg-deploy-001",
	})
	// Unseeded app ⇒ head-state read NotFound ⇒ workflow fails ⇒ façade 503.
	if !errors.Is(err, harness.ErrUnavailable) {
		t.Fatalf("deployAfterConstruction (unseeded app): expected ErrUnavailable (head-state NotFound at the façade), got err=%v published=%t revision=%q", err, published, revision)
	}

	// The reconcile tick over the REAL, empty in-flight scan is a clean success (observed=0):
	// the head-state store enables what the stub could not.
	observed, transitions, republished, err := tr.ReconcileOperatedState(ctx, "tick-recon-1", nil)
	if err != nil {
		t.Fatalf("reconcileOperatedState (no in-flight apps): expected success, got err=%v", err)
	}
	if observed != 0 || transitions != 0 || republished != 0 {
		t.Fatalf("reconcileOperatedState (no in-flight apps): expected observed=0/transitions=0/republished=0, got observed=%d transitions=%d republished=%d", observed, transitions, republished)
	}

	view, err := tr.QueryOperatedSystemView(ctx, operatedAppID, "req-view-1")
	if !errors.Is(err, harness.ErrUnavailable) {
		t.Fatalf("queryOperatedSystemView (unseeded app): expected ErrUnavailable (head-state NotFound at the façade), got err=%v view=%+v", err, view)
	}
}

// Test_UC4_ApplyDelinquencyPolicy_QueuedSignalSucceeds is STP-UC4-N1 (Delinquency policy
// pauses the operated system), adapted: the queued Manager→Manager signal is durably enqueued
// and returns success (it never touches head-state on the request path, only
// SignalWithStartWorkflow), which is exactly what STP-UC4-N1's step 1 asserts. The subsequent
// read-back against an unseeded app hits the real head-state NotFound (→ 503), as step 2's
// Phase=paused assertion needs a seeded, enforced row (seeding-handoff follow-up).
func Test_UC4_ApplyDelinquencyPolicy_QueuedSignalSucceeds(t *testing.T) {
	requireStack(t)
	ctx := context.Background()
	tr := operationsSurfaceServer(t)

	customerID := harness.NewProjectID()
	if err := tr.ApplyDelinquencyPolicy(ctx, customerID, true /* pauseNotWithdraw */); err != nil {
		t.Fatalf("applyDelinquencyPolicy: expected the queued signal to succeed, got %v", err)
	}

	operatedAppID := harness.NewProjectID()
	view, err := tr.QueryOperatedSystemView(ctx, operatedAppID, "req-view-2")
	if !errors.Is(err, harness.ErrUnavailable) {
		t.Fatalf("queryOperatedSystemView after delinquency signal (unseeded app): expected ErrUnavailable (head-state NotFound at the façade), got err=%v view=%+v", err, view)
	}
}

// Test_UC4_DeployAfterConstruction_DuplicateChangeID_SameDeterministicOutcome is STP-UC4-B1
// (Duplicate DeployAfterConstruction with the same changeId is idempotent), adapted: a
// replayed changeId must never produce a DIVERGENT outcome. Against an unseeded app neither
// call can succeed (head-state NotFound → 503), so "no divergence" means both calls fail
// IDENTICALLY (same ErrUnavailable, published=false, no revision) — a real, if weaker,
// replay-safety proof.
func Test_UC4_DeployAfterConstruction_DuplicateChangeID_SameDeterministicOutcome(t *testing.T) {
	requireStack(t)
	ctx := context.Background()
	tr := operationsSurfaceServer(t)

	operatedAppID := harness.NewProjectID()
	change := harness.DesiredStateChange{
		Reason:    "deployAfterConstruction",
		PatchKind: "fullBundle",
		ChangeID:  "chg-deploy-001",
	}

	published1, revision1, err1 := tr.DeployAfterConstruction(ctx, operatedAppID, change)
	if !errors.Is(err1, harness.ErrUnavailable) {
		t.Fatalf("first deploy: expected ErrUnavailable (head-state NotFound at the façade), got err=%v published=%t revision=%q", err1, published1, revision1)
	}

	published2, revision2, err2 := tr.DeployAfterConstruction(ctx, operatedAppID, change)
	if !errors.Is(err2, harness.ErrUnavailable) {
		t.Fatalf("replayed deploy (same changeId): expected ErrUnavailable (head-state NotFound at the façade), got err=%v published=%t revision=%q", err2, published2, revision2)
	}
	if published1 != published2 || revision1 != revision2 {
		t.Fatalf("replayed deploy diverged from the first attempt: (published=%t,revision=%q) vs (published=%t,revision=%q)",
			published1, revision1, published2, revision2)
	}
}

// Test_UC4_WithdrawSystem_UnseededIsIdempotentNoOp is STP-UC4-N2 (Withdrawal is terminal;
// reconcile does not resurrect a withdrawn app), adapted to the real head-state store: an
// unseeded app has no row, which IS the already-withdrawn terminal post-condition, so
// withdrawSystem returns an idempotent no-op SUCCESS (withdrawn=true). The follow-up reconcile
// then runs its empty in-flight scan to a clean success — no resurrection, no stub failure.
func Test_UC4_WithdrawSystem_UnseededIsIdempotentNoOp(t *testing.T) {
	requireStack(t)
	ctx := context.Background()
	tr := operationsSurfaceServer(t)

	operatedAppID := harness.NewProjectID()

	withdrawn, err := tr.WithdrawSystem(ctx, operatedAppID, "chg-withdraw-1", "Customer offboarded.")
	if err != nil {
		t.Fatalf("withdrawSystem (unseeded app): expected idempotent no-op success, got err=%v withdrawn=%t", err, withdrawn)
	}
	if !withdrawn {
		t.Fatalf("withdrawSystem (unseeded app): expected withdrawn=true (already-withdrawn terminal), got false")
	}

	observed, transitions, republished, err := tr.ReconcileOperatedState(ctx, "tick-recon-post-wd", nil)
	if err != nil {
		t.Fatalf("reconcileOperatedState after withdraw: expected success, got err=%v", err)
	}
	if observed != 0 || transitions != 0 || republished != 0 {
		t.Fatalf("reconcileOperatedState after withdraw: expected all-zero counts (no resurrection), got observed=%d transitions=%d republished=%d", observed, transitions, republished)
	}
}
