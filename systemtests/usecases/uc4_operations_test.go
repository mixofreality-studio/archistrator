package usecases

import (
	"context"
	"errors"
	"testing"

	"github.com/mixofreality-studio/archistrator/systemtests/internal/harness"
)

// I-UC4 — UC4 (operations / Phase-4) black-box wire coverage per the System Test
// Plan (.aiarch/state/project.json .testingState.systemTestPlan STP-UC4): a black-
// box exercise of the operationsManager surface.
//
// IMPLEMENTATION-STATE FINDING (read directly from the server module, not inferred):
// server/cmd/server/main.go wires operationsManager's two head-state/runtime RAs
// UNCONDITIONALLY to the GENERATED no-arg STUBS —
// operatedsystemstate.NewOperatedSystemStateAccess() and
// operatedruntime.NewOperatedRuntimeAccess() — whose every method returns
// fwra.New(fwra.Unknown, "not implemented") (operatedsystemstate/contract.gen.go:99-126,
// operatedruntime/contract.gen.go:72-96). Unlike constructionManager (UC3), operations
// has NO dry-run profile (no ARCHISTRATOR_OPERATIONS_DRYRUN, no operations_dryrun.go)
// and NO external HTTP seam a systemtests fake could intercept (operatedSystemState /
// operatedRuntime are plain in-process Go stubs, not an outbound REST call to a
// configurable base URL like the GitHub rail construction/design use) — so there is
// currently NO way, black-box OR fake-augmented, to drive the STP-UC4 happy-path
// business outcomes (Healthy phase, a real published revision, a real paused/withdrawn
// transition). fwra.Unknown is non-retryable by default (framework-go/resourceaccess/
// errors.go DefaultRetryable), so the workflow fails on its FIRST activity attempt —
// every Temporal-workflow op (DeployAfterConstruction / ReconcileOperatedState /
// WithdrawSystem / QueryOperatedSystemView) therefore fails FAST and DETERMINISTICALLY
// (no hang, no flake) with the façade's uniform we.Get()-error mapping to
// fwmgr.Infrastructure (operationsmanager.go) -> HTTP 503 (statusForKind).
//
// What THIS file proves instead, faithfully mapped to the STP-UC4 case ids: the
// operationsManager wire surface is fully REGISTERED, AUTHENTICATED, and REQUEST-
// VALIDATED, and the unimplemented-RA gap surfaces as a clean, deterministic 503 —
// never a panic, a hang, or a silently wrong success — at every op the plan drives.
// ApplyDelinquencyPolicy is the one exception: it is a QUEUED Signal
// (SignalWithStartWorkflow, fire-and-forget — operationsmanager.go) that returns
// success once durably enqueued, independent of whether the enforcement workflow it
// starts later succeeds; STP-UC4-N1's step 1 (errorExpected=false) is therefore
// provable as-is even though step 2's Phase assertion is not.
//
// See the traceability table in the commit/PR description for the STP case ↔ test
// mapping and this exact caveat.

// operationsSurfaceServer boots a plain server (no construction/agentic profile —
// operationsManager needs no git substrate of its own) with the harness's own
// auto-provisioned throwaway project-state repo (main_test.go's startServer
// default), and returns a bound Transport.
func operationsSurfaceServer(t *testing.T) harness.Transport {
	t.Helper()
	srv := startServer(t, true)
	tr := harness.NewHTTPTransport(srv.BaseURL())
	t.Cleanup(func() { _ = tr.Close() })
	return tr
}

// Test_UC4_DeployReconcileObserve_UnimplementedRA_FailsFastDeterministically is
// STP-UC4-H1 (Deploy after construction, reconcile, and observe Healthy), adapted to
// the CURRENT implementation state (see the file-level finding above): the deploy ->
// reconcile -> observe chain is wired end to end, and each op fails deterministically
// on the unimplemented operatedSystemState/operatedRuntime RA rather than the plan's
// literal Published/Healthy outcome.
func Test_UC4_DeployReconcileObserve_UnimplementedRA_FailsFastDeterministically(t *testing.T) {
	requireStack(t)
	ctx := context.Background()
	tr := operationsSurfaceServer(t)

	operatedAppID := harness.NewProjectID()

	published, revision, err := tr.DeployAfterConstruction(ctx, operatedAppID, harness.DesiredStateChange{
		Reason:               "deployAfterConstruction",
		PatchKind:            "fullBundle",
		ChangeID:             "chg-deploy-001",
		RenderedDesiredState: []byte("apiVersion: v1"),
	})
	if !errors.Is(err, harness.ErrUnavailable) {
		t.Fatalf("deployAfterConstruction: expected ErrUnavailable (unimplemented operatedSystemState RA), got err=%v published=%t revision=%q", err, published, revision)
	}

	observed, transitions, republished, err := tr.ReconcileOperatedState(ctx, "tick-recon-1", []string{operatedAppID})
	if !errors.Is(err, harness.ErrUnavailable) {
		t.Fatalf("reconcileOperatedState: expected ErrUnavailable, got err=%v observed=%d transitions=%d republished=%d", err, observed, transitions, republished)
	}

	view, err := tr.QueryOperatedSystemView(ctx, operatedAppID, "req-view-1")
	if !errors.Is(err, harness.ErrUnavailable) {
		t.Fatalf("queryOperatedSystemView: expected ErrUnavailable, got err=%v view=%+v", err, view)
	}
}

// Test_UC4_ApplyDelinquencyPolicy_QueuedSignalSucceeds is STP-UC4-N1 (Delinquency
// policy pauses the operated system), adapted: the queued Manager->Manager signal
// itself is durably enqueued and returns success (provable — it never touches the
// unimplemented RA, only SignalWithStartWorkflow), which is exactly what STP-UC4-N1's
// step 1 asserts (errorExpected=false). The subsequent read-back
// (QueryOperatedSystemView, step 2's Phase=paused-not-withdrawn assertion) hits the
// same unimplemented-RA gap as every other Temporal-workflow op in this file.
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
		t.Fatalf("queryOperatedSystemView after delinquency signal: expected ErrUnavailable, got err=%v view=%+v", err, view)
	}
}

// Test_UC4_DeployAfterConstruction_DuplicateChangeID_SameDeterministicOutcome is
// STP-UC4-B1 (Duplicate DeployAfterConstruction with the same changeId is
// idempotent), adapted: a replayed changeId must never produce a DIVERGENT outcome.
// Since neither call can succeed against the unimplemented RA, "no divergence" means
// both calls fail IDENTICALLY (same ErrUnavailable, published=false both times, no
// revision minted) — a real, if weaker, replay-safety proof: the second call is not a
// crash, a hang, nor a different error from the first.
func Test_UC4_DeployAfterConstruction_DuplicateChangeID_SameDeterministicOutcome(t *testing.T) {
	requireStack(t)
	ctx := context.Background()
	tr := operationsSurfaceServer(t)

	operatedAppID := harness.NewProjectID()
	change := harness.DesiredStateChange{
		Reason:               "deployAfterConstruction",
		PatchKind:            "fullBundle",
		ChangeID:             "chg-deploy-001",
		RenderedDesiredState: []byte("apiVersion: v1"),
	}

	published1, revision1, err1 := tr.DeployAfterConstruction(ctx, operatedAppID, change)
	if !errors.Is(err1, harness.ErrUnavailable) {
		t.Fatalf("first deploy: expected ErrUnavailable, got err=%v published=%t revision=%q", err1, published1, revision1)
	}

	published2, revision2, err2 := tr.DeployAfterConstruction(ctx, operatedAppID, change)
	if !errors.Is(err2, harness.ErrUnavailable) {
		t.Fatalf("replayed deploy (same changeId): expected ErrUnavailable, got err=%v published=%t revision=%q", err2, published2, revision2)
	}
	if published1 != published2 || revision1 != revision2 {
		t.Fatalf("replayed deploy diverged from the first attempt: (published=%t,revision=%q) vs (published=%t,revision=%q)",
			published1, revision1, published2, revision2)
	}
}

// Test_UC4_WithdrawSystem_UnimplementedRA_ReconcileAlsoFailsFast is STP-UC4-N2
// (Withdrawal is terminal; reconcile does not resurrect a withdrawn app), adapted:
// both the withdrawal and the post-withdrawal reconcile fail on the same
// unimplemented-RA gap, deterministically and without a resurrection-shaped silent
// success — the strongest claim provable without operatedSystemState/operatedRuntime.
func Test_UC4_WithdrawSystem_UnimplementedRA_ReconcileAlsoFailsFast(t *testing.T) {
	requireStack(t)
	ctx := context.Background()
	tr := operationsSurfaceServer(t)

	operatedAppID := harness.NewProjectID()

	withdrawn, err := tr.WithdrawSystem(ctx, operatedAppID, "chg-withdraw-1", "Customer offboarded.")
	if !errors.Is(err, harness.ErrUnavailable) {
		t.Fatalf("withdrawSystem: expected ErrUnavailable, got err=%v withdrawn=%t", err, withdrawn)
	}

	observed, transitions, republished, err := tr.ReconcileOperatedState(ctx, "tick-recon-post-wd", []string{operatedAppID})
	if !errors.Is(err, harness.ErrUnavailable) {
		t.Fatalf("reconcileOperatedState after withdraw: expected ErrUnavailable, got err=%v observed=%d transitions=%d republished=%d", err, observed, transitions, republished)
	}
}
