package settlement

// =============================================================================
// SERVICE TEST PLAN (STP) — settlementManager (C-MST)
// (the-method-testing: STP-first — the list of all the ways to demonstrate the
//  component does NOT work. NO BDD/Gherkin. White-box test client + black-box tests
//  with hand-written fakes for the frozen-collaborator seams. This Manager handles
//  REAL MONEY — correctness, idempotency, and exact-money invariants are paramount.)
//
// A. Façade pre-condition / contract-misuse (this file; no Temporal client needed —
//    the checks short-circuit before any client call, a nil client is safe):
//   A1  OnboardPaymentIntegration rejects empty deployedAppId      → ContractMisuse
//   A2  RegisterCustomer rejects empty customerId                  → ContractMisuse
//   A3  CloseSettlementCycle rejects empty customerId              → ContractMisuse
//   A4  CloseSettlementCycle rejects empty cycleId                 → ContractMisuse
//   A5  RunShortfallSweep rejects empty tickId                     → ContractMisuse
//   A6  RecordInboundRevenue rejects empty customerId/cycleId/gatewayEventId → ContractMisuse
//   A7  RecordRevenueReversal rejects empty customerId/cycleId/gatewayEventId → ContractMisuse
//   A8  Workflow-id derivation tokens are the §6.1 shapes
//   A9  RoutingDirective String() coverage
//   A10 gatewayIdempotencyKey is settle:{customerId}:{cycleId}
//
// B. OnboardWorkflow (workflow_test.go):
//   B1  happy path: read → createConnectedAccount → wireRuntime → bindGatewayLive →
//                   registerSchedule; returns the resolved customerId
//   B2  a missing settlement aggregate (read NotFound) → FailedPrecondition; no gateway move
//
// C. RegisterCustomerWorkflow (workflow_test.go):
//   C1  happy path: validateStoredInstrument → registerCustomer; returns the customerId
//
// D. CloseCycleWorkflow (workflow_test.go) — the money spine:
//   D1  Payout: net > 0 routes payoutCustomer + records settleCycle(Payout)
//   D2  Charge: net < 0 routes chargeCustomer (positive magnitude) + records settleCycle(Charge)
//   D3  NoAction: net == 0 routes NOTHING + records settleCycle(NoAction)
//   D4  exact money: the charge amount is the EXACT positive magnitude of the signed net
//   D5  not registered/gateway-bound → FailedPrecondition; nothing routed
//   D6  inbound-revenue signals drained before close are appended (idempotent on event id)
//
// E. CloseCycleWorkflow charge-failure branch (OQ-4, workflow_test.go):
//   E1  decline → Retry → re-charge succeeds; not escalated
//   E2  decline → Escalate → settleCycle(Escalated=true); CloseCycleResult.Escalated true
//   E3  decline → Delay → not escalated, no further charge (left for the sweep)
//
// F. RecomputeCycle / chargeback saga (workflow_test.go):
//   F1  chargebackReceived → recordReversal → RecomputeNet → resettleCycle → route delta
//
// G. ShortfallSweepWorkflow (workflow_test.go):
//   G1  delinquent customers → one queued applyDelinquencyPolicy signal per customer
//   G2  empty sweep → no signals, empty SignalledCustomers (a normal quiet sweep)
//
// H. §6.5 Conflict discipline (workflow_test.go) — money-affecting idempotent replay:
//   H1  settleCycle returns Conflict twice → re-read→re-apply converges to ONE record
// =============================================================================

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
)

// These tests cover the façade-boundary pre-condition checks the contract puts on the
// six public ops (settlementManager.md §2/§3.1). They run BEFORE any Temporal client
// call, so they need no cluster and no client — a nil client is safe because the checks
// short-circuit first.

func asSettlementError(t *testing.T, err error) *fwmgr.Error {
	t.Helper()
	var se *fwmgr.Error
	if !errors.As(err, &se) {
		t.Fatalf("expected *SettlementError, got %T: %v", err, err)
	}
	return se
}

// ---- A1: OnboardPaymentIntegration ------------------------------------------

func Test_Onboard_EmptyDeployedAppID(t *testing.T) {
	m := NewManager(nil)
	_, err := m.OnboardPaymentIntegration(context.Background(), uuid.Nil)
	if got := asSettlementError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// ---- A2: RegisterCustomer ----------------------------------------------------

func Test_Register_EmptyCustomerID(t *testing.T) {
	m := NewManager(nil)
	_, err := m.RegisterCustomer(context.Background(), uuid.Nil)
	if got := asSettlementError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// ---- A3/A4: CloseSettlementCycle --------------------------------------------

func Test_Close_EmptyCustomerID(t *testing.T) {
	m := NewManager(nil)
	_, err := m.CloseSettlementCycle(context.Background(), uuid.Nil, "cycle-1")
	if got := asSettlementError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_Close_EmptyCycleID(t *testing.T) {
	m := NewManager(nil)
	_, err := m.CloseSettlementCycle(context.Background(), uuid.New(), "")
	if got := asSettlementError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// ---- A5: RunShortfallSweep --------------------------------------------------

func Test_Sweep_EmptyTickID(t *testing.T) {
	m := NewManager(nil)
	_, err := m.RunShortfallSweep(context.Background(), "")
	if got := asSettlementError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// ---- A6: RecordInboundRevenue -----------------------------------------------

func Test_RecordInbound_EmptyCustomerID(t *testing.T) {
	m := NewManager(nil)
	err := m.RecordInboundRevenue(context.Background(), GatewayRevenueEvent{CycleID: "c1", GatewayEventID: "g1"})
	if got := asSettlementError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_RecordInbound_EmptyCycleID(t *testing.T) {
	m := NewManager(nil)
	err := m.RecordInboundRevenue(context.Background(), GatewayRevenueEvent{CustomerID: uuid.New(), GatewayEventID: "g1"})
	if got := asSettlementError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_RecordInbound_EmptyGatewayEventID(t *testing.T) {
	m := NewManager(nil)
	err := m.RecordInboundRevenue(context.Background(), GatewayRevenueEvent{CustomerID: uuid.New(), CycleID: "c1"})
	if got := asSettlementError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// ---- A7: RecordRevenueReversal ----------------------------------------------

func Test_RecordReversal_EmptyGatewayEventID(t *testing.T) {
	m := NewManager(nil)
	err := m.RecordRevenueReversal(context.Background(), GatewayReversalEvent{CustomerID: uuid.New(), CycleID: "c1"})
	if got := asSettlementError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_RecordReversal_EmptyCustomerID(t *testing.T) {
	m := NewManager(nil)
	err := m.RecordRevenueReversal(context.Background(), GatewayReversalEvent{CycleID: "c1", GatewayEventID: "g1"})
	if got := asSettlementError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// ---- A8: workflow id derivation (§6.1) --------------------------------------

func Test_WorkflowIDDerivation(t *testing.T) {
	cid := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	dapp := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	if got := onboardWorkflowID(dapp); got != dapp.String()+":onboard" {
		t.Fatalf("onboard id: %q", got)
	}
	if got := registerWorkflowID(cid); got != cid.String()+":register" {
		t.Fatalf("register id: %q", got)
	}
	if got := closeWorkflowID(cid, "cycle-7"); got != cid.String()+":cycle-7:close" {
		t.Fatalf("close id: %q", got)
	}
	if got := shortfallSweepWorkflowID("t9"); got != ":all:shortfallSweep:t9" {
		t.Fatalf("sweep id: %q", got)
	}
}

// ---- A9: RoutingDirective String coverage -----------------------------------

func Test_RoutingDirective_String(t *testing.T) {
	cases := map[RoutingDirectiveSeam]string{
		RoutingNoAction: "NoAction",
		RoutingPayout:   "Payout",
		RoutingCharge:   "Charge",
	}
	for d, want := range cases {
		if got := d.String(); got != want {
			t.Fatalf("RoutingDirective(%d).String() = %q, want %q", int(d), got, want)
		}
	}
}

// ---- A10: gateway idempotency key shape -------------------------------------

func Test_GatewayIdempotencyKey(t *testing.T) {
	cid := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	if got := gatewayIdempotencyKey(cid, "cycle-3"); got != "settle:"+cid.String()+":cycle-3" {
		t.Fatalf("gateway idempotency key: %q", got)
	}
}
