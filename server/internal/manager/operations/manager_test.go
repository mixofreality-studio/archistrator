package operations

// =============================================================================
// SERVICE TEST PLAN (STP) — operationsManager (C-MOP)
// (the-method-testing: STP-first — the list of all the ways to demonstrate the
//  component does NOT work. NO BDD/Gherkin. White-box test client + black-box tests
//  with hand-written fakes for the frozen-collaborator seams.)
//
// A. Façade pre-condition / contract-misuse (this file; no Temporal client needed —
//    the checks short-circuit before any client call):
//   A1  DeployAfterConstruction rejects empty operatedAppId            → ContractMisuse
//   A2  DeployAfterConstruction rejects empty changeId                 → ContractMisuse
//   A3  DeployAfterConstruction rejects Reason=autoscale (reserved)    → ContractMisuse  (OQ-5)
//   A4  DeployAfterConstruction rejects Reason=delinquency (reserved)  → ContractMisuse  (OQ-5)
//   A5  DeployAfterConstruction rejects Reason=unknown                 → ContractMisuse
//   A6  ReconcileOperatedState rejects empty tickId                    → ContractMisuse
//   A7  WithdrawSystem rejects empty operatedAppId / empty changeId    → ContractMisuse
//   A8  QueryCostProjection rejects empty operatedAppId / empty requestId → ContractMisuse
//   A9  ApplyDelinquencyPolicy rejects empty customerId                → ContractMisuse
//   A10 Workflow-id derivation tokens are the §6.1 shapes
//   A11 DesiredStateReason / AutoscaleAction String() coverage
//
// B. DeployWorkflow (workflow_test.go):
//   B1  happy path: read → (bundle) → publish runtime → record head-state(deploy)
//   B2  missing deployableBundleRef on a full-bundle first deploy → FailedPrecondition
//   B3  operator scale (PatchScale) republish: no bundle retrieve, publish+record
//
// C. ReconcileWorkflow (workflow_test.go):
//   C1  Path B: health transition → recordRuntimeStatusChange + DecideOnHealth(Retry)
//                                   → re-publish; usage recorded
//   C2  Path C: autoscaler Pause (idle) → publish replicas=0 + record(autoscale)
//   C3  quiet tick: no transition + NoChange → no transitions, no republishes
//   C4  multiple in-flight apps counted in ReconcileResult.Observed
//
// D. WithdrawWorkflow (workflow_test.go):
//   D1  happy path: withdraw runtime → record final usage → withdraw head-state
//   D2  already-withdrawn head-state → no-op success (no runtime call)
//   D3  read NotFound (unknown app) → no-op success
//
// E. CostProjectionWorkflow (workflow_test.go):
//   E1  reads usage range + head-state, returns the Engine projection, NO mutation
//       (asserted by zero head-state writes on the fake)
//
// F. DelinquencyEnforcementWorkflow (workflow_test.go):
//   F1  queued signal resumes branch → pause (replicas=0) publish + recordDelinquencyAction
//   F2  withdraw-terms branch → withdraw runtime + recordDelinquencyAction
//
// G. §6.5 Conflict discipline (workflow_test.go):
//   G1  recordPublishDesiredState returns Conflict twice → re-read→re-apply converges
// =============================================================================

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
)

// These tests cover the façade-boundary pre-condition checks the contract puts on the
// five public ops (operationsManager.md §2/§3.4). They run BEFORE any Temporal client
// call, so they need no cluster and no client — a nil client is safe because the
// checks short-circuit first.

// bgCtx is the Manager-layer call Context the façade pre-condition tests pass (the
// Principal is zero — these checks short-circuit before any Temporal/authz path).
func bgCtx() fwmgr.Context {
	return fwmgr.Context{Context: context.Background()}
}

func asOperationsError(t *testing.T, err error) *fwmgr.Error {
	t.Helper()
	var oe *fwmgr.Error
	if !errors.As(err, &oe) {
		t.Fatalf("expected *OperationsError, got %T: %v", err, err)
	}
	return oe
}

// ---- A1/A2: DeployAfterConstruction id checks -------------------------------

func Test_Deploy_EmptyOperatedAppID(t *testing.T) {
	m := newOperationsManager(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.DeployAfterConstruction(bgCtx(), uuid.Nil,
		DesiredStateChange{Reason: ReasonDeployAfterConstruction, ChangeID: "c1"})
	if got := asOperationsError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_Deploy_EmptyChangeID(t *testing.T) {
	m := newOperationsManager(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.DeployAfterConstruction(bgCtx(), uuid.New(),
		DesiredStateChange{Reason: ReasonOperator, ChangeID: ""})
	if got := asOperationsError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// ---- A3/A4/A5: the reason discriminator rejection (OQ-5) --------------------

func Test_Deploy_RejectsReservedAutoscaleReason(t *testing.T) {
	m := newOperationsManager(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.DeployAfterConstruction(bgCtx(), uuid.New(),
		DesiredStateChange{Reason: ReasonAutoscale, ChangeID: "c1"})
	if got := asOperationsError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("autoscale reason must be ContractMisuse on deploy, got %s", got)
	}
}

func Test_Deploy_RejectsReservedDelinquencyReason(t *testing.T) {
	m := newOperationsManager(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.DeployAfterConstruction(bgCtx(), uuid.New(),
		DesiredStateChange{Reason: ReasonDelinquency, ChangeID: "c1"})
	if got := asOperationsError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("delinquency reason must be ContractMisuse on deploy, got %s", got)
	}
}

func Test_Deploy_RejectsUnknownReason(t *testing.T) {
	m := newOperationsManager(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.DeployAfterConstruction(bgCtx(), uuid.New(),
		DesiredStateChange{Reason: ReasonUnknown, ChangeID: "c1"})
	if got := asOperationsError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("unknown reason must be ContractMisuse on deploy, got %s", got)
	}
}

// ---- A6: ReconcileOperatedState ---------------------------------------------

func Test_Reconcile_EmptyTickID(t *testing.T) {
	m := newOperationsManager(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.ReconcileOperatedState(bgCtx(), "", nil)
	if got := asOperationsError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// ---- A7: WithdrawSystem ------------------------------------------------------

func Test_Withdraw_EmptyOperatedAppID(t *testing.T) {
	m := newOperationsManager(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.WithdrawSystem(bgCtx(), uuid.Nil, "c1", WithdrawReason{})
	if got := asOperationsError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_Withdraw_EmptyChangeID(t *testing.T) {
	m := newOperationsManager(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.WithdrawSystem(bgCtx(), uuid.New(), "", WithdrawReason{})
	if got := asOperationsError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// ---- A8: QueryCostProjection ------------------------------------------------

func Test_CostProjection_EmptyOperatedAppID(t *testing.T) {
	m := newOperationsManager(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.QueryCostProjection(bgCtx(), uuid.Nil, "r1", nil)
	if got := asOperationsError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_CostProjection_EmptyRequestID(t *testing.T) {
	m := newOperationsManager(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.QueryCostProjection(bgCtx(), uuid.New(), "", nil)
	if got := asOperationsError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// ---- A8b: QueryOperatedSystemView (op 2.7) ----------------------------------

func Test_View_EmptyOperatedAppID(t *testing.T) {
	m := newOperationsManager(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.QueryOperatedSystemView(bgCtx(), uuid.Nil, "r1")
	if got := asOperationsError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_View_EmptyRequestID(t *testing.T) {
	m := newOperationsManager(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.QueryOperatedSystemView(bgCtx(), uuid.New(), "")
	if got := asOperationsError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// ---- A9: ApplyDelinquencyPolicy ---------------------------------------------

func Test_Delinquency_EmptyCustomerID(t *testing.T) {
	m := newOperationsManager(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	err := m.ApplyDelinquencyPolicy(bgCtx(), uuid.Nil, DelinquencyContext{})
	if got := asOperationsError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// ---- A10: workflow id derivation (§6.1) -------------------------------------

func Test_WorkflowIDDerivation(t *testing.T) {
	pid := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	if got := deployWorkflowID(pid, "c1"); got != pid.String()+":deploy:c1" {
		t.Fatalf("deploy id: %q", got)
	}
	if got := reconcileWorkflowID("t1"); got != "operatedStateReconcile:t1" {
		t.Fatalf("reconcile id: %q", got)
	}
	if got := withdrawWorkflowID(pid, "c2"); got != pid.String()+":withdraw:c2" {
		t.Fatalf("withdraw id: %q", got)
	}
	if got := costProjectionWorkflowID(pid, "r1"); got != pid.String()+":costProjection:r1" {
		t.Fatalf("cost projection id: %q", got)
	}
	if got := viewWorkflowID(pid, "r1"); got != pid.String()+":view:r1" {
		t.Fatalf("view id: %q", got)
	}
	if got := delinquencyWorkflowID(pid); got != pid.String()+":delinquency" {
		t.Fatalf("delinquency id: %q", got)
	}
}

// ---- A11: String coverage ---------------------------------------------------

func Test_DesiredStateReason_String(t *testing.T) {
	cases := map[DesiredStateReason]string{
		ReasonDeployAfterConstruction: "deployAfterConstruction",
		ReasonOperator:                "operator",
		ReasonAutoscale:               "autoscale",
		ReasonDelinquency:             "delinquency",
		ReasonUnknown:                 "unknown",
	}
	for r, want := range cases {
		if got := desiredStateReasonName(r); got != want {
			t.Fatalf("desiredStateReasonName(%d) = %q, want %q", int(r), got, want)
		}
	}
}

func Test_AutoscaleAction_String(t *testing.T) {
	cases := map[AutoscaleAction]string{
		AutoscaleNoChange: "NoChange", AutoscaleScaleUp: "ScaleUp",
		AutoscaleScaleDown: "ScaleDown", AutoscalePause: "Pause", AutoscaleResume: "Resume",
	}
	for a, want := range cases {
		if got := autoscaleActionName(a); got != want {
			t.Fatalf("autoscaleActionName(%d) = %q, want %q", int(a), got, want)
		}
	}
}
