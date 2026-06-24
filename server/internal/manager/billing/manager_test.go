package billing

// =============================================================================
// SERVICE TEST PLAN (STP) — billingManager (C-BM)
// (the-method-testing: STP-first. White-box test client, black-box tests with
//  hand-written fakes. NO BDD/Gherkin.)
//
// A. Façade pre-condition / contract-misuse (this file; no Temporal client needed —
//    the checks short-circuit before any client call):
//   A1  RegisterCustomer rejects empty customerId          → ContractMisuse
//   A2  CloseBillingPeriod rejects empty customerId        → ContractMisuse
//   A3  CloseBillingPeriod rejects empty periodId          → ContractMisuse
//   A4  RunBillingRetrySweep rejects empty tickId          → ContractMisuse
//   A5  Workflow-id derivation tokens match §6.1 shapes
//
// B. RegisterCustomerWorkflow (workflow_test.go):
//   B1  happy path: validate → confirm GitHub → open aggregate → register schedule
//   B2  GitHub App not installed → FailedPrecondition surfaced
//
// C. CloseBillingPeriodWorkflow (workflow_test.go):
//   C1  happy path: read aggregate → read usage → price → charge → record (Charged=true)
//   C2  zero invoice (no usage) → Charged=false, no charge call
//   C3  period-already-closed guard (P3) → return recorded result, no re-charge
//   C4  charge decline → DecideOnBillingFailure(Escalate) → Charged=false, record
//
// D. RunBillingRetrySweepWorkflow (workflow_test.go):
//   D1  happy path: delinquent customers → charge → signal on decline
//   D2  quiet sweep (no delinquent customers) → empty SignalledCustomers
//
// E. §6.5 Conflict discipline (workflow_test.go):
//   E1  OpenBillingAggregateActivity returns Conflict → re-read → re-apply converges
// =============================================================================

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
)

// These tests cover the façade-boundary pre-condition checks the contract puts on
// the three public ops (C-BM §2/§3.4). They run BEFORE any Temporal client call,
// so they need no cluster and no client — a nil client is safe because the checks
// short-circuit first.

func asBillingError(t *testing.T, err error) *fwmgr.Error {
	t.Helper()
	var be *fwmgr.Error
	if !errors.As(err, &be) {
		t.Fatalf("expected *BillingError, got %T: %v", err, err)
	}
	return be
}

// ---- A1: RegisterCustomer ---------------------------------------------------

func Test_RegisterCustomer_EmptyCustomerID(t *testing.T) {
	m := NewManager(nil)
	_, err := m.RegisterCustomer(context.Background(), uuid.Nil)
	if got := asBillingError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// ---- A2/A3: CloseBillingPeriod ----------------------------------------------

func Test_CloseBillingPeriod_EmptyCustomerID(t *testing.T) {
	m := NewManager(nil)
	_, err := m.CloseBillingPeriod(context.Background(), uuid.Nil, "2026-06")
	if got := asBillingError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_CloseBillingPeriod_EmptyPeriodID(t *testing.T) {
	m := NewManager(nil)
	_, err := m.CloseBillingPeriod(context.Background(), uuid.New(), "")
	if got := asBillingError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// ---- A4: RunBillingRetrySweep -----------------------------------------------

func Test_RunBillingRetrySweep_EmptyTickID(t *testing.T) {
	m := NewManager(nil)
	_, err := m.RunBillingRetrySweep(context.Background(), "")
	if got := asBillingError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// ---- A5: workflow-id derivation (§6.1) --------------------------------------

func Test_WorkflowIDDerivation(t *testing.T) {
	cid := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	if got := registerWorkflowID(cid); got != cid.String()+":register" {
		t.Fatalf("register id: %q", got)
	}
	if got := closePeriodWorkflowID(cid, "2026-06"); got != cid.String()+":2026-06:close" {
		t.Fatalf("close period id: %q", got)
	}
	if got := billingRetrySweepWorkflowID("t1"); got != ":all:billingRetrySweep:t1" {
		t.Fatalf("retry sweep id: %q", got)
	}
}
