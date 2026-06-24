// Package billing is the billingManager component of the archistrator server's
// Manager layer — the use-case façade that owns the Temporal billing task queue
// and orchestrates three workflows: registerCustomer (UC5 registration),
// closeBillingPeriod (UC5 period-close), and runBillingRetrySweep (retry sweep).
// Contract: .aiarch/state/project.json → .serviceContracts["C-BM"] (FROZEN, Rev r2).
//
// This is the MANAGER layer. It OWNS Temporal on the "billing" task queue:
// public ops map to Temporal Workflows, it registers the per-customer
// closeBillingPeriod:<customerId> Schedule and the hourly billingRetrySweep
// Schedule at startup, and derives the idempotency key "${workflowId}:${activityId}"
// passed down to each head-state RA write and gateway call. Temporal lives ONLY
// in this component; the downstream Engines (billingEngine, interventionEngine —
// pure, in-workflow, by value) and ResourceAccess ports (billingStateAccess,
// usageAccess, billingGatewayAccess, sourceControlAccess, durableExecutionAccess)
// import no Temporal.
//
// File layout:
//   - billingmanager.go : Manager + public ops + workflow-id helpers (§6.2)
//   - contract.go       : public façade types (§3)
//   - deps.go           : consumer-side dep interfaces + Manager-local seam types (§5)
//   - workflow.go       : Workflows struct + workflow bodies + activity option presets (§6.3/§6.4)
//   - activities.go     : Manager-owned Activity wrappers (§6.4)
//   - errors.go         : port-error → Temporal ApplicationError helper (§6.4)
//   - worker.go         : task queue / execution kinds / RegisterWorker / RegisterSchedules (§6.1)
package billing

import (
	"github.com/google/uuid"

	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
)

// ---------------------------------------------------------------------------
// Public data contracts (C-BM §3) — the Client surface.
// ---------------------------------------------------------------------------

// CustomerID is the billing aggregate identifier; a plain uuid.UUID,
// canonical in billingStateAccess (C-BM §3.0).
type CustomerID = uuid.UUID

// PeriodID is the billing period identifier (replaces the settlement-era CycleID).
type PeriodID = string

// BillingRef is the continuity token returned by RegisterCustomer (C-BM §3.3).
type BillingRef struct {
	CustomerID CustomerID `json:"customerId"`
}

// CloseBillingPeriodResult is the result of CloseBillingPeriod (C-BM §3.3).
// Charged is false on a zero invoice (normal outcome) or a persistent decline
// (Escalated path); it is NOT a BillingError.
type CloseBillingPeriodResult struct {
	CustomerID CustomerID `json:"customerId"`
	PeriodID   PeriodID   `json:"periodId"`
	Charged    bool       `json:"charged"`
}

// BillingRetrySweepResult is the result of RunBillingRetrySweep (C-BM §3.3).
// SignalledCustomers lists the customers whose persistent decline triggered the
// operationsManager applyDelinquencyPolicy signal; possibly empty (quiet sweep).
type BillingRetrySweepResult struct {
	SignalledCustomers []CustomerID `json:"signalledCustomers"`
}

// ---------------------------------------------------------------------------
// Façade error model (C-BM §3.4).
// Caller/programmer errors at the façade boundary only. A declined charge is NOT
// a BillingError — it is a terminal ContentPolicy from billingGatewayAccess routed
// to interventionEngine.DecideOnBillingFailure inside the workflow. A zero invoice
// is a normal outcome (Charged=false), not an error.
// ---------------------------------------------------------------------------

// BillingError is the typed façade error (C-BM §3.4). An alias for fwmgr.Error so
// errors.As(&BillingError) call sites work.
type BillingError = fwmgr.Error

func newError(kind fwmgr.Kind, detail string) *fwmgr.Error {
	return fwmgr.New(kind, detail)
}
